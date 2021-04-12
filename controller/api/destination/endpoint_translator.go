package destination

import (
	"fmt"
	"strconv"
	"strings"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/pkg/addr"
	"github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
)

const (
	defaultWeight uint32 = 10000
	// inboundListenAddr is the environment variable holding the inbound
	// listening address for the proxy container.
	envInboundListenAddr = "LINKERD2_PROXY_INBOUND_LISTEN_ADDR"
)

// endpointTranslator satisfies EndpointUpdateListener and translates updates
// into Destination.Get messages.
type endpointTranslator struct {
	controllerNS        string
	identityTrustDomain string
	enableH2Upgrade     bool
	nodeTopologyLabels  map[string]string
	defaultOpaquePorts  map[uint32]struct{}

	availableEndpoints watcher.AddressSet
	filteredSnapshot   watcher.AddressSet
	stream             pb.Destination_GetServer
	log                *logging.Entry
}

func newEndpointTranslator(
	controllerNS string,
	identityTrustDomain string,
	enableH2Upgrade bool,
	service string,
	srcNodeName string,
	defaultOpaquePorts map[uint32]struct{},
	nodes coreinformers.NodeInformer,
	stream pb.Destination_GetServer,
	log *logging.Entry,
) *endpointTranslator {
	log = log.WithFields(logging.Fields{
		"component": "endpoint-translator",
		"service":   service,
	})

	nodeTopologyLabels, err := getK8sNodeTopology(nodes, srcNodeName)
	if err != nil {
		log.Errorf("Failed to get node topology for node %s: %s", srcNodeName, err)
	}
	availableEndpoints := newEmptyAddressSet()

	filteredSnapshot := newEmptyAddressSet()

	return &endpointTranslator{
		controllerNS,
		identityTrustDomain,
		enableH2Upgrade,
		nodeTopologyLabels,
		defaultOpaquePorts,
		availableEndpoints,
		filteredSnapshot,
		stream,
		log,
	}
}

func (et *endpointTranslator) Add(set watcher.AddressSet) {
	for id, address := range set.Addresses {
		et.availableEndpoints.Addresses[id] = address
	}

	et.sendFilteredUpdate(set)
}

func (et *endpointTranslator) Remove(set watcher.AddressSet) {
	for id := range set.Addresses {
		delete(et.availableEndpoints.Addresses, id)
	}

	et.sendFilteredUpdate(set)
}

func (et *endpointTranslator) sendFilteredUpdate(set watcher.AddressSet) {
	et.availableEndpoints = watcher.AddressSet{
		Addresses:       et.availableEndpoints.Addresses,
		Labels:          set.Labels,
		TopologicalPref: set.TopologicalPref,
	}

	filtered := et.filterAddresses()
	diffAdd, diffRemove := et.diffEndpoints(filtered)

	if len(diffAdd.Addresses) > 0 {
		et.sendClientAdd(diffAdd)
	}
	if len(diffRemove.Addresses) > 0 {
		et.sendClientRemove(diffRemove)
	}

	et.filteredSnapshot = filtered
}

// filterAddresses is responsible for filtering endpoints based on service topology preference.
// The client will receive only endpoints with the same topology label value as the source node,
// the order of labels is based on the topological preference elicited from the K8s service.
func (et *endpointTranslator) filterAddresses() watcher.AddressSet {
	if len(et.availableEndpoints.TopologicalPref) == 0 {
		allAvailEndpoints := make(map[watcher.ID]watcher.Address)
		for k, v := range et.availableEndpoints.Addresses {
			allAvailEndpoints[k] = v
		}
		return watcher.AddressSet{
			Addresses: allAvailEndpoints,
			Labels:    et.availableEndpoints.Labels,
		}
	}

	et.log.Debugf("Filtering through address set with preference %v", et.availableEndpoints.TopologicalPref)
	filtered := make(map[watcher.ID]watcher.Address)
	for _, pref := range et.availableEndpoints.TopologicalPref {
		// '*' as a topology preference means all endpoints
		if pref == "*" {
			return et.availableEndpoints
		}

		srcLocality, ok := et.nodeTopologyLabels[pref]
		if !ok {
			continue
		}

		for id, address := range et.availableEndpoints.Addresses {
			addrLocality := address.TopologyLabels[pref]
			if addrLocality == srcLocality {
				filtered[id] = address
			}
		}

		// if we filtered at least one endpoint, it means that preference has been satisfied
		if len(filtered) > 0 {
			et.log.Debugf("Filtered %d from a total of %d", len(filtered), len(et.availableEndpoints.Addresses))
			return watcher.AddressSet{
				Addresses: filtered,
				Labels:    et.availableEndpoints.Labels,
			}
		}
	}

	// if we have no filtered endpoints or the '*' preference then no topology pref is satisfied
	return newEmptyAddressSet()
}

