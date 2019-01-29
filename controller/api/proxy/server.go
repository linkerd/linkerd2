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
	k8sAPI          *k8s.API
	resolver        streamingDestinationResolver
	enableH2Upgrade bool
	enableTLS       bool
}

// NewServer returns a new instance of the proxy-api server.
//
// The proxy-api server serves service discovery and other information to the
// proxy.  This implementation supports the "k8s" destination scheme and expects
// destination paths to be of the form:
// <service>.<namespace>.svc.cluster.local:<port>
//
// If the port is omitted, 80 is used as a default.  If the namespace is
// omitted, "default" is used as a default.append
//
// Addresses for the given destination are fetched from the Kubernetes Endpoints
// API.
func NewServer(
	addr, k8sDNSZone string,
	controllerNamespace string,
	enableTLS, enableH2Upgrade, singleNamespace bool,
	k8sAPI *k8s.API,
	done chan struct{},
) (*grpc.Server, net.Listener, error) {
	resolver, err := buildResolver(k8sDNSZone, controllerNamespace, k8sAPI, singleNamespace)
	if err != nil {
		return nil, nil, err
	}

	srv := server{
		k8sAPI:          k8sAPI,
		resolver:        resolver,
		enableH2Upgrade: enableH2Upgrade,
		enableTLS:       enableTLS,
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	s := prometheus.NewGrpcServer()
	pb.RegisterDestinationServer(s, &srv)

	go func() {
		<-done
		resolver.stop()
	}()

	return s, lis, nil
}

func (s *server) Get(dest *pb.GetDestination, stream pb.Destination_GetServer) error {
	log.Debugf("Get %v", dest)
	host, port, err := getHostAndPort(dest)
	if err != nil {
		return err
	}

	return s.streamResolution(host, port, stream)
}

func (s *server) GetProfile(dest *pb.GetDestination, stream pb.Destination_GetProfileServer) error {
	log.Debugf("GetProfile %v", dest)
	host, _, err := getHostAndPort(dest)
	if err != nil {
		return err
	}

	listener := newProfileListener(stream)

	err = s.resolver.streamProfiles(host, listener)
	if err != nil {
		log.Errorf("Error streaming profile for %s: %v", dest.Path, err)
	}
	return err
}

func (s *server) streamResolution(host string, port int, stream pb.Destination_GetServer) error {
	listener := newEndpointListener(stream, s.k8sAPI.GetOwnerKindAndName, s.enableTLS, s.enableH2Upgrade)

	resolverCanResolve, err := s.resolver.canResolve(host, port)
	if err != nil {
		return fmt.Errorf("resolver [%+v] found error resolving host [%s] port [%d]: %v", s.resolver, host, port, err)
	}
	if !resolverCanResolve {
		return fmt.Errorf("cannot find resolver for host [%s] port [%d]", host, port)
	}
	return s.resolver.streamResolution(host, port, listener)
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

func buildResolver(
	k8sDNSZone, controllerNamespace string,
	k8sAPI *k8s.API,
	singleNamespace bool,
) (streamingDestinationResolver, error) {
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

	var pw *profileWatcher
	if !singleNamespace {
		pw = newProfileWatcher(k8sAPI)
	}

	k8sResolver := newK8sResolver(k8sDNSZoneLabels, controllerNamespace, newEndpointsWatcher(k8sAPI), pw)

	log.Infof("Built k8s name resolver")

	return k8sResolver, nil
}
