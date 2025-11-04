package destination

import (
	"errors"
	"fmt"
	"net"
	"strings"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
)

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
	translator, err := newProfileTranslatorWithCapacity(service, stream, log, fqn, port, streamEnd, s.config.StreamQueueCapacity)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to create profile translator: %s", err)
	}
	translator.Start()
	defer translator.Stop()

	// The opaque ports adaptor merges profile updates with service opaque
	// port annotation updates; it then publishes the result to the traffic
	// split adaptor.
	opaquePortsAdaptor := newOpaquePortsAdaptor(translator)

	// Create an adaptor that merges service-level opaque port configurations
	// onto profile updates.
	err = s.opaquePorts.Subscribe(service, opaquePortsAdaptor)
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
		s.config.ForceOpaqueTransport,
		s.config.EnableH2Upgrade,
		s.config.ControllerNS,
		s.config.IdentityTrustDomain,
		s.config.DefaultOpaquePorts,
		s.config.MeshedHttp2ClientParams,
		stream,
		streamEnd,
		log,
		s.config.StreamQueueCapacity,
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
