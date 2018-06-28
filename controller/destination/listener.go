package destination

import (
	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/proxy/destination"
	"github.com/runconduit/conduit/pkg/addr"
	pkgK8s "github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	coreV1 "k8s.io/api/core/v1"
)

type podsByIpFn func(string) ([]*coreV1.Pod, error)

type updateListener interface {
	Update(add []common.TcpAddress, remove []common.TcpAddress)
	ClientClose() <-chan struct{}
	ServerClose() <-chan struct{}
	NoEndpoints(exists bool)
	SetServiceId(id *serviceId)
	Stop()
}

// implements the updateListener interface
type endpointListener struct {
	stream    pb.Destination_GetServer
	podsByIp  podsByIpFn
	labels    map[string]string
	enableTLS bool
	stopCh    chan struct{}
}

func newEndpointListener(
	stream pb.Destination_GetServer,
	podsByIp podsByIpFn,
	enableTLS bool,
) *endpointListener {
	return &endpointListener{
		stream:    stream,
		podsByIp:  podsByIp,
		labels:    make(map[string]string),
		enableTLS: enableTLS,
		stopCh:    make(chan struct{}),
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

func (l *endpointListener) Update(add []common.TcpAddress, remove []common.TcpAddress) {
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

func (l *endpointListener) toWeightedAddrSet(endpoints []common.TcpAddress) *pb.WeightedAddrSet {
	addrs := make([]*pb.WeightedAddr, 0)
	for _, address := range endpoints {
		addrs = append(addrs, l.toWeightedAddr(address))
	}

	return &pb.WeightedAddrSet{
		Addrs:        addrs,
		MetricLabels: l.labels,
	}
}

func (l *endpointListener) toWeightedAddr(address common.TcpAddress) *pb.WeightedAddr {
	var tlsIdentity *pb.TlsIdentity
	metricLabelsForPod := map[string]string{}
	ipAsString := addr.IPToString(address.Ip)

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

func (l *endpointListener) toAddrSet(endpoints []common.TcpAddress) *pb.AddrSet {
	addrs := make([]*common.TcpAddress, 0)
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

	identity := pkgK8s.TLSIdentity{
		Name:                pod.Name,
		Kind:                pkgK8s.GetOwnerType(pod.ObjectMeta),
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
