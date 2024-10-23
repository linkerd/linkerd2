package destination

import (
	"fmt"
	"net/netip"
	"reflect"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	ewv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

const (
	defaultWeight uint32 = 10000

	// inboundListenAddr is the environment variable holding the inbound
	// listening address for the proxy container.
	envInboundListenAddr = "LINKERD2_PROXY_INBOUND_LISTEN_ADDR"

	updateQueueCapacity = 100
)

// endpointTranslator satisfies EndpointUpdateListener and translates updates
// into Destination.Get messages.
type (
	endpointTranslator struct {
		controllerNS        string
		identityTrustDomain string
		nodeTopologyZone    string
		nodeName            string
		defaultOpaquePorts  map[uint32]struct{}

		enableH2Upgrade,
		enableEndpointFiltering,
		enableIPv6,

		extEndpointZoneWeights bool

		meshedHTTP2ClientParams *pb.Http2ClientParams

		availableEndpoints watcher.AddressSet
		filteredSnapshot   watcher.AddressSet
		stream             pb.Destination_GetServer
		endStream          chan struct{}
		log                *logging.Entry
		overflowCounter    prometheus.Counter

		updates chan interface{}
		stop    chan struct{}
	}

	addUpdate struct {
		set watcher.AddressSet
	}

	removeUpdate struct {
		set watcher.AddressSet
	}

	noEndpointsUpdate struct {
		exists bool
	}
)

var updatesQueueOverflowCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "endpoint_updates_queue_overflow",
		Help: "A counter incremented whenever the endpoint updates queue overflows",
	},
	[]string{
		"service",
	},
)

func newEndpointTranslator(
	controllerNS string,
	identityTrustDomain string,
	enableH2Upgrade,
	enableEndpointFiltering,
	enableIPv6,
	extEndpointZoneWeights bool,
	meshedHTTP2ClientParams *pb.Http2ClientParams,
	service string,
	srcNodeName string,
	defaultOpaquePorts map[uint32]struct{},
	k8sAPI *k8s.MetadataAPI,
	stream pb.Destination_GetServer,
	endStream chan struct{},
	log *logging.Entry,
) *endpointTranslator {
	log = log.WithFields(logging.Fields{
		"component": "endpoint-translator",
		"service":   service,
	})

	nodeTopologyZone, err := getNodeTopologyZone(k8sAPI, srcNodeName)
	if err != nil {
		log.Errorf("Failed to get node topology zone for node %s: %s", srcNodeName, err)
	}
	availableEndpoints := newEmptyAddressSet()

	filteredSnapshot := newEmptyAddressSet()

	return &endpointTranslator{
		controllerNS,
		identityTrustDomain,
		nodeTopologyZone,
		srcNodeName,
		defaultOpaquePorts,
		enableH2Upgrade,
		enableEndpointFiltering,
		enableIPv6,
		extEndpointZoneWeights,
		meshedHTTP2ClientParams,

		availableEndpoints,
		filteredSnapshot,
		stream,
		endStream,
		log,
		updatesQueueOverflowCounter.With(prometheus.Labels{"service": service}),
		make(chan interface{}, updateQueueCapacity),
		make(chan struct{}),
	}
}

func (et *endpointTranslator) Add(set watcher.AddressSet) {
	et.enqueueUpdate(&addUpdate{set})
}

func (et *endpointTranslator) Remove(set watcher.AddressSet) {
	et.enqueueUpdate(&removeUpdate{set})
}

func (et *endpointTranslator) NoEndpoints(exists bool) {
	et.enqueueUpdate(&noEndpointsUpdate{exists})
}

// Add, Remove, and NoEndpoints are called from a client-go informer callback
// and therefore must not block. For each of these, we enqueue an update in
// a channel so that it can be processed asyncronously. To ensure that enqueuing
// does not block, we first check to see if there is capacity in the buffered
// channel. If there is not, we drop the update and signal to the stream that
// it has fallen too far behind and should be closed.
func (et *endpointTranslator) enqueueUpdate(update interface{}) {
	select {
	case et.updates <- update:
		// Update has been successfully enqueued.
	default:
		// We are unable to enqueue because the channel does not have capacity.
		// The stream has fallen too far behind and should be closed.
		et.overflowCounter.Inc()
		select {
		case <-et.endStream:
			// The endStream channel has already been closed so no action is
			// necessary.
		default:
			et.log.Error("endpoint update queue full; aborting stream")
			close(et.endStream)
		}
	}
}

