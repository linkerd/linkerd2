package proxy

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type server struct {
	k8sAPI    *k8s.API
	resolvers []streamingDestinationResolver
	enableTLS bool
}

// The proxy-api service serves service discovery and other information to the
// proxy.  This implementation supports the "k8s" destination scheme and expects
// destination paths to be of the form:
// <service>.<namespace>.svc.cluster.local:<port>
//
// If the port is omitted, 80 is used as a default.  If the namespace is
// omitted, "default" is used as a default.append
//
// Addresses for the given destination are fetched from the Kubernetes Endpoints
// API.
func NewServer(addr, k8sDNSZone string, controllerNamespace string, enableTLS bool, k8sAPI *k8s.API, done chan struct{}) (*grpc.Server, net.Listener, error) {
	resolvers, err := buildResolversList(k8sDNSZone, controllerNamespace, k8sAPI)
	if err != nil {
		return nil, nil, err
	}

	srv := server{
		k8sAPI:    k8sAPI,
		resolvers: resolvers,
		enableTLS: enableTLS,
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	s := prometheus.NewGrpcServer()
	pb.RegisterDestinationServer(s, &srv)

	go func() {
		<-done
		for _, resolver := range resolvers {
			resolver.stop()
		}
	}()

	return s, lis, nil
}

func (s *server) Get(dest *pb.GetDestination, stream pb.Destination_GetServer) error {
	log.Debugf("Get %v", dest)
	host, port, err := getHostAndPort(dest)
	if err != nil {
		return err
	}

	return s.streamResolutionUsingCorrectResolverFor(host, port, stream)
}

func (s *server) GetProfile(dest *pb.GetDestination, stream pb.Destination_GetProfileServer) error {
	log.Debugf("GetProfile %v", dest)
	host, port, err := getHostAndPort(dest)
	if err != nil {
		return err
	}

	listener := newProfileListener(stream)

	for _, resolver := range s.resolvers {
		resolverCanResolve, err := resolver.canResolve(host, port)
		if err != nil {
			return fmt.Errorf("resolver [%+v] found error resolving host [%s] port [%d]: %v", resolver, host, port, err)
		}
		if resolverCanResolve {
			return resolver.streamProfiles(host, listener)
		}
	}
	return fmt.Errorf("cannot find resolver for host [%s] port [%d]", host, port)
}

func (s *server) streamResolutionUsingCorrectResolverFor(host string, port int, stream pb.Destination_GetServer) error {
	listener := newEndpointListener(stream, s.k8sAPI.GetOwnerKindAndName, s.enableTLS)

	for _, resolver := range s.resolvers {
		resolverCanResolve, err := resolver.canResolve(host, port)
		if err != nil {
			return fmt.Errorf("resolver [%+v] found error resolving host [%s] port [%d]: %v", resolver, host, port, err)
		}
		if resolverCanResolve {
			return resolver.streamResolution(host, port, listener)
		}
	}
	return fmt.Errorf("cannot find resolver for host [%s] port [%d]", host, port)
}

func getHostAndPort(dest *pb.GetDestination) (string, int, error) {
	if dest.Scheme != "k8s" {
		err := fmt.Errorf("Unsupported scheme %s", dest.Scheme)
		log.Error(err)
		return "", 0, err
	}
	hostPort := strings.Split(dest.Path, ":")
	if len(hostPort) > 2 {
		err := fmt.Errorf("Invalid destination %s", dest.Path)
		log.Error(err)
		return "", 0, err
	}
	host := hostPort[0]
	port := 80
	if len(hostPort) == 2 {
		var err error
		port, err = strconv.Atoi(hostPort[1])
		if err != nil {
			err = fmt.Errorf("Invalid port %s", hostPort[1])
			log.Error(err)
			return "", 0, err
		}
	}
	return host, port, nil
}

func buildResolversList(k8sDNSZone string, controllerNamespace string, k8sAPI *k8s.API) ([]streamingDestinationResolver, error) {
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

	k8sResolver := newK8sResolver(k8sDNSZoneLabels, controllerNamespace, k8sAPI)

	log.Infof("Adding k8s name resolver")

	return []streamingDestinationResolver{k8sResolver}, nil
}
