package destination

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/proxy/destination"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/controller/util"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type server struct {
	podsByIp  k8s.PodIndex
	resolvers []streamingDestinationResolver
}

// The Destination service serves service discovery information to the proxy.
// This implementation supports the "k8s" destination scheme and expects
// destination paths to be of the form:
// <service>.<namespace>.svc.cluster.local:<port>
//
// If the port is omitted, 80 is used as a default.  If the namespace is
// omitted, "default" is used as a default.append
//
// Addresses for the given destination are fetched from the Kubernetes Endpoints
// API, or resolved via DNS in the case of ExternalName type services.
func NewServer(addr, kubeconfig string, k8sDNSZone string, done chan struct{}) (*grpc.Server, net.Listener, error) {
	clientSet, err := k8s.NewClientSet(kubeconfig)
	if err != nil {
		return nil, nil, err
	}

	podsByIp, err := k8s.NewPodsByIp(clientSet)
	if err != nil {
		return nil, nil, err
	}
	err = podsByIp.Run()
	if err != nil {
		return nil, nil, err
	}

	endpointsWatcher := k8s.NewEndpointsWatcher(clientSet)
	err = endpointsWatcher.Run()
	if err != nil {
		return nil, nil, err
	}

	dnsWatcher := NewDnsWatcher()

	resolvers, err := buildResolversList(k8sDNSZone, endpointsWatcher, dnsWatcher)
	if err != nil {
		return nil, nil, err
	}

	srv := server{
		podsByIp:  podsByIp,
		resolvers: resolvers,
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	s := util.NewGrpcServer()
	pb.RegisterDestinationServer(s, &srv)

	go func() {
		<-done
		endpointsWatcher.Stop()
	}()

	return s, lis, nil
}

func (s *server) Get(dest *common.Destination, stream pb.Destination_GetServer) error {
	log.Debugf("Get %v", dest)
	if dest.Scheme != "k8s" {
		err := fmt.Errorf("Unsupported scheme %v", dest.Scheme)
		log.Error(err)
		return err
	}
	hostPort := strings.Split(dest.Path, ":")
	if len(hostPort) > 2 {
		err := fmt.Errorf("Invalid destination %s", dest.Path)
		log.Error(err)
		return err
	}
	host := hostPort[0]
	port := 80
	if len(hostPort) == 2 {
		var err error
		port, err = strconv.Atoi(hostPort[1])
		if err != nil {
			err = fmt.Errorf("Invalid port %s", hostPort[1])
			log.Error(err)
			return err
		}
	}

	return streamResolutionUsingCorrectResolverFor(s.podsByIp, s.resolvers, host, port, stream)
}

func streamResolutionUsingCorrectResolverFor(podsByIp k8s.PodIndex, resolvers []streamingDestinationResolver, host string, port int, stream pb.Destination_GetServer) error {
	serviceName := fmt.Sprintf("%s:%d", host, port)
	listener := &endpointListener{serviceName: serviceName, stream: stream, podsByIp: podsByIp}

	for _, resolver := range resolvers {
		resolverCanResolve, err := resolver.canResolve(host, port)
		if err != nil {
			return fmt.Errorf("resolver [%+v] found error resolving host [%s] port[%d]: %v", resolver, host, port, err)
		}
		if resolverCanResolve {
			return resolver.streamResolution(host, port, listener)
		}
	}
	return fmt.Errorf("cannot find resolver for host [%s] port [%d]", host, port)
}

func buildResolversList(k8sDNSZone string, endpointsWatcher k8s.EndpointsWatcher, dnsWatcher DnsWatcher) ([]streamingDestinationResolver, error) {
	var k8sDNSZoneLabels []string
	if k8sDNSZone == "" {
		k8sDNSZoneLabels = []string{}
	} else {
		var err error
		k8sDNSZoneLabels, err = splitDNSName(k8sDNSZone)
		if err != nil {
			return nil, err
		}
	}

	ipResolver := &echoIpV4Resolver{}
	log.Infof("Adding ipv4 name resolver")

	k8sResolver := &k8sResolver{
		k8sDNSZoneLabels: k8sDNSZoneLabels,
		endpointsWatcher: endpointsWatcher,
		dnsWatcher:       dnsWatcher,
	}
	log.Infof("Adding k8s name resolver")

	return []streamingDestinationResolver{ipResolver, k8sResolver}, nil
}

type endpointListener struct {
	serviceName string
	stream      pb.Destination_GetServer
	podsByIp    k8s.PodIndex
}

func (listener *endpointListener) Done() <-chan struct{} {
	return listener.stream.Context().Done()
}

func (listener *endpointListener) Update(add []common.TcpAddress, remove []common.TcpAddress) {
	if len(add) > 0 {
		update := &pb.Update{
			Update: &pb.Update_Add{
				Add: toWeightedAddrSet(listener.serviceName, listener.podsByIp, add),
			},
		}
		err := listener.stream.Send(update)
		if err != nil {
			log.Error(err)
		}
	}
	if len(remove) > 0 {
		update := &pb.Update{
			Update: &pb.Update_Remove{
				Remove: toAddrSet(remove),
			},
		}
		err := listener.stream.Send(update)
		if err != nil {
			log.Error(err)
		}
	}
}

func (listener *endpointListener) NoEndpoints(exists bool) {
	update := &pb.Update{
		Update: &pb.Update_NoEndpoints{
			NoEndpoints: &pb.NoEndpoints{
				Exists: exists,
			},
		},
	}
	listener.stream.Send(update)
}

func toWeightedAddrSet(serviceName string, podsByIp k8s.PodIndex, endpoints []common.TcpAddress) *pb.WeightedAddrSet {
	var namespace string
	addrs := make([]*pb.WeightedAddr, 0)
	for i, address := range endpoints {
		metricLabelsForPod := map[string]string{}

		ipAsString := util.IPToString(address.Ip)
		resultingPods, err := podsByIp.GetPodsByIndex(ipAsString)
		if err != nil {
			log.Errorf("Error while finding pod for IP [%s], this IP will be sent with no metric labels: %v", ipAsString, err)
		} else {
			if len(resultingPods) == 0 || resultingPods[0] == nil {
				log.Errorf("Could not find pod for IP [%s], this IP will be sent with no metric labels.", ipAsString)
			} else {
				pod := resultingPods[0]
				metricLabelsForPod = map[string]string{
					"k8s_pod": pod.Name,
				}

				namespace = pod.Namespace
			}
		}

		addrs = append(addrs, &pb.WeightedAddr{
			Addr:         &endpoints[i],
			Weight:       1,
			MetricLabels: metricLabelsForPod,
		})
	}

	globalMetricLabels := map[string]string{
		"k8s_service":   serviceName,
		"k8s_namespace": namespace,
	}

	return &pb.WeightedAddrSet{
		Addrs:        addrs,
		MetricLabels: globalMetricLabels,
	}
}

func toAddrSet(endpoints []common.TcpAddress) *pb.AddrSet {
	addrs := make([]*common.TcpAddress, 0)
	for i := range endpoints {
		addrs = append(addrs, &endpoints[i])
	}
	return &pb.AddrSet{Addrs: addrs}
}
