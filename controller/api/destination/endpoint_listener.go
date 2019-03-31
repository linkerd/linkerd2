package destination

import (
	"fmt"

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

// String is used by tests for comparison and logging.
func (ua *updateAddress) String() string {
	return fmt.Sprintf("{address=%s, pod=%s, ns=%s}", ua.Address(), ua.Name(), ua.Namespace())
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
	controllerNS,
	identityTrustDomain string
	stream           pb.Destination_GetServer
	ownerKindAndName ownerKindAndNameFn
	labels           map[string]string
	enableH2Upgrade  bool
	stopCh           chan struct{}
	log              *log.Entry
}

func newEndpointListener(
	stream pb.Destination_GetServer,
	ownerKindAndName ownerKindAndNameFn,
	enableH2Upgrade bool,
	controllerNS, identityTrustDomain string,
) *endpointListener {
	return &endpointListener{
		controllerNS:        controllerNS,
		identityTrustDomain: identityTrustDomain,
		stream:              stream,
		ownerKindAndName:    ownerKindAndName,
		labels:              make(map[string]string),
		enableH2Upgrade:     enableH2Upgrade,
		stopCh:              make(chan struct{}),
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
	weight, labels, hint, tlsIdentity := l.getAddrMetadata(address.pod)

	return &pb.WeightedAddr{
		Addr:         address.address,
		Weight:       weight,
		MetricLabels: labels,
		TlsIdentity:  tlsIdentity,
		ProtocolHint: hint,
	}
}

func (l *endpointListener) getAddrMetadata(pod *corev1.Pod) (uint32, map[string]string, *pb.ProtocolHint, *pb.TlsIdentity) {
	weight := pkgK8s.GetPodWeight(pod)

	controllerNS := pod.Labels[pkgK8s.ControllerNSLabel]
	sa, ns := pkgK8s.GetServiceAccountAndNS(pod)
	ok, on := l.ownerKindAndName(pod)
	labels := pkgK8s.GetPodLabels(ok, on, pod)

	// If the pod is controlled by any Linkerd control plane, then it can be hinted
	// that this destination knows H2 (and handles our orig-proto translation).
	var hint *pb.ProtocolHint
	if l.enableH2Upgrade && controllerNS != "" {
		hint = &pb.ProtocolHint{
			Protocol: &pb.ProtocolHint_H2_{
				H2: &pb.ProtocolHint_H2{},
			},
		}
	}

	// If the pod is controlled by the same Linkerd control plane, then it can
	// participate in identity with peers.
	//
	// TODO this should be relaxed to match a trust domain annotation so that
	// multiple meshes can participate in identity if they share trust roots.
	var identity *pb.TlsIdentity
	if l.identityTrustDomain != "" &&
		controllerNS == l.controllerNS &&
		pod.Annotations[pkgK8s.IdentityModeAnnotation] == pkgK8s.IdentityModeDefault {

		id := fmt.Sprintf("%s.%s.serviceaccount.identity.%s.%s", sa, ns, controllerNS, l.identityTrustDomain)
		identity = &pb.TlsIdentity{
			Strategy: &pb.TlsIdentity_DnsLikeIdentity_{
				DnsLikeIdentity: &pb.TlsIdentity_DnsLikeIdentity{
					Name: id,
				},
			},
		}
	}

	return weight, labels, hint, identity
}
