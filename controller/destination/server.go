package destination

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"errors"
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
		endpoints        *k8s.EndpointsWatcher
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
	err = endpoints.Run()
	if err != nil {
		return nil, nil, err
	}

	srv, err := newServer(k8sDNSZone, endpoints)
	if err != nil {
		return nil, nil, err
	}

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
func newServer(k8sDNSZone string, endpoints *k8s.EndpointsWatcher) (*server, error) {
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
	return &server{
		k8sDNSZoneLabels: k8sDNSZoneLabels,
		endpoints:        endpoints,
	}, nil
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

	// If this is an IP address, echo it back
	isIP, ip := isIPAddress(host)
	if isIP {
		echoIPDestination(ip, port, stream)
		return nil
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

func isIPAddress(host string) (bool, *common.IPAddress) {
	ip, err := util.ParseIPV4(host)
	return err == nil, ip
}

func echoIPDestination(ip *common.IPAddress, port int, stream pb.Destination_GetServer) bool {
	update := &pb.Update{
		Update: &pb.Update_Add{
			Add: &pb.WeightedAddrSet{
				Addrs: []*pb.WeightedAddr{
					&pb.WeightedAddr{
						Addr: &common.TcpAddress{
							Ip:   ip,
							Port: uint32(port),
						},
						Weight: 1,
					},
				},
			},
		},
	}
	stream.Send(update)

	<-stream.Context().Done()

	return true
}

func (s *server) resolveKubernetesService(id string, port int, stream pb.Destination_GetServer) error {
	listener := endpointListener{stream: stream}

	s.endpoints.Subscribe(id, uint32(port), listener)

	<-stream.Context().Done()

	s.endpoints.Unsubscribe(id, uint32(port), listener)

	return nil
}

// localKubernetesServiceIdFromDNSName returns the name of the service in
// "namespace-name/service-name" form if `host` is a DNS name in a form used
// for local Kubernetes services. It returns nil if `host` isn't in such a
// form.
func (s *server) localKubernetesServiceIdFromDNSName(host string) (*string, error) {
	hostLabels, err := splitDNSName(host)
	if err != nil {
		return nil, err
	}

	// Verify that `host` ends with ".svc.$zone", ".svc.cluster.local," or ".svc".
	matched := false
	if len(s.k8sDNSZoneLabels) > 0 {
		hostLabels, matched = maybeStripSuffixLabels(hostLabels, s.k8sDNSZoneLabels)
	}
	// Accept "cluster.local" as an alias for "$zone". The Kubernetes DNS
	// specification
	// (https://github.com/kubernetes/dns/blob/master/docs/specification.md)
	// doesn't require Kubernetes to do this, but some hosting providers like
	// GKE do it, and so we need to support it for transparency.
	if !matched {
		hostLabels, matched = maybeStripSuffixLabels(hostLabels, []string{"cluster", "local"})
	}
	// TODO:
	// ```
	// 	if !matched {
	//		return nil, nil
	//  }
	// ```
	//
	// This is technically wrong since the protocol definition for the
	// Destination service indicates that `host` is a FQDN and so we should
	// never append a ".$zone" suffix to it, but we need to do this as a
	// workaround until the proxies are configured to know "$zone."
	hostLabels, matched = maybeStripSuffixLabels(hostLabels, []string{"svc"})
	if !matched {
		return nil, nil
	}

	// Extract the service name and namespace. TODO: Federated services have
	// *three* components before "svc"; see
	// https://github.com/runconduit/conduit/issues/156.
	if len(hostLabels) != 2 {
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

func splitDNSName(dnsName string) ([]string, error) {
	// TODO: Validate that `dnsName` is a valid DNS name:
	// https://github.com/runconduit/conduit/issues/170.

	// If the name is fully qualified, strip off the final dot.
	if strings.HasSuffix(dnsName, ".") {
		dnsName = dnsName[:len(dnsName)-1]
	}

	labels := strings.Split(dnsName, ".")

	// Rejects any empty labels, which is especially important to do for
	// the beginning and the end because we do matching based on labels'
	// relative positions. For example, we need to reject ".example.com"
	// instead of splitting it into ["", "example", "com"].
	for _, l := range labels {
		if l == "" {
			return []string{}, errors.New("Empty label in DNS name: " + dnsName)
		}
	}
	return labels, nil
}

func maybeStripSuffixLabels(input []string, suffix []string) ([]string, bool) {
	n := len(input) - len(suffix)
	if n < 0 {
		return input, false
	}
	if !reflect.DeepEqual(input[n:], suffix) {
		return input, false
	}
	return input[:n], true
}