// Start initiates a goroutine which processes update events off of the
// endpointTranslator's internal queue and sends to the grpc stream as
// appropriate. The goroutine calls several non-thread-safe functions (including
// Send) and therefore, Start must not be called more than once.
func (et *endpointTranslator) Start() {
	go func() {
		for {
			select {
			case update := <-et.updates:
				et.processUpdate(update)
			case <-et.stop:
				return
			}
		}
	}()
}

// Stop terminates the goroutine started by Start.
func (et *endpointTranslator) Stop() {
	close(et.stop)
}

func (et *endpointTranslator) processUpdate(update interface{}) {
	switch update := update.(type) {
	case *addUpdate:
		et.add(update.set)
	case *removeUpdate:
		et.remove(update.set)
	case *noEndpointsUpdate:
		et.noEndpoints(update.exists)
	}
}

func (et *endpointTranslator) add(set watcher.AddressSet) {
	for id, address := range set.Addresses {
		et.availableEndpoints.Addresses[id] = address
	}

	et.availableEndpoints.Labels = set.Labels
	et.availableEndpoints.LocalTrafficPolicy = set.LocalTrafficPolicy

	et.sendFilteredUpdate()
}

func (et *endpointTranslator) remove(set watcher.AddressSet) {
	for id := range set.Addresses {
		delete(et.availableEndpoints.Addresses, id)
	}

	et.sendFilteredUpdate()
}

func (et *endpointTranslator) noEndpoints(exists bool) {
	et.log.Debugf("NoEndpoints(%+v)", exists)

	et.availableEndpoints.Addresses = map[watcher.ID]watcher.Address{}
	et.filteredSnapshot.Addresses = map[watcher.ID]watcher.Address{}

	et.sendFilteredUpdate()
}

func (et *endpointTranslator) sendFilteredUpdate() {
	filtered := et.filterAddresses()
	filtered = et.selectAddressFamily(filtered)
	diffAdd, diffRemove := et.diffEndpoints(filtered)

	if len(diffAdd.Addresses) > 0 {
		et.sendClientAdd(diffAdd)
	}
	if len(diffRemove.Addresses) > 0 {
		et.sendClientRemove(diffRemove)
	}

	et.filteredSnapshot = filtered
}

func (et *endpointTranslator) selectAddressFamily(addresses watcher.AddressSet) watcher.AddressSet {
	filtered := make(map[watcher.ID]watcher.Address)
	for id, addr := range addresses.Addresses {
		if id.IPFamily == corev1.IPv6Protocol && !et.enableIPv6 {
			continue
		}

		if id.IPFamily == corev1.IPv4Protocol && et.enableIPv6 {
			// Only consider IPv4 address for which there's not already an IPv6
			// alternative
			altID := id
			altID.IPFamily = corev1.IPv6Protocol
			if _, ok := addresses.Addresses[altID]; ok {
				continue
			}
		}

		filtered[id] = addr
	}

	return watcher.AddressSet{
		Addresses:          filtered,
		Labels:             addresses.Labels,
		LocalTrafficPolicy: addresses.LocalTrafficPolicy,
	}
}

