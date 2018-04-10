package destination

import (
	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/proxy/destination"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/controller/util"
	pkgK8s "github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
)

type updateListener interface {
	Update(add []common.TcpAddress, remove []common.TcpAddress)
	Done() <-chan struct{}
	NoEndpoints(exists bool)
	SetServiceId(id *serviceId)
}

// implements the updateListener interface
type endpointListener struct {
	stream   pb.Destination_GetServer
	podsByIp k8s.PodIndex
	labels   map[string]string
}

func (l *endpointListener) Done() <-chan struct{} {
	return l.stream.Context().Done()
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
	for i, address := range endpoints {
		metricLabelsForPod := map[string]string{}

		ipAsString := util.IPToString(address.Ip)
		resultingPods, err := l.podsByIp.GetPodsByIndex(ipAsString)
		if err != nil {
			log.Errorf("Error while finding pod for IP [%s], this IP will be sent with no metric labels: %v", ipAsString, err)
		} else {
			if len(resultingPods) == 0 || resultingPods[0] == nil {
				log.Errorf("Could not find pod for IP [%s], this IP will be sent with no metric labels.", ipAsString)
			} else {
				pod := resultingPods[0]
				metricLabelsForPod = pkgK8s.GetOwnerLabels(pod.ObjectMeta)
				metricLabelsForPod["pod"] = pod.Name
			}
		}

		addrs = append(addrs, &pb.WeightedAddr{
			Addr:         &endpoints[i],
			Weight:       1,
			MetricLabels: metricLabelsForPod,
		})
	}

	return &pb.WeightedAddrSet{
		Addrs:        addrs,
		MetricLabels: l.labels,
	}
}

func (l *endpointListener) toAddrSet(endpoints []common.TcpAddress) *pb.AddrSet {
	addrs := make([]*common.TcpAddress, 0)
	for i := range endpoints {
		addrs = append(addrs, &endpoints[i])
	}
	return &pb.AddrSet{Addrs: addrs}
}
