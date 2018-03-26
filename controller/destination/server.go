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

type (
	server struct {
		resolver *destinationResolver
	}
)

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

	resolver, err := newDestinationResolver(k8sDNSZone, endpointsWatcher, dnsWatcher)
	if err != nil {
		return nil, nil, err
	}

	srv := server{
		resolver: resolver,
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

	listener := &endpointListener{stream: stream}
	return s.resolver.StreamResolutionFor(host, port, listener)
}
