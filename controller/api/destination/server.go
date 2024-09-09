package destination

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/controller/k8s"
	labels "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/util"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

type (
	Config struct {
		ControllerNS,
		IdentityTrustDomain,
		ClusterDomain string

		EnableH2Upgrade,
		EnableEndpointSlices,
		EnableIPv6,
		ExtEndpointZoneWeights bool

		MeshedHttp2ClientParams *pb.Http2ClientParams

		DefaultOpaquePorts map[uint32]struct{}
	}

	server struct {
		pb.UnimplementedDestinationServer

		config Config

		workloads    *watcher.WorkloadWatcher
		endpoints    *watcher.EndpointsWatcher
		opaquePorts  *watcher.OpaquePortsWatcher
		profiles     *watcher.ProfileWatcher
		clusterStore *watcher.ClusterStore

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

	srv := server{
		pb.UnimplementedDestinationServer{},
		config,
		workloads,
		endpoints,
		opaquePorts,
		profiles,
		clusterStore,
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

func (s *server) Get(dest *pb.GetDestination, stream pb.Destination_GetServer) error {
	log := s.log

	client, _ := peer.FromContext(stream.Context())
	if client != nil {
		log = log.WithField("remote", client.Addr)
	}

	var token contextToken
	if dest.GetContextToken() != "" {
		log.Debugf("Dest token: %q", dest.GetContextToken())
		token = s.parseContextToken(dest.GetContextToken())
		log = log.WithFields(logging.Fields{"context-pod": token.Pod, "context-ns": token.Ns})
	}

	log.Debugf("Get %s", dest.GetPath())

	streamEnd := make(chan struct{})
	// The host must be fully-qualified or be an IP address.
	host, port, err := getHostAndPort(dest.GetPath())
	if err != nil {
		log.Debugf("Invalid service %s", dest.GetPath())
		return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
	}

	// Return error for an IP query
	if ip := net.ParseIP(host); ip != nil {
		return status.Errorf(codes.InvalidArgument, "IP queries not supported by Get API: host=%s", host)
	}

	service, instanceID, err := parseK8sServiceName(host, s.config.ClusterDomain)
	if err != nil {
		log.Debugf("Invalid service %s", dest.GetPath())
		return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
	}

	svc, err := s.k8sAPI.Svc().Lister().Services(service.Namespace).Get(service.Name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			log.Debugf("Service not found %s", service)
			return status.Errorf(codes.NotFound, "Service %s.%s not found", service.Name, service.Namespace)
		}
		log.Debugf("Failed to get service %s: %v", service, err)
		return status.Errorf(codes.Internal, "Failed to get service %s", dest.GetPath())
	}

	remoteDiscovery, remoteDiscoveryFound := svc.Annotations["multicluster.linkerd.io/remote-discovery"]
	localDiscovery, localDiscoveryFound := svc.Annotations["multicluster.linkerd.io/local-discovery"]

	if remoteDiscoveryFound || localDiscoveryFound {
		remotes := strings.Split(remoteDiscovery, ",")
		for _, remote := range remotes {
			parts := strings.Split(remote, "@")
			remoteSvc := parts[0]
			cluster := parts[1]
			remoteWatcher, remoteConfig, found := s.clusterStore.Get(cluster)
			if !found {
				log.Errorf("Failed to get remote cluster %s", cluster)
				return status.Errorf(codes.NotFound, "Remote cluster not found: %s", cluster)
			}
			translator := newEndpointTranslator(
				s.config.ControllerNS,
				remoteConfig.TrustDomain,
				s.config.EnableH2Upgrade,
				false, // Disable endpoint filtering for remote discovery.
				s.config.EnableIPv6,
				s.config.ExtEndpointZoneWeights,
				s.config.MeshedHttp2ClientParams,
				fmt.Sprintf("%s.%s.svc.%s:%d", remoteSvc, service.Namespace, remoteConfig.ClusterDomain, port),
				token.NodeName,
				s.config.DefaultOpaquePorts,
				s.metadataAPI,
				stream,
				streamEnd,
				log,
			)
			err = remoteWatcher.Subscribe(watcher.ServiceID{Namespace: service.Namespace, Name: remoteSvc}, port, instanceID, translator)
			if err != nil {
				var ise watcher.InvalidService
				if errors.As(err, &ise) {
					log.Debugf("Invalid remote discovery service %s", dest.GetPath())
					return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
				}
				log.Errorf("Failed to subscribe to remote disocvery service %q in cluster %s: %s", dest.GetPath(), cluster, err)
				return err
			}
			defer remoteWatcher.Unsubscribe(watcher.ServiceID{Namespace: service.Namespace, Name: remoteSvc}, port, instanceID, translator)
		}

		// Local discovery
		translator := newEndpointTranslator(
			s.config.ControllerNS,
			s.config.IdentityTrustDomain,
			s.config.EnableH2Upgrade,
			true,
			s.config.EnableIPv6,
			s.config.ExtEndpointZoneWeights,
			s.config.MeshedHttp2ClientParams,
			localDiscovery,
			token.NodeName,
			s.config.DefaultOpaquePorts,
			s.metadataAPI,
			stream,
			streamEnd,
			log,
		)
		translator.Start()
		defer translator.Stop()

		err = s.endpoints.Subscribe(watcher.ServiceID{Namespace: service.Namespace, Name: localDiscovery}, port, instanceID, translator)
		if err != nil {
			var ise watcher.InvalidService
			if errors.As(err, &ise) {
				log.Debugf("Invalid service %s", dest.GetPath())
				return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
			}
			log.Errorf("Failed to subscribe to %s: %s", dest.GetPath(), err)
			return err
		}
		defer s.endpoints.Unsubscribe(service, port, instanceID, translator)

	} else if cluster, found := svc.Labels[labels.RemoteDiscoveryLabel]; found {
		// Remote discovery
		remoteSvc, found := svc.Labels[labels.RemoteServiceLabel]
		if !found {
			log.Debugf("Remote discovery service missing remote service name %s", service)
			return status.Errorf(codes.FailedPrecondition, "Remote discovery service missing remote service name %s", dest.GetPath())
		}
		remoteWatcher, remoteConfig, found := s.clusterStore.Get(cluster)
		if !found {
			log.Errorf("Failed to get remote cluster %s", cluster)
			return status.Errorf(codes.NotFound, "Remote cluster not found: %s", cluster)
		}
		translator := newEndpointTranslator(
			s.config.ControllerNS,
			remoteConfig.TrustDomain,
			s.config.EnableH2Upgrade,
			false, // Disable endpoint filtering for remote discovery.
			s.config.EnableIPv6,
			s.config.ExtEndpointZoneWeights,
			s.config.MeshedHttp2ClientParams,
			fmt.Sprintf("%s.%s.svc.%s:%d", remoteSvc, service.Namespace, remoteConfig.ClusterDomain, port),
			token.NodeName,
			s.config.DefaultOpaquePorts,
			s.metadataAPI,
			stream,
			streamEnd,
			log,
		)
		translator.Start()
		defer translator.Stop()

		err = remoteWatcher.Subscribe(watcher.ServiceID{Namespace: service.Namespace, Name: remoteSvc}, port, instanceID, translator)
		if err != nil {
			var ise watcher.InvalidService
			if errors.As(err, &ise) {
				log.Debugf("Invalid remote discovery service %s", dest.GetPath())
				return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
			}
			log.Errorf("Failed to subscribe to remote disocvery service %q in cluster %s: %s", dest.GetPath(), cluster, err)
			return err
		}
		defer remoteWatcher.Unsubscribe(watcher.ServiceID{Namespace: service.Namespace, Name: remoteSvc}, port, instanceID, translator)

	} else {
		// Local discovery
		translator := newEndpointTranslator(
			s.config.ControllerNS,
			s.config.IdentityTrustDomain,
			s.config.EnableH2Upgrade,
			true,
			s.config.EnableIPv6,
			s.config.ExtEndpointZoneWeights,
			s.config.MeshedHttp2ClientParams,
			dest.GetPath(),
			token.NodeName,
			s.config.DefaultOpaquePorts,
			s.metadataAPI,
			stream,
			streamEnd,
			log,
		)
		translator.Start()
		defer translator.Stop()

		err = s.endpoints.Subscribe(service, port, instanceID, translator)
		if err != nil {
			var ise watcher.InvalidService
			if errors.As(err, &ise) {
				log.Debugf("Invalid service %s", dest.GetPath())
				return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
			}
			log.Errorf("Failed to subscribe to %s: %s", dest.GetPath(), err)
			return err
		}
		defer s.endpoints.Unsubscribe(service, port, instanceID, translator)
	}

	select {
	case <-s.shutdown:
	case <-stream.Context().Done():
		log.Debugf("Get %s cancelled", dest.GetPath())
	case <-streamEnd:
		log.Errorf("Get %s stream aborted", dest.GetPath())
	}

	return nil
}

func (s *server) GetProfile(dest *pb.GetDestination, stream pb.Destination_GetProfileServer) error {
	log := s.log

	client, _ := peer.FromContext(stream.Context())
	if client != nil {
		log = log.WithField("remote", client.Addr)
	}

	var token contextToken
	if dest.GetContextToken() != "" {
		log.Debugf("Dest token: %q", dest.GetContextToken())
		token = s.parseContextToken(dest.GetContextToken())
		log = log.WithFields(logging.Fields{"context-pod": token.Pod, "context-ns": token.Ns})
	}

	log.Debugf("Getting profile for %s", dest.GetPath())

	// The host must be fully-qualified or be an IP address.
	host, port, err := getHostAndPort(dest.GetPath())
	if err != nil {
		log.Debugf("Invalid address %q", dest.GetPath())
		return status.Errorf(codes.InvalidArgument, "invalid authority: %q: %q", dest.GetPath(), err)
	}

	if ip := net.ParseIP(host); ip != nil {
		err = s.getProfileByIP(token, ip, port, log, stream)
		if err != nil {
			var ise watcher.InvalidService
			if errors.As(err, &ise) {
				log.Debugf("Invalid service %s", dest.GetPath())
				return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
			}
			log.Errorf("Failed to subscribe to profile by ip %q: %q", dest.GetPath(), err)
		}
		return err
	}

	err = s.getProfileByName(token, host, port, log, stream)
	if err != nil {
		var ise watcher.InvalidService
		if errors.As(err, &ise) {
			log.Debugf("Invalid service %s", dest.GetPath())
			return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
		}
		log.Errorf("Failed to subscribe to profile by name %q: %q", dest.GetPath(), err)
	}
	return err
}

func (s *server) getProfileByIP(
	token contextToken,
	ip net.IP,
	port uint32,
	log *logging.Entry,
	stream pb.Destination_GetProfileServer,
) error {
	// Get the service that the IP currently maps to.
	svcID, err := getSvcID(s.k8sAPI, ip.String(), s.log)
	if err != nil {
		return err
	}

	if svcID == nil {
		return s.subscribeToEndpointProfile(nil, "", ip.String(), port, log, stream)
	}

	fqn := fmt.Sprintf("%s.%s.svc.%s", svcID.Name, svcID.Namespace, s.config.ClusterDomain)
	return s.subscribeToServiceProfile(*svcID, token, fqn, port, log, stream)
}

func (s *server) getProfileByName(
	token contextToken,
	host string,
	port uint32,
	log *logging.Entry,
	stream pb.Destination_GetProfileServer,
) error {
	service, hostname, err := parseK8sServiceName(host, s.config.ClusterDomain)
	if err != nil {
		s.log.Debugf("Invalid service %s", host)
		return status.Errorf(codes.InvalidArgument, "invalid service %q: %q", host, err)
	}

	// If the pod name (instance ID) is not empty, it means we parsed a DNS
	// name. When we fetch the profile using a pod's DNS name, we want to
	// return an endpoint in the profile response.
	if hostname != "" {
		return s.subscribeToEndpointProfile(&service, hostname, "", port, log, stream)
	}

	return s.subscribeToServiceProfile(service, token, host, port, log, stream)
}

// Resolves a profile for a service, sending updates to the provided stream.
//
// This function does not return until the stream is closed.
func (s *server) subscribeToServiceProfile(
	service watcher.ID,
	token contextToken,
	fqn string,
	port uint32,
	log *logging.Entry,
	stream pb.Destination_GetProfileServer,
) error {
	log = log.
		WithField("ns", service.Namespace).
		WithField("svc", service.Name).
		WithField("port", port)

	canceled := stream.Context().Done()
	streamEnd := make(chan struct{})

	// We build up the pipeline of profile updaters backwards, starting from
	// the translator which takes profile updates, translates them to protobuf
	// and pushes them onto the gRPC stream.
	translator := newProfileTranslator(stream, log, fqn, port, streamEnd)
	translator.Start()
	defer translator.Stop()

	// The opaque ports adaptor merges profile updates with service opaque
	// port annotation updates; it then publishes the result to the traffic
	// split adaptor.
	opaquePortsAdaptor := newOpaquePortsAdaptor(translator)

	// Create an adaptor that merges service-level opaque port configurations
	// onto profile updates.
	err := s.opaquePorts.Subscribe(service, opaquePortsAdaptor)
	if err != nil {
		log.Warnf("Failed to subscribe to service updates for %s: %s", service, err)
		return err
	}
	defer s.opaquePorts.Unsubscribe(service, opaquePortsAdaptor)

	// Ensure that (1) nil values are turned into a default policy and (2)
	// subsequent updates that refer to same service profile object are
	// deduplicated to prevent sending redundant updates.
	dup := newDedupProfileListener(opaquePortsAdaptor, log)
	defaultProfile := sp.ServiceProfile{}
	listener := newDefaultProfileListener(&defaultProfile, dup, log)

	// The primary lookup uses the context token to determine the requester's
	// namespace. If there's no namespace in the token, start a single
	// subscription.
	if token.Ns == "" {
		return s.subscribeToServiceWithoutContext(fqn, listener, canceled, log, streamEnd)
	}
	return s.subscribeToServicesWithContext(fqn, token, listener, canceled, log, streamEnd)
}

// subscribeToServicesWithContext establishes two profile watches: a "backup"
// watch (ignoring the client namespace) and a preferred "primary" watch
// assuming the client's context. Once updates are received for both watches, we
// select over both watches to send profile updates to the stream. A nil update
// may be sent if both the primary and backup watches are initialized with a nil
// value.
func (s *server) subscribeToServicesWithContext(
	fqn string,
	token contextToken,
	listener watcher.ProfileUpdateListener,
	canceled <-chan struct{},
	log *logging.Entry,
	streamEnd <-chan struct{},
) error {
	// We ned to support two subscriptions:
	// - First, a backup subscription that assumes the context of the server
	//   namespace.
	// - And then, a primary subscription that assumes the context of the client
	//   namespace.
	primary, backup := newFallbackProfileListener(listener, log)

	// The backup lookup ignores the context token to lookup any
	// server-namespace-hosted profiles.
	backupID, err := profileID(fqn, contextToken{}, s.config.ClusterDomain)
	if err != nil {
		log.Debug("Invalid service")
		return status.Errorf(codes.InvalidArgument, "invalid profile ID: %s", err)
	}
	err = s.profiles.Subscribe(backupID, backup)
	if err != nil {
		log.Warnf("Failed to subscribe to profile: %s", err)
		return err
	}
	defer s.profiles.Unsubscribe(backupID, backup)

	primaryID, err := profileID(fqn, token, s.config.ClusterDomain)
	if err != nil {
		log.Debug("Invalid service")
		return status.Errorf(codes.InvalidArgument, "invalid profile ID: %s", err)
	}
	err = s.profiles.Subscribe(primaryID, primary)
	if err != nil {
		log.Warnf("Failed to subscribe to profile: %s", err)
		return err
	}
	defer s.profiles.Unsubscribe(primaryID, primary)

	select {
	case <-s.shutdown:
	case <-canceled:
		log.Debugf("GetProfile %s cancelled", fqn)
	case <-streamEnd:
		log.Errorf("GetProfile %s stream aborted", fqn)
	}
	return nil
}

// subscribeToServiceWithoutContext establishes a single profile watch, assuming
// no client context. All udpates are published to the provided listener.
func (s *server) subscribeToServiceWithoutContext(
	fqn string,
	listener watcher.ProfileUpdateListener,
	canceled <-chan struct{},
	log *logging.Entry,
	streamEnd <-chan struct{},
) error {
	id, err := profileID(fqn, contextToken{}, s.config.ClusterDomain)
	if err != nil {
		log.Debug("Invalid service")
		return status.Errorf(codes.InvalidArgument, "invalid profile ID: %s", err)
	}
	err = s.profiles.Subscribe(id, listener)
	if err != nil {
		log.Warnf("Failed to subscribe to profile: %s", err)
		return err
	}
	defer s.profiles.Unsubscribe(id, listener)

	select {
	case <-s.shutdown:
	case <-canceled:
		log.Debugf("GetProfile %s cancelled", fqn)
	case <-streamEnd:
		log.Errorf("GetProfile %s stream aborted", fqn)
	}
	return nil
}

// Resolves a profile for a single endpoint, sending updates to the provided
// stream.
//
// This function does not return until the stream is closed.
func (s *server) subscribeToEndpointProfile(
	service *watcher.ServiceID,
	hostname,
	ip string,
	port uint32,
	log *logging.Entry,
	stream pb.Destination_GetProfileServer,
) error {
	canceled := stream.Context().Done()
	streamEnd := make(chan struct{})
	translator := newEndpointProfileTranslator(
		s.config.EnableH2Upgrade,
		s.config.ControllerNS,
		s.config.IdentityTrustDomain,
		s.config.DefaultOpaquePorts,
		s.config.MeshedHttp2ClientParams,
		stream,
		streamEnd,
		log,
	)
	translator.Start()
	defer translator.Stop()

	var err error
	ip, err = s.workloads.Subscribe(service, hostname, ip, port, translator)
	if err != nil {
		return err
	}
	defer s.workloads.Unsubscribe(ip, port, translator)

	select {
	case <-s.shutdown:
	case <-canceled:
		s.log.Debugf("Cancelled")
	case <-streamEnd:
		log.Errorf("GetProfile %s:%d stream aborted", ip, port)
	}
	return nil
}

// getSvcID returns the service that corresponds to a Cluster IP address if one
// exists.
func getSvcID(k8sAPI *k8s.API, clusterIP string, log *logging.Entry) (*watcher.ServiceID, error) {
	objs, err := k8sAPI.Svc().Informer().GetIndexer().ByIndex(watcher.PodIPIndex, clusterIP)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	services := make([]*corev1.Service, 0)
	for _, obj := range objs {
		service := obj.(*corev1.Service)
		services = append(services, service)
	}
	if len(services) > 1 {
		conflictingServices := []string{}
		for _, service := range services {
			conflictingServices = append(conflictingServices, fmt.Sprintf("%s:%s", service.Namespace, service.Name))
		}
		log.Warnf("found conflicting %s cluster IP: %s", clusterIP, strings.Join(conflictingServices, ","))
		return nil, status.Errorf(codes.FailedPrecondition, "found %d services with conflicting cluster IP %s", len(services), clusterIP)
	}
	if len(services) == 0 {
		return nil, nil
	}
	service := &watcher.ServiceID{
		Namespace: services[0].Namespace,
		Name:      services[0].Name,
	}
	return service, nil
}

////////////
/// util ///
////////////

type contextToken struct {
	Ns       string `json:"ns,omitempty"`
	NodeName string `json:"nodeName,omitempty"`
	Pod      string `json:"pod,omitempty"`
}

func (s *server) parseContextToken(token string) contextToken {
	ctxToken := contextToken{}
	if token == "" {
		return ctxToken
	}
	if err := json.Unmarshal([]byte(token), &ctxToken); err != nil {
		// if json is invalid, means token can have ns:<namespace> form
		parts := strings.Split(token, ":")
		if len(parts) == 2 && parts[0] == "ns" {
			s.log.Warnf("context token %s using old token format", token)
			ctxToken = contextToken{
				Ns: parts[1],
			}
		} else {
			s.log.Errorf("context token %s is invalid: %s", token, err)
		}
	}
	return ctxToken
}

func profileID(authority string, ctxToken contextToken, clusterDomain string) (watcher.ProfileID, error) {
	host, _, err := getHostAndPort(authority)
	if err != nil {
		return watcher.ProfileID{}, fmt.Errorf("invalid authority: %w", err)
	}
	service, _, err := parseK8sServiceName(host, clusterDomain)
	if err != nil {
		return watcher.ProfileID{}, fmt.Errorf("invalid k8s service name: %w", err)
	}
	id := watcher.ProfileID{
		Name:      fmt.Sprintf("%s.%s.svc.%s", service.Name, service.Namespace, clusterDomain),
		Namespace: service.Namespace,
	}
	if ctxToken.Ns != "" {
		id.Namespace = ctxToken.Ns
	}
	return id, nil
}

func getHostAndPort(authority string) (string, watcher.Port, error) {
	if !strings.Contains(authority, ":") {
		return authority, watcher.Port(80), nil
	}

	host, sport, err := net.SplitHostPort(authority)
	if err != nil {
		return "", 0, fmt.Errorf("invalid destination: %w", err)
	}
	port, err := strconv.Atoi(sport)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port %s: %w", sport, err)
	}
	if port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("invalid port %d", port)
	}
	return host, watcher.Port(port), nil
}

