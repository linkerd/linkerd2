package destination

import (
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	net "github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	coreV1 "k8s.io/api/core/v1"
)

type podsByIpFn func(string) ([]*coreV1.Pod, error)
type ownerKindAndNameFn func(*coreV1.Pod) (string, string)

type updateListener interface {
	Update(add []net.TcpAddress, remove []net.TcpAddress)
	ClientClose() <-chan struct{}
	ServerClose() <-chan struct{}
	NoEndpoints(exists bool)
	SetServiceId(id *serviceId)
	Stop()
}

// implements the updateListener interface
type endpointListener struct {
	stream           pb.Destination_GetServer
	podsByIp         podsByIpFn
	ownerKindAndName ownerKindAndNameFn
	labels           map[string]string
	enableTLS        bool
	stopCh           chan struct{}
}

func newEndpointListener(
	stream pb.Destination_GetServer,
	podsByIp podsByIpFn,
	ownerKindAndName ownerKindAndNameFn,
	enableTLS bool,
) *endpointListener {
	return &endpointListener{
		stream:           stream,
		podsByIp:         podsByIp,
		ownerKindAndName: ownerKindAndName,
		labels:           make(map[string]string),
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

func (l *endpointListener) SetServiceId(id *serviceId) {
	if id != nil {
		l.labels = map[string]string{
			"namespace": id.namespace,
			"service":   id.name,
		}
	}
}

func (l *endpointListener) Update(add []net.TcpAddress, remove []net.TcpAddress) {
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

func (l *endpointListener) toWeightedAddrSet(endpoints []net.TcpAddress) *pb.WeightedAddrSet {
	addrs := make([]*pb.WeightedAddr, 0)
	for _, address := range endpoints {
		addrs = append(addrs, l.toWeightedAddr(address))
	}

	return &pb.WeightedAddrSet{
		Addrs:        addrs,
		MetricLabels: l.labels,
	}
}

func (l *endpointListener) toWeightedAddr(address net.TcpAddress) *pb.WeightedAddr {
	var tlsIdentity *pb.TlsIdentity
	metricLabelsForPod := map[string]string{}
	ipAsString := addr.ProxyIPToString(address.Ip)

	resultingPods, err := l.podsByIp(ipAsString)
	if err != nil {
		log.Errorf("Error while finding pod for IP [%s], this IP will be sent with no metric labels: %v", ipAsString, err)
	} else {
		podFound := false
		for _, pod := range resultingPods {
			if pod.Status.Phase == coreV1.PodRunning {
				podFound = true
				metricLabelsForPod = pkgK8s.GetOwnerLabels(pod.ObjectMeta)
				metricLabelsForPod["pod"] = pod.Name
				tlsIdentity = l.toTlsIdentity(pod)
				break
			}
		}
		if !podFound {
			log.Errorf("Could not find running pod for IP [%s], this IP will be sent with no metric labels.", ipAsString)
		}
	}

	return &pb.WeightedAddr{
		Addr:         &address,
		Weight:       1,
		MetricLabels: metricLabelsForPod,
		TlsIdentity:  tlsIdentity,
	}
}

func (l *endpointListener) toAddrSet(endpoints []net.TcpAddress) *pb.AddrSet {
	addrs := make([]*net.TcpAddress, 0)
	for i := range endpoints {
		addrs = append(addrs, &endpoints[i])
	}
	return &pb.AddrSet{Addrs: addrs}
}

func (l *endpointListener) toTlsIdentity(pod *coreV1.Pod) *pb.TlsIdentity {
	if !l.enableTLS {
		return nil
	}

	controllerNs := pkgK8s.GetControllerNs(pod.ObjectMeta)
	ownerKind, ownerName := l.ownerKindAndName(pod)

	identity := pkgK8s.TLSIdentity{
		Name:                ownerName,
		Kind:                ownerKind,
		Namespace:           pod.Namespace,
		ControllerNamespace: controllerNs,
	}

	return &pb.TlsIdentity{
		Strategy: &pb.TlsIdentity_K8SPodIdentity_{
			K8SPodIdentity: &pb.TlsIdentity_K8SPodIdentity{
				PodIdentity:  identity.ToDNSName(),
				ControllerNs: controllerNs,
			},
		},
	}
}
