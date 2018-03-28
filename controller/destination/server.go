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

	endpointsWatcher := k8s.NewEndpointsWatcher(clientSet)
	err = endpointsWatcher.Run()
	if err != nil {
		return nil, nil, err
	}

	dnsWatcher := NewDnsWatcher()

	resolver, err := buildResolversList(k8sDNSZone, endpointsWatcher, dnsWatcher)
	if err != nil {
		return nil, nil, err
	}

	srv := server{
		resolvers: resolver,
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

	return streamResolutionUsingCorrectResolverFor(s.resolvers, host, port, stream)
}

func streamResolutionUsingCorrectResolverFor(resolvers []streamingDestinationResolver, host string, port int, stream pb.Destination_GetServer) error {
	listener := &endpointListener{stream: stream}

	for _, resolver := range resolvers {
		resolverCanResolve, err := resolver.canResolve(host, port)
		if err != nil {
			return fmt.Errorf("resolver [%+v] found error resolving host [%s] port[%d]: %v", resolver, host, port, err)
		}
		if resolverCanResolve {
			resolver.streamResolution(host, port, listener)
			break
		}
	}
	return nil
}

func buildResolversList(k8sDNSZone string, endpointsWatcher *k8s.EndpointsWatcher, dnsWatcher *DnsWatcher) ([]streamingDestinationResolver, error) {
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

	ipResolver := &echoIpResolver{}

	k8sResolver := &k8sResolver{k8sDNSZoneLabels: k8sDNSZoneLabels,
		endpointsWatcher: endpointsWatcher,
		dnsWatcher:       dnsWatcher,
	}

	resolvers := []streamingDestinationResolver{ipResolver, k8sResolver}
	return resolvers, nil
}

type endpointListener struct {
	stream pb.Destination_GetServer
}

func (listener *endpointListener) Done() <-chan struct{} {
	return listener.stream.Context().Done()
}

func (listener *endpointListener) Update(add []common.TcpAddress, remove []common.TcpAddress) {
	if len(add) > 0 {
		update := &pb.Update{
			Update: &pb.Update_Add{
				Add: toWeightedAddrSet(add),
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

func toWeightedAddrSet(endpoints []common.TcpAddress) *pb.WeightedAddrSet {
	addrs := make([]*pb.WeightedAddr, 0)
	for i := range endpoints {
		addrs = append(addrs, &pb.WeightedAddr{
			Addr:   &endpoints[i],
			Weight: 1,
		})
	}
	return &pb.WeightedAddrSet{Addrs: addrs}
}

func toAddrSet(endpoints []common.TcpAddress) *pb.AddrSet {
	addrs := make([]*common.TcpAddress, 0)
	for i := range endpoints {
		addrs = append(addrs, &endpoints[i])
	}
	return &pb.AddrSet{Addrs: addrs}
}