type instanceID = string

// parseK8sServiceName is a utility that destructures a Kubernetes service hostname into its constituent components.
//
// If the authority does not represent a Kubernetes service, an error is returned.
//
// If the hostname is a pod DNS name, then the pod's name (instanceID) is returned
// as well. See https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/.
func parseK8sServiceName(fqdn, clusterDomain string) (watcher.ServiceID, instanceID, error) {
	labels := strings.Split(fqdn, ".")
	suffix := append([]string{"svc"}, strings.Split(clusterDomain, ".")...)

	if !hasSuffix(labels, suffix) {
		return watcher.ServiceID{}, "", fmt.Errorf("name %s does not match cluster domain %s", fqdn, clusterDomain)
	}

	n := len(labels)
	if n == 2+len(suffix) {
		// <service>.<namespace>.<suffix>
		service := watcher.ServiceID{
			Name:      labels[0],
			Namespace: labels[1],
		}
		return service, "", nil
	}

	if n == 3+len(suffix) {
		// <instance-id>.<service>.<namespace>.<suffix>
		instanceID := labels[0]
		service := watcher.ServiceID{
			Name:      labels[1],
			Namespace: labels[2],
		}
		return service, instanceID, nil
	}

	return watcher.ServiceID{}, "", fmt.Errorf("invalid k8s service %s", fqdn)
}

func hasSuffix(slice []string, suffix []string) bool {
	if len(slice) < len(suffix) {
		return false
	}
	for i, s := range slice[len(slice)-len(suffix):] {
		if s != suffix[i] {
			return false
		}
	}
	return true
}

func getPodSkippedInboundPortsAnnotations(pod *corev1.Pod) map[uint32]struct{} {
	annotation, ok := pod.Annotations[labels.ProxyIgnoreInboundPortsAnnotation]
	if !ok || annotation == "" {
		return nil
	}

	return util.ParsePorts(annotation)
}
