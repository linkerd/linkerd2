package destination

import (
	"github.com/golang/protobuf/proto"
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	net "github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

type endpointUpdateListener interface {
	Update(add, remove []*updateAddress)
	ClientClose() <-chan struct{}
	ServerClose() <-chan struct{}
	NoEndpoints(exists bool)
	SetServiceID(id *serviceID)
	Stop()
}

type ownerKindAndNameFn func(*corev1.Pod) (string, string)

// updateAddress is a pairing of TCP address to Kubernetes pod object
type updateAddress struct {
	address *net.TcpAddress
	pod     *corev1.Pod
}

func (ua *updateAddress) Address() string {
	return addr.ProxyAddressToString(ua.address)
}

func (ua *updateAddress) Name() string {
	if ua.pod == nil {
		return ""
	}
	return ua.pod.Name
}

func (ua *updateAddress) Namespace() string {
	if ua.pod == nil {
		return ""
	}
	return ua.pod.Namespace
}

func (ua *updateAddress) clone() *updateAddress {
	return &updateAddress{
		pod:     ua.pod.DeepCopy(),
		address: proto.Clone(ua.address).(*net.TcpAddress),
	}
}

func diffUpdateAddresses(oldAddrs, newAddrs []*updateAddress) ([]*updateAddress, []*updateAddress) {
	addSet := make(map[string]*updateAddress)
	removeSet := make(map[string]*updateAddress)

	for _, a := range newAddrs {
		key := addr.ProxyAddressToString(a.address)
		addSet[key] = a
	}

	for _, a := range oldAddrs {
		key := addr.ProxyAddressToString(a.address)
		delete(addSet, key)
		removeSet[key] = a
	}

	for _, a := range newAddrs {
		key := addr.ProxyAddressToString(a.address)
		delete(removeSet, key)
	}

	add := make([]*updateAddress, 0)
	for _, a := range addSet {
		add = append(add, a)
	}

	remove := make([]*updateAddress, 0)
	for _, a := range removeSet {
		remove = append(remove, a)
	}

	return add, remove
}

// implements the endpointUpdateListener interface
type endpointListener struct {
	stream           pb.Destination_GetServer
	ownerKindAndName ownerKindAndNameFn
	labels           map[string]string
	enableH2Upgrade  bool
	enableTLS        bool
	stopCh           chan struct{}
	log              *log.Entry
}

func newEndpointListener(
	stream pb.Destination_GetServer,
	ownerKindAndName ownerKindAndNameFn,
	enableTLS, enableH2Upgrade bool,
) *endpointListener {
	return &endpointListener{
		stream:           stream,
		ownerKindAndName: ownerKindAndName,
		labels:           make(map[string]string),
		enableH2Upgrade:  enableH2Upgrade,
		enableTLS:        enableTLS,
		stopCh:           make(chan struct{}),
		log: log.WithFields(log.Fields{
			"component": "endpoint-listener",
		}),
	}
}

func (l *endpointListener) ClientClose() <-chan struct{} {
	return l.stream.Context().Done()
}

func (l *endpointListener) ServerClose() <-chan struct{} {
	return l.stopCh
}

func (l *endpointListener) Stop() {
	close(l.stopCh)
}

func (l *endpointListener) SetServiceID(id *serviceID) {
	if id != nil {
		l.labels = map[string]string{
			"namespace": id.namespace,
			"service":   id.name,
		}
		l.log = l.log.WithFields(log.Fields{
			"namespace": id.namespace,
			"service":   id.name,
		})
	}
}

// Update is called with lists of newly added and/or removed pods in a service
// and address updates on the listener's gRPC response stream.
//
// N.B. that pod is nil on remove addresses.
//
// TODO change the type signature to use more precise types.
func (l *endpointListener) Update(add, remove []*updateAddress) {
	l.log.Debugf("Update: add=%d; remove=%d", len(add), len(remove))

	if len(add) > 0 {
		// If pods were added, send the list of metadata-rich WeightedAddr endpoints.
		set := &pb.WeightedAddrSet{MetricLabels: l.labels}
		for _, a := range add {
			w := l.toWeightedAddr(a)
			l.log.Debugf("Update: add: addr=%s; pod=%s; %+v", a.Address(), a.Name(), w)
			set.Addrs = append(set.Addrs, w)
		}

		u := &pb.Update{Update: &pb.Update_Add{Add: set}}
		if err := l.stream.Send(u); err != nil {
			l.log.Errorf("Failed to send address update: %s", err)
		}
	}

	if len(remove) > 0 {
		// If pods were removed, send the list of IP addresses.
		set := &pb.AddrSet{}
		for _, a := range remove {
			l.log.Debugf("Update: remove: addr=%s pod=%s;", a.Address(), a.Name())
			set.Addrs = append(set.Addrs, a.address)
		}

		u := &pb.Update{Update: &pb.Update_Remove{Remove: set}}
		if err := l.stream.Send(u); err != nil {
			l.log.Errorf("Failed to send address update: %s", err)
		}
	}
}

func (l *endpointListener) NoEndpoints(exists bool) {
	l.log.Debugf("NoEndpoints(%+v)", exists)

	u := &pb.Update{
		Update: &pb.Update_NoEndpoints{
			NoEndpoints: &pb.NoEndpoints{
				Exists: exists,
			},
		},
	}
	if err := l.stream.Send(u); err != nil {
		l.log.Errorf("Failed to send address update: %s", err)
	}
}

func (l *endpointListener) toWeightedAddr(address *updateAddress) *pb.WeightedAddr {
	labels, hint, tlsIdentity := l.getAddrMetadata(address.pod)

	return &pb.WeightedAddr{
		Addr:         address.address,
		Weight:       addr.DefaultWeight,
		MetricLabels: labels,
		TlsIdentity:  tlsIdentity,
		ProtocolHint: hint,
	}
}

func (l *endpointListener) getAddrMetadata(pod *corev1.Pod) (map[string]string, *pb.ProtocolHint, *pb.TlsIdentity) {
	controllerNs := pod.Labels[pkgK8s.ControllerNSLabel]
	ownerKind, ownerName := l.ownerKindAndName(pod)
	labels := pkgK8s.GetPodLabels(ownerKind, ownerName, pod)

	var hint *pb.ProtocolHint

	// If the pod is controlled by us, then it can be hinted that this destination
	// knows H2 (and handles our orig-proto translation). Note that this check
	// does not verify that the pod's control plane matches the control plane
	// where the destination service is running; all pods injected for all control
	// planes are considered valid for providing the H2 hint.
	if l.enableH2Upgrade && controllerNs != "" {
		hint = &pb.ProtocolHint{
			Protocol: &pb.ProtocolHint_H2_{
				H2: &pb.ProtocolHint_H2{},
			},
		}
	}

	if !l.enableTLS {
		return labels, hint, nil
	}

	identity := pkgK8s.TLSIdentity{
		Name:                ownerName,
		Kind:                ownerKind,
		Namespace:           pod.Namespace,
		ControllerNamespace: controllerNs,
	}

	return labels, hint, &pb.TlsIdentity{
		Strategy: &pb.TlsIdentity_K8SPodIdentity_{
			K8SPodIdentity: &pb.TlsIdentity_K8SPodIdentity{
				PodIdentity:  identity.ToDNSName(),
				ControllerNs: controllerNs,
			},
		},
	}
}
