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
// API.
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

	resolvers, err := buildResolversList(k8sDNSZone, endpointsWatcher)
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

	return s.streamResolutionUsingCorrectResolverFor(host, port, stream)
}

func (s *server) streamResolutionUsingCorrectResolverFor(host string, port int, stream pb.Destination_GetServer) error {
	listener := &endpointListener{stream: stream, podsByIp: s.podsByIp}

	for _, resolver := range s.resolvers {
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

func buildResolversList(k8sDNSZone string, endpointsWatcher k8s.EndpointsWatcher) ([]streamingDestinationResolver, error) {
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

	k8sResolver := &k8sResolver{
		k8sDNSZoneLabels: k8sDNSZoneLabels,
		endpointsWatcher: endpointsWatcher,
	}
	log.Infof("Adding k8s name resolver")

	return []streamingDestinationResolver{k8sResolver}, nil
}