// filterAddresses is responsible for filtering endpoints based on the node's
// topology zone. The client will only receive endpoints with the same
// consumption zone as the node. An endpoints consumption zone is set
// by its Hints field and can be different than its actual Topology zone.
// when service.spec.internalTrafficPolicy is set to local, Topology Aware
// Hints are not used.
func (et *endpointTranslator) filterAddresses() watcher.AddressSet {
	filtered := make(map[watcher.ID]watcher.Address)

	// If endpoint filtering is disabled, return all available addresses.
	if !et.enableEndpointFiltering {
		for k, v := range et.availableEndpoints.Addresses {
			filtered[k] = v
		}
		return watcher.AddressSet{
			Addresses: filtered,
			Labels:    et.availableEndpoints.Labels,
		}
	}

	// If service.spec.internalTrafficPolicy is set to local, filter and return the addresses
	// for local node only
	if et.availableEndpoints.LocalTrafficPolicy {
		et.log.Debugf("Filtering through addresses that should be consumed by node %s", et.nodeName)
		for id, address := range et.availableEndpoints.Addresses {
			if address.Pod != nil && address.Pod.Spec.NodeName == et.nodeName {
				filtered[id] = address
			}
		}
		et.log.Debugf("Filtered from %d to %d addresses", len(et.availableEndpoints.Addresses), len(filtered))
		return watcher.AddressSet{
			Addresses:          filtered,
			Labels:             et.availableEndpoints.Labels,
			LocalTrafficPolicy: et.availableEndpoints.LocalTrafficPolicy,
		}
	}
	// If any address does not have a hint, then all hints are ignored and all
	// available addresses are returned. This replicates kube-proxy behavior
	// documented in the KEP: https://github.com/kubernetes/enhancements/blob/master/keps/sig-network/2433-topology-aware-hints/README.md#kube-proxy
	for _, address := range et.availableEndpoints.Addresses {
		if len(address.ForZones) == 0 {
			for k, v := range et.availableEndpoints.Addresses {
				filtered[k] = v
			}
			et.log.Debugf("Hints not available on endpointslice. Zone Filtering disabled. Falling back to routing to all pods")
			return watcher.AddressSet{
				Addresses:          filtered,
				Labels:             et.availableEndpoints.Labels,
				LocalTrafficPolicy: et.availableEndpoints.LocalTrafficPolicy,
			}
		}
	}

	// Each address that has a hint matching the node's zone should be added
	// to the set of addresses that will be returned.
	et.log.Debugf("Filtering through addresses that should be consumed by zone %s", et.nodeTopologyZone)
	for id, address := range et.availableEndpoints.Addresses {
		for _, zone := range address.ForZones {
			if zone.Name == et.nodeTopologyZone {
				filtered[id] = address
			}
		}
	}
	if len(filtered) > 0 {
		et.log.Debugf("Filtered from %d to %d addresses", len(et.availableEndpoints.Addresses), len(filtered))
		return watcher.AddressSet{
			Addresses:          filtered,
			Labels:             et.availableEndpoints.Labels,
			LocalTrafficPolicy: et.availableEndpoints.LocalTrafficPolicy,
		}
	}

	// If there were no filtered addresses, then fall to using endpoints from
	// all zones.
	for k, v := range et.availableEndpoints.Addresses {
		filtered[k] = v
	}
	return watcher.AddressSet{
		Addresses:          filtered,
		Labels:             et.availableEndpoints.Labels,
		LocalTrafficPolicy: et.availableEndpoints.LocalTrafficPolicy,
	}
}

// diffEndpoints calculates the difference between the filtered set of
// endpoints in the current (Add/Remove) operation and the snapshot of
// previously filtered endpoints. This diff allows the client to receive only
// the endpoints that match the topological zone, by adding new endpoints and
// removing stale ones.
func (et *endpointTranslator) diffEndpoints(filtered watcher.AddressSet) (watcher.AddressSet, watcher.AddressSet) {
	add := make(map[watcher.ID]watcher.Address)
	remove := make(map[watcher.ID]watcher.Address)

	for id, new := range filtered.Addresses {
		old, ok := et.filteredSnapshot.Addresses[id]
		if !ok {
			add[id] = new
		} else if !reflect.DeepEqual(old, new) {
			add[id] = new
		}
	}

	for id, address := range et.filteredSnapshot.Addresses {
		if _, ok := filtered.Addresses[id]; !ok {
			remove[id] = address
		}
	}

	return watcher.AddressSet{
			Addresses: add,
			Labels:    filtered.Labels,
		},
		watcher.AddressSet{
			Addresses: remove,
			Labels:    filtered.Labels,
		}
}

