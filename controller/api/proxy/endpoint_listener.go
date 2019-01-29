package proxy

import (
	"fmt"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	net "github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	coreV1 "k8s.io/api/core/v1"
)

type endpointUpdateListener interface {
	Update(add, remove []*updateAddress)
	ClientClose() <-chan struct{}
	ServerClose() <-chan struct{}
	NoEndpoints(exists bool)
	SetServiceID(id *serviceID)
	Stop()
}

type ownerKindAndNameFn func(*coreV1.Pod) (string, string)

// updateAddress is a pairing of TCP address to Kubernetes pod object
type updateAddress struct {
	address *net.TcpAddress
	pod     *coreV1.Pod
}

func (ua updateAddress) String() string {
	return fmt.Sprintf("{address:%v, pod:%s.%s}", addr.ProxyAddressToString(ua.address), ua.pod.Namespace, ua.pod.Name)
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
	}
}

func (l *endpointListener) Update(add, remove []*updateAddress) {
	if len(add) > 0 {
		update := &pb.Update{
			Update: &pb.Update_Add{
				Add: l.toWeightedAddrSet(add),
			},
		}
		err := l.stream.Send(update)
		if err != nil {
			log.Error(err)
		}
	}
	if len(remove) > 0 {
		update := &pb.Update{
			Update: &pb.Update_Remove{
				Remove: l.toAddrSet(remove),
			},
		}
		err := l.stream.Send(update)
		if err != nil {
			log.Error(err)
		}
	}
}

func (l *endpointListener) NoEndpoints(exists bool) {
	update := &pb.Update{
		Update: &pb.Update_NoEndpoints{
			NoEndpoints: &pb.NoEndpoints{
				Exists: exists,
			},
		},
	}
	l.stream.Send(update)
}

func (l *endpointListener) toWeightedAddrSet(addresses []*updateAddress) *pb.WeightedAddrSet {
	addrs := make([]*pb.WeightedAddr, 0)
	for _, a := range addresses {
		addrs = append(addrs, l.toWeightedAddr(a))
	}

	return &pb.WeightedAddrSet{
		Addrs:        addrs,
		MetricLabels: l.labels,
	}
}

func (l *endpointListener) toWeightedAddr(address *updateAddress) *pb.WeightedAddr {
	labels, hint, tlsIdentity := l.getAddrMetadata(address.pod)

	return &pb.WeightedAddr{
		Addr:         address.address,
		Weight:       1,
		MetricLabels: labels,
		TlsIdentity:  tlsIdentity,
		ProtocolHint: hint,
	}
}

func (l *endpointListener) toAddrSet(addresses []*updateAddress) *pb.AddrSet {
	addrs := make([]*net.TcpAddress, 0)
	for _, a := range addresses {
		addrs = append(addrs, a.address)
	}
	return &pb.AddrSet{Addrs: addrs}
}

func (l *endpointListener) getAddrMetadata(pod *coreV1.Pod) (map[string]string, *pb.ProtocolHint, *pb.TlsIdentity) {
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