// diffEndpoints calculates the difference between the filtered set of endpoints in the current (Add/Remove) operation
// and the snapshot of previously filtered endpoints. This diff allows the client to receive only the endpoints that
// satisfy the topological preference, by adding new endpoints and removing stale ones.
func (et *endpointTranslator) diffEndpoints(filtered watcher.AddressSet) (watcher.AddressSet, watcher.AddressSet) {
	add := make(map[watcher.ID]watcher.Address)
	remove := make(map[watcher.ID]watcher.Address)

	for id, address := range filtered.Addresses {
		if _, ok := et.filteredSnapshot.Addresses[id]; !ok {
			add[id] = address
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

func (et *endpointTranslator) NoEndpoints(exists bool) {
	et.log.Debugf("NoEndpoints(%+v)", exists)

	et.availableEndpoints.Addresses = map[watcher.ID]watcher.Address{}
	et.filteredSnapshot.Addresses = map[watcher.ID]watcher.Address{}

	u := &pb.Update{
		Update: &pb.Update_NoEndpoints{
			NoEndpoints: &pb.NoEndpoints{
				Exists: exists,
			},
		},
	}

	et.log.Debugf("Sending destination no endpoints: %+v", u)
	if err := et.stream.Send(u); err != nil {
		et.log.Errorf("Failed to send address update: %s", err)
	}
}

func (et *endpointTranslator) sendClientAdd(set watcher.AddressSet) {
	addrs := []*pb.WeightedAddr{}
	for _, address := range set.Addresses {
		var (
			wa  *pb.WeightedAddr
			err error
		)
		if address.Pod != nil {
			opaquePorts, ok, getErr := getPodOpaquePortsAnnotations(address.Pod)
			if getErr != nil {
				et.log.Errorf("failed getting opaque ports annotation for pod: %s", getErr)
			}
			// If the opaque ports annotation was not set, then set the
			// endpoint's opaque ports to the default value.
			if !ok {
				opaquePorts = et.defaultOpaquePorts
			}

			skippedInboundPorts, skippedErr := getPodSkippedInboundPortsAnnotations(address.Pod)
			if skippedErr != nil {
				et.log.Errorf("failed getting ignored inbound ports annoatation for pod: %s", err)
			}

			wa, err = toWeightedAddr(address, opaquePorts, skippedInboundPorts, et.enableH2Upgrade, et.identityTrustDomain, et.controllerNS, et.log)
		} else {
			var authOverride *pb.AuthorityOverride
			if address.AuthorityOverride != "" {
				authOverride = &pb.AuthorityOverride{
					AuthorityOverride: address.AuthorityOverride,
				}
			}

			// handling address with no associated pod
			var addr *net.TcpAddress
			addr, err = toAddr(address)
			wa = &pb.WeightedAddr{
				Addr:              addr,
				Weight:            defaultWeight,
				AuthorityOverride: authOverride,
			}

			if address.Identity != "" {
				wa.TlsIdentity = &pb.TlsIdentity{
					Strategy: &pb.TlsIdentity_DnsLikeIdentity_{
						DnsLikeIdentity: &pb.TlsIdentity_DnsLikeIdentity{
							Name: address.Identity,
						},
					},
				}
				// in this case we most likely have a proxy on the other side, so set protocol hint as well.
				if et.enableH2Upgrade {
					wa.ProtocolHint = &pb.ProtocolHint{
						Protocol: &pb.ProtocolHint_H2_{
							H2: &pb.ProtocolHint_H2{},
						},
					}
				}
			}
		}
		if err != nil {
			et.log.Errorf("Failed to translate endpoints to weighted addr: %s", err)
			continue
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
		et.log.Errorf("Failed to send address update: %s", err)
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
		et.log.Errorf("Failed to send address update: %s", err)
	}
}

func toAddr(address watcher.Address) (*net.TcpAddress, error) {
	ip, err := addr.ParseProxyIPV4(address.IP)
	if err != nil {
		return nil, err
	}
	return &net.TcpAddress{
		Ip:   ip,
		Port: address.Port,
	}, nil
}

func toWeightedAddr(address watcher.Address, opaquePorts, skippedInboundPorts map[uint32]struct{}, enableH2Upgrade bool, identityTrustDomain string, controllerNS string, log *logging.Entry) (*pb.WeightedAddr, error) {
	controllerNSLabel := address.Pod.Labels[k8s.ControllerNSLabel]
	sa, ns := k8s.GetServiceAccountAndNS(address.Pod)
	labels := k8s.GetPodLabels(address.OwnerKind, address.OwnerName, address.Pod)
	_, isSkippedInboundPort := skippedInboundPorts[address.Port]
	// If the pod is controlled by any Linkerd control plane, then it can be
	// hinted that this destination knows H2 (and handles our orig-proto
	// translation)
	var hint *pb.ProtocolHint
	if enableH2Upgrade && controllerNSLabel != "" && !isSkippedInboundPort {
		hint = &pb.ProtocolHint{
			Protocol: &pb.ProtocolHint_H2_{
				H2: &pb.ProtocolHint_H2{},
			},
		}
		if _, ok := opaquePorts[address.Port]; ok {
			port, err := getInboundPort(&address.Pod.Spec)
			if err != nil {
				log.Error(err)
			} else {
				hint.OpaqueTransport = &pb.ProtocolHint_OpaqueTransport{
					InboundPort: port,
				}
			}
		}
	}

	// If the pod is controlled by the same Linkerd control plane, then it can
	// participate in identity with peers.
	//
	// TODO this should be relaxed to match a trust domain annotation so that
	// multiple meshes can participate in identity if they share trust roots.
	var identity *pb.TlsIdentity
	if identityTrustDomain != "" &&
		controllerNSLabel == controllerNS &&
		address.Pod.Annotations[k8s.IdentityModeAnnotation] == k8s.IdentityModeDefault &&
		!isSkippedInboundPort {

		id := fmt.Sprintf("%s.%s.serviceaccount.identity.%s.%s", sa, ns, controllerNSLabel, identityTrustDomain)
		identity = &pb.TlsIdentity{
			Strategy: &pb.TlsIdentity_DnsLikeIdentity_{
				DnsLikeIdentity: &pb.TlsIdentity_DnsLikeIdentity{
					Name: id,
				},
			},
		}
	}

	tcpAddr, err := toAddr(address)
	if err != nil {
		return nil, err
	}

	return &pb.WeightedAddr{
		Addr:         tcpAddr,
		Weight:       defaultWeight,
		MetricLabels: labels,
		TlsIdentity:  identity,
		ProtocolHint: hint,
	}, nil
}

func getK8sNodeTopology(nodes coreinformers.NodeInformer, srcNode string) (map[string]string, error) {
	nodeTopology := make(map[string]string)
	node, err := nodes.Lister().Get(srcNode)
	if err != nil {
		return nodeTopology, err
	}

	for k, v := range node.Labels {
		if k == corev1.LabelHostname ||
			k == corev1.LabelZoneFailureDomainStable ||
			k == corev1.LabelZoneRegionStable {
			nodeTopology[k] = v
		}
	}

	return nodeTopology, nil
}

func newEmptyAddressSet() watcher.AddressSet {
	return watcher.AddressSet{
		Addresses:       make(map[watcher.ID]watcher.Address),
		Labels:          make(map[string]string),
		TopologicalPref: []string{},
	}
}

// getInboundPort gets the inbound port from the proxy container's environment
// variable.
func getInboundPort(podSpec *corev1.PodSpec) (uint32, error) {
	for _, containerSpec := range podSpec.Containers {
		if containerSpec.Name != k8s.ProxyContainerName {
			continue
		}
		for _, envVar := range containerSpec.Env {
			if envVar.Name != envInboundListenAddr {
				continue
			}
			addr := strings.Split(envVar.Value, ":")
			port, err := strconv.ParseUint(addr[1], 10, 32)
			if err != nil {
				return 0, fmt.Errorf("failed to parse inbound port for proxy container: %s", err)
			}
			return uint32(port), nil
		}
	}
	return 0, fmt.Errorf("failed to find %s environment variable in any container for given pod spec", envInboundListenAddr)
}