func (et *endpointTranslator) sendClientAdd(set watcher.AddressSet) {
	addrs := []*pb.WeightedAddr{}
	for _, address := range set.Addresses {
		var (
			wa          *pb.WeightedAddr
			opaquePorts map[uint32]struct{}
			err         error
		)
		if address.Pod != nil {
			opaquePorts = watcher.GetAnnotatedOpaquePorts(address.Pod, et.defaultOpaquePorts)
			wa, err = createWeightedAddr(address, opaquePorts,
				et.enableH2Upgrade, et.identityTrustDomain, et.controllerNS, et.meshedHTTP2ClientParams)
			if err != nil {
				et.log.Errorf("Failed to translate Pod endpoints to weighted addr: %s", err)
				continue
			}
		} else if address.ExternalWorkload != nil {
			opaquePorts = watcher.GetAnnotatedOpaquePortsForExternalWorkload(address.ExternalWorkload, et.defaultOpaquePorts)
			wa, err = createWeightedAddrForExternalWorkload(address, opaquePorts, et.meshedHTTP2ClientParams)
			if err != nil {
				et.log.Errorf("Failed to translate ExternalWorkload endpoints to weighted addr: %s", err)
				continue
			}
		} else {
			// When there's no associated pod, we may still need to set metadata
			// (especially for remote multi-cluster services).
			var addr *net.TcpAddress
			addr, err = toAddr(address)
			if err != nil {
				et.log.Errorf("Failed to translate endpoints to weighted addr: %s", err)
				continue
			}

			var authOverride *pb.AuthorityOverride
			if address.AuthorityOverride != "" {
				authOverride = &pb.AuthorityOverride{
					AuthorityOverride: address.AuthorityOverride,
				}
			}
			wa = &pb.WeightedAddr{
				Addr:              addr,
				Weight:            defaultWeight,
				AuthorityOverride: authOverride,
				MetricLabels:      map[string]string{},
			}

			if address.Identity != "" {
				wa.TlsIdentity = &pb.TlsIdentity{
					Strategy: &pb.TlsIdentity_DnsLikeIdentity_{
						DnsLikeIdentity: &pb.TlsIdentity_DnsLikeIdentity{
							Name: address.Identity,
						},
					},
				}
				if et.enableH2Upgrade {
					wa.ProtocolHint = &pb.ProtocolHint{
						Protocol: &pb.ProtocolHint_H2_{
							H2: &pb.ProtocolHint_H2{},
						},
					}
				}
				wa.Http2 = et.meshedHTTP2ClientParams
			}
		}

		if et.nodeTopologyZone != "" && address.Zone != nil {
			if *address.Zone == et.nodeTopologyZone {
				wa.MetricLabels["zone_locality"] = "local"

				if et.extEndpointZoneWeights {
					// EXPERIMENTAL: Use the endpoint weight field to indicate zonal
					// preference so that local endoints are more heavily weighted.
					wa.Weight *= 10
				}
			} else {
				wa.MetricLabels["zone_locality"] = "remote"
			}
		} else {
			wa.MetricLabels["zone_locality"] = "unknown"
		}

		addrs = append(addrs, wa)
	}

	add := &pb.Update{Update: &pb.Update_Add{
		Add: &pb.WeightedAddrSet{
			Addrs:        addrs,
			MetricLabels: set.Labels,
		},
	}}

	et.log.Debugf("Sending destination add: %+v", add)
	if err := et.stream.Send(add); err != nil {
		et.log.Debugf("Failed to send address update: %s", err)
	}
}

func (et *endpointTranslator) sendClientRemove(set watcher.AddressSet) {
	addrs := []*net.TcpAddress{}
	for _, address := range set.Addresses {
		tcpAddr, err := toAddr(address)
		if err != nil {
			et.log.Errorf("Failed to translate endpoints to addr: %s", err)
			continue
		}
		addrs = append(addrs, tcpAddr)
	}

	remove := &pb.Update{Update: &pb.Update_Remove{
		Remove: &pb.AddrSet{
			Addrs: addrs,
		},
	}}

	et.log.Debugf("Sending destination remove: %+v", remove)
	if err := et.stream.Send(remove); err != nil {
		et.log.Debugf("Failed to send address update: %s", err)
	}
}

func toAddr(address watcher.Address) (*net.TcpAddress, error) {
	ip, err := addr.ParseProxyIP(address.IP)
	if err != nil {
		return nil, err
	}
	return &net.TcpAddress{
		Ip:   ip,
		Port: address.Port,
	}, nil
}

