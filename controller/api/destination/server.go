package destination

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/util"
	discoveryPb "github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

type server struct {
	k8sAPI          *k8s.API
	resolver        streamingDestinationResolver
	enableH2Upgrade bool
	enableTLS       bool
	controllerNS    string
	log             *log.Entry
}

// NewServer returns a new instance of the destination server.
//
// The destination server serves service discovery and other information to the
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
	controllerNS string,
	enableTLS, enableH2Upgrade bool,
	k8sAPI *k8s.API,
	done chan struct{},
) (*grpc.Server, error) {
	resolver, err := buildResolver(k8sDNSZone, controllerNS, k8sAPI)
	if err != nil {
		return nil, err
	}

	srv := server{
		k8sAPI:          k8sAPI,
		resolver:        resolver,
		enableH2Upgrade: enableH2Upgrade,
		enableTLS:       enableTLS,
		controllerNS:    controllerNS,
		log: log.WithFields(log.Fields{
			"addr":      addr,
			"component": "server",
		}),
	}

	s := prometheus.NewGrpcServer()

	// this server satisfies 2 gRPC interfaces:
	// 1) linkerd2-proxy-api/destination.Destination (proxy-facing)
	// 2) controller/discovery.Discovery (controller-facing)
	pb.RegisterDestinationServer(s, &srv)
	discoveryPb.RegisterDiscoveryServer(s, &srv)

	go func() {
		<-done
		resolver.stop()
	}()

	return s, nil
}

func (s *server) Get(dest *pb.GetDestination, stream pb.Destination_GetServer) error {
	s.log.Debugf("Get(%+v)", dest)
	host, port, err := getHostAndPort(dest)
	if err != nil {
		return err
	}

	return s.streamResolution(host, port, stream)
}

func (s *server) GetProfile(dest *pb.GetDestination, stream pb.Destination_GetProfileServer) error {
	s.log.Debugf("GetProfile(%+v)", dest)
	host, _, err := getHostAndPort(dest)
	if err != nil {
		return err
	}

	listener := newProfileListener(stream)

	proxyID := strings.Split(dest.ProxyId, ".")
	proxyNS := ""
	// <deployment>.deployment.<namespace>.linkerd-managed.linkerd.svc.cluster.local
	if len(proxyID) >= 3 {
		proxyNS = proxyID[2]
	}

	err = s.resolver.streamProfiles(host, proxyNS, listener)
	if err != nil {
		s.log.Errorf("Error streaming profile for %s: %v", dest.Path, err)
	}
	return err
}

func (s *server) Endpoints(ctx context.Context, params *discoveryPb.EndpointsParams) (*discoveryPb.EndpointsResponse, error) {
	s.log.Debugf("Endpoints(%+v)", params)

	servicePorts := s.resolver.getState()

	rsp := discoveryPb.EndpointsResponse{
		ServicePorts: make(map[string]*discoveryPb.ServicePort),
	}

	for serviceID, portMap := range servicePorts {
		discoverySP := discoveryPb.ServicePort{
			PortEndpoints: make(map[uint32]*discoveryPb.PodAddresses),
		}
		for port, sp := range portMap {
			podAddrs := discoveryPb.PodAddresses{
				PodAddresses: []*discoveryPb.PodAddress{},
			}

			for _, ua := range sp.addresses {
				ownerKind, ownerName := s.k8sAPI.GetOwnerKindAndName(ua.pod)
				pod := util.K8sPodToPublicPod(*ua.pod, ownerKind, ownerName)

				podAddrs.PodAddresses = append(
					podAddrs.PodAddresses,
					&discoveryPb.PodAddress{
						Addr: addr.NetToPublic(ua.address),
						Pod:  &pod,
					},
				)
			}

			discoverySP.PortEndpoints[port] = &podAddrs
		}

		s.log.Debugf("ServicePorts[%s]: %+v", serviceID, discoverySP)
		rsp.ServicePorts[serviceID.String()] = &discoverySP
	}

	return &rsp, nil
}

func (s *server) streamResolution(host string, port int, stream pb.Destination_GetServer) error {
	listener := newEndpointListener(stream, s.k8sAPI.GetOwnerKindAndName, s.enableTLS, s.enableH2Upgrade, s.controllerNS)

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
	k8sDNSZone, controllerNS string,
	k8sAPI *k8s.API,
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
	serviceProfiles, err := pkgK8s.ServiceProfilesAccess(k8sAPI.Client)
	if err != nil {
		return nil, err
	}
	if serviceProfiles {
		pw = newProfileWatcher(k8sAPI)
	}

	k8sResolver := newK8sResolver(k8sDNSZoneLabels, controllerNS, newEndpointsWatcher(k8sAPI), pw)

	log.Infof("Built k8s name resolver")

	return k8sResolver, nil
}
