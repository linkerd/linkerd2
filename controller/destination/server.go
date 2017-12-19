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
	"reflect"
)

type (
	server struct {
		k8sDNSZoneLabels []string
		endpoints *k8s.EndpointsWatcher
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
// API.
func NewServer(addr, kubeconfig string, k8sDNSZone string, done chan struct{}) (*grpc.Server, net.Listener, error) {
	clientSet, err := k8s.NewClientSet(kubeconfig)
	if err != nil {
		return nil, nil, err
	}

	endpoints := k8s.NewEndpointsWatcher(clientSet)
	go endpoints.Run()

	srv := newServer(k8sDNSZone, endpoints)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	s := util.NewGrpcServer()
	pb.RegisterDestinationServer(s, srv)

	go func() {
		<-done
		endpoints.Stop()
	}()

	return s, lis, nil
}

// Split out from NewServer make it easy to write unit tests.
func newServer(k8sDNSZone string, endpoints *k8s.EndpointsWatcher) *server {
	var k8sDNSZoneLabels []string
	if k8sDNSZone == "" {
		k8sDNSZoneLabels = []string{}
	} else {
		k8sDNSZoneLabels = strings.Split(k8sDNSZone, ".")
	}
	return &server{
		k8sDNSZoneLabels: k8sDNSZoneLabels,
		endpoints: endpoints,
	}
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
			err := fmt.Errorf("Invalid port %s", hostPort[1])
			log.Error(err)
			return err
		}
	}

	id, err := s.localKubernetesServiceIdFromDNSName(host)
	if err != nil {
		log.Error(err)
		return err
	}

	if id != nil {
		return s.resolveKubernetesService(*id, port, stream)
	}

	// TODO: Resolve name using DNS similar to Kubernetes' ClusterFirst
	// resolution.
	err = fmt.Errorf("cannot resolve service that isn't a local Kubernetes service: %s", host)
	log.Error(err)
	return err
}

func (s *server) resolveKubernetesService(id string, port int, stream pb.Destination_GetServer) error {
	listener := endpointListener{stream: stream}

	s.endpoints.Subscribe(id, uint32(port), listener)

	<-stream.Context().Done()

	s.endpoints.Unsubscribe(id, uint32(port), listener)

	return nil
}

func (s *server) localKubernetesServiceIdFromDNSName(host string) (*string, error) {
	hostLabels := strings.Split(host, ".")

	// Verify that `host` ends with .svc.$zone.
	if len(hostLabels) <= 1 + len(s.k8sDNSZoneLabels) {
		return nil, nil
	}
	n := len(hostLabels) - len(s.k8sDNSZoneLabels)
	if !reflect.DeepEqual(hostLabels[n:], s.k8sDNSZoneLabels) {
		return nil, nil
	}
	if hostLabels[n - 1] != "svc" {
		return nil, nil
	}

	// Extract the service name and namespace.
	if n != 3 {
		return nil, fmt.Errorf("not a service: %s", host)
	}
	service := hostLabels[0]
	namespace := hostLabels[1]

	id := namespace + "/" + service
	return &id, nil
}

type endpointListener struct {
	stream pb.Destination_GetServer
}

func (listener endpointListener) Update(add []common.TcpAddress, remove []common.TcpAddress) {
	if len(add) > 0 {
		update := &pb.Update{
			Update: &pb.Update_Add{
				Add: toWeightedAddrSet(add),
			},
		}
		listener.stream.Send(update)
	}
	if len(remove) > 0 {
		update := &pb.Update{
			Update: &pb.Update_Remove{
				Remove: toAddrSet(remove),
			},
		}
		listener.stream.Send(update)
	}
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