func createWeightedAddrForExternalWorkload(
	address watcher.Address,
	opaquePorts map[uint32]struct{},
	http2 *pb.Http2ClientParams,
) (*pb.WeightedAddr, error) {
	tcpAddr, err := toAddr(address)
	if err != nil {
		return nil, err
	}

	weightedAddr := pb.WeightedAddr{
		Addr:         tcpAddr,
		Weight:       defaultWeight,
		MetricLabels: map[string]string{},
	}

	weightedAddr.MetricLabels = pkgK8s.GetExternalWorkloadLabels(address.OwnerKind, address.OwnerName, address.ExternalWorkload)
	// If the address is not backed by an ExternalWorkload, there is no additional metadata
	// to add.
	if address.ExternalWorkload == nil {
		return &weightedAddr, nil
	}

	weightedAddr.ProtocolHint = &pb.ProtocolHint{}
	weightedAddr.Http2 = http2

	_, opaquePort := opaquePorts[address.Port]
	// If address is set as opaque by a Server, or its port is set as
	// opaque by annotation or default value, then set the hinted protocol to
	// Opaque.
	if address.OpaqueProtocol || opaquePort {
		weightedAddr.ProtocolHint.Protocol = &pb.ProtocolHint_Opaque_{
			Opaque: &pb.ProtocolHint_Opaque{},
		}

		port, err := getInboundPortFromExternalWorkload(&address.ExternalWorkload.Spec)
		if err != nil {
			return nil, fmt.Errorf("failed to read inbound port: %w", err)
		}
		weightedAddr.ProtocolHint.OpaqueTransport = &pb.ProtocolHint_OpaqueTransport{
			InboundPort: port,
		}
	} else {
		weightedAddr.ProtocolHint.Protocol = &pb.ProtocolHint_H2_{
			H2: &pb.ProtocolHint_H2{},
		}
	}

	// we assume external workloads support only SPIRE identity
	weightedAddr.TlsIdentity = &pb.TlsIdentity{
		Strategy: &pb.TlsIdentity_UriLikeIdentity_{
			UriLikeIdentity: &pb.TlsIdentity_UriLikeIdentity{
				Uri: address.ExternalWorkload.Spec.MeshTLS.Identity,
			},
		},
		ServerName: &pb.TlsIdentity_DnsLikeIdentity{
			Name: address.ExternalWorkload.Spec.MeshTLS.ServerName,
		},
	}

	weightedAddr.MetricLabels = pkgK8s.GetExternalWorkloadLabels(address.OwnerKind, address.OwnerName, address.ExternalWorkload)
	// Set a zone label, even if it is empty (for consistency).
	z := ""
	if address.Zone != nil {
		z = *address.Zone
	}
	weightedAddr.MetricLabels["zone"] = z

	return &weightedAddr, nil
}

