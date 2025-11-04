package destination

import (
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

// DefaultStreamQueueCapacity defines the default maximum number of pending
// updates buffered per stream before the stream is closed.
const DefaultStreamQueueCapacity = 100

type (
	Config struct {
		ControllerNS,
		IdentityTrustDomain,
		ClusterDomain string

		ForceOpaqueTransport,
		EnableH2Upgrade,
		EnableEndpointSlices,
		EnableIPv6,
		ExtEndpointZoneWeights bool

		MeshedHttp2ClientParams *pb.Http2ClientParams

		DefaultOpaquePorts map[uint32]struct{}

		StreamQueueCapacity int
	}

	server struct {
		pb.UnimplementedDestinationServer

		config Config

		workloads         *watcher.WorkloadWatcher
		endpoints         *watcher.EndpointsWatcher
		opaquePorts       *watcher.OpaquePortsWatcher
		profiles          *watcher.ProfileWatcher
		clusterStore      *watcher.ClusterStore
		federatedServices *federatedServiceWatcher

		k8sAPI      *k8s.API
		metadataAPI *k8s.MetadataAPI
		log         *logging.Entry
		shutdown    <-chan struct{}
	}
)

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
	addr string,
	config Config,
	k8sAPI *k8s.API,
	metadataAPI *k8s.MetadataAPI,
	clusterStore *watcher.ClusterStore,
	shutdown <-chan struct{},
) (*grpc.Server, error) {
	log := logging.WithFields(logging.Fields{
		"addr":      addr,
		"component": "server",
	})

	// Initialize indexers that are used across watchers
	err := watcher.InitializeIndexers(k8sAPI)
	if err != nil {
		return nil, err
	}

	workloads, err := watcher.NewWorkloadWatcher(k8sAPI, metadataAPI, log, config.EnableEndpointSlices, config.DefaultOpaquePorts)
	if err != nil {
		return nil, err
	}
	endpoints, err := watcher.NewEndpointsWatcher(k8sAPI, metadataAPI, log, config.EnableEndpointSlices, "local")
	if err != nil {
		return nil, err
	}
	opaquePorts, err := watcher.NewOpaquePortsWatcher(k8sAPI, log, config.DefaultOpaquePorts)
	if err != nil {
		return nil, err
	}
	profiles, err := watcher.NewProfileWatcher(k8sAPI, log)
	if err != nil {
		return nil, err
	}
	federatedServices, err := newFederatedServiceWatcher(k8sAPI, metadataAPI, &config, clusterStore, endpoints, log)
	if err != nil {
		return nil, err
	}

	srv := server{
		pb.UnimplementedDestinationServer{},
		config,
		workloads,
		endpoints,
		opaquePorts,
		profiles,
		clusterStore,
		federatedServices,
		k8sAPI,
		metadataAPI,
		log,
		shutdown,
	}

	s := prometheus.NewGrpcServer(grpc.MaxConcurrentStreams(0))
	// linkerd2-proxy-api/destination.Destination (proxy-facing)
	pb.RegisterDestinationServer(s, &srv)
	return s, nil
}