func createWeightedAddr(
	address watcher.Address,
	opaquePorts map[uint32]struct{},
	enableH2Upgrade bool,
	identityTrustDomain string,
	controllerNS string,
	meshedHttp2 *pb.Http2ClientParams,
) (*pb.WeightedAddr, error) {
	tcpAddr, err := toAddr(address)
	if err != nil {
		return nil, err
	}

	weightedAddr := pb.WeightedAddr{
		Addr:         tcpAddr,
		Weight:       defaultWeight,
		MetricLabels: map[string]string{},
	}

	// If the address is not backed by a pod, there is no additional metadata
	// to add.
	if address.Pod == nil {
		return &weightedAddr, nil
	}

	skippedInboundPorts := getPodSkippedInboundPortsAnnotations(address.Pod)

	controllerNSLabel := address.Pod.Labels[pkgK8s.ControllerNSLabel]
	sa, ns := pkgK8s.GetServiceAccountAndNS(address.Pod)
	weightedAddr.MetricLabels = pkgK8s.GetPodLabels(address.OwnerKind, address.OwnerName, address.Pod)

	// Set a zone label, even if it is empty (for consistency).
	z := ""
	if address.Zone != nil {
		z = *address.Zone
	}
	weightedAddr.MetricLabels["zone"] = z

	_, isSkippedInboundPort := skippedInboundPorts[address.Port]

	if controllerNSLabel != "" && !isSkippedInboundPort {
		weightedAddr.Http2 = meshedHttp2
		weightedAddr.ProtocolHint = &pb.ProtocolHint{}

		_, opaquePort := opaquePorts[address.Port]
		// If address is set as opaque by a Server, or its port is set as
		// opaque by annotation or default value, then set the hinted protocol to
		// Opaque.
		if address.OpaqueProtocol || opaquePort {
			weightedAddr.ProtocolHint.Protocol = &pb.ProtocolHint_Opaque_{
				Opaque: &pb.ProtocolHint_Opaque{},
			}

			port, err := getInboundPort(&address.Pod.Spec)
			if err != nil {
				return nil, fmt.Errorf("failed to read inbound port: %w", err)
			}
			weightedAddr.ProtocolHint.OpaqueTransport = &pb.ProtocolHint_OpaqueTransport{
				InboundPort: port,
			}
		} else if enableH2Upgrade {
			// If the pod is controlled by any Linkerd control plane, then it can be
			// hinted that this destination knows H2 (and handles our orig-proto
			// translation)
			weightedAddr.ProtocolHint.Protocol = &pb.ProtocolHint_H2_{
				H2: &pb.ProtocolHint_H2{},
			}
		}
	}

	// If the pod is controlled by the same Linkerd control plane, then it can
	// participate in identity with peers.
	//
	// TODO this should be relaxed to match a trust domain annotation so that
	// multiple meshes can participate in identity if they share trust roots.
	if identityTrustDomain != "" &&
		controllerNSLabel == controllerNS &&
		!isSkippedInboundPort {

		id := fmt.Sprintf("%s.%s.serviceaccount.identity.%s.%s", sa, ns, controllerNSLabel, identityTrustDomain)
		tlsId := &pb.TlsIdentity_DnsLikeIdentity{Name: id}

		weightedAddr.TlsIdentity = &pb.TlsIdentity{
			Strategy: &pb.TlsIdentity_DnsLikeIdentity_{
				DnsLikeIdentity: tlsId,
			},
			ServerName: tlsId,
		}
	}

	return &weightedAddr, nil
}

func getNodeTopologyZone(k8sAPI *k8s.MetadataAPI, srcNode string) (string, error) {
	node, err := k8sAPI.Get(k8s.Node, srcNode)
	if err != nil {
		return "", err
	}
	if zone, ok := node.Labels[corev1.LabelTopologyZone]; ok {
		return zone, nil
	}
	return "", nil
}

func newEmptyAddressSet() watcher.AddressSet {
	return watcher.AddressSet{
		Addresses: make(map[watcher.ID]watcher.Address),
		Labels:    make(map[string]string),
	}
}

// getInboundPort gets the inbound port from the proxy container's environment
// variable.
func getInboundPort(podSpec *corev1.PodSpec) (uint32, error) {
	containers := append(podSpec.InitContainers, podSpec.Containers...)
	for _, containerSpec := range containers {
		if containerSpec.Name != pkgK8s.ProxyContainerName {
			continue
		}
		for _, envVar := range containerSpec.Env {
			if envVar.Name != envInboundListenAddr {
				continue
			}
			addrPort, err := netip.ParseAddrPort(envVar.Value)
			if err != nil {
				return 0, fmt.Errorf("failed to parse inbound port for proxy container: %w", err)
			}

			return uint32(addrPort.Port()), nil
		}
	}
	return 0, fmt.Errorf("failed to find %s environment variable in any container for given pod spec", envInboundListenAddr)
}

// getInboundPortFromExternalWorkload gets the inbound port from the ExternalWorkload spec
// variable.
func getInboundPortFromExternalWorkload(ewSpec *ewv1beta1.ExternalWorkloadSpec) (uint32, error) {
	for _, p := range ewSpec.Ports {
		if p.Name == pkgK8s.ProxyPortName {
			return uint32(p.Port), nil
		}
	}

	return 0, fmt.Errorf("failed to find %s port for given ExternalWorkloadSpec", pkgK8s.ProxyPortName)
}
