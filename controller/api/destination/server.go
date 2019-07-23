package destination

import (
	"context"
	"fmt"
	"strings"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	discoveryPb "github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type (
	server struct {
		endpoints     *watcher.EndpointsWatcher
		profiles      *watcher.ProfileWatcher
		trafficSplits *watcher.TrafficSplitWatcher

		enableH2Upgrade     bool
		controllerNS        string
		identityTrustDomain string
		clusterDomain       string

		log      *logging.Entry
		shutdown <-chan struct{}
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
	controllerNS string,
	identityTrustDomain string,
	enableH2Upgrade bool,
	k8sAPI *k8s.API,
	clusterDomain string,
	shutdown <-chan struct{},
) *grpc.Server {
	log := logging.WithFields(logging.Fields{
		"addr":      addr,
		"component": "server",
	})
	endpoints := watcher.NewEndpointsWatcher(k8sAPI, log)
	profiles := watcher.NewProfileWatcher(k8sAPI, log)
	trafficSplits := watcher.NewTrafficSplitWatcher(k8sAPI, log)

	srv := server{
		endpoints,
		profiles,
		trafficSplits,
		enableH2Upgrade,
		controllerNS,
		identityTrustDomain,
		clusterDomain,
		log,
		shutdown,
	}

	s := prometheus.NewGrpcServer()
	// linkerd2-proxy-api/destination.Destination (proxy-facing)
	pb.RegisterDestinationServer(s, &srv)
	// controller/discovery.Discovery (controller-facing)
	discoveryPb.RegisterDiscoveryServer(s, &srv)
	return s
}

func (s *server) Get(dest *pb.GetDestination, stream pb.Destination_GetServer) error {
	client, _ := peer.FromContext(stream.Context())
	log := s.log
	if client != nil {
		log = s.log.WithField("remote", client.Addr)
	}
	log.Debugf("Get %s", dest.GetPath())

	service, port, hostname, err := watcher.GetServiceAndPort(dest.GetPath(), s.clusterDomain)
	if err != nil {
		log.Errorf("Invalid authority %s", dest.GetPath())
		return err
	}

	translator := newEndpointTranslator(
		s.controllerNS,
		s.identityTrustDomain,
		s.enableH2Upgrade,
		service,
		stream,
		log,
	)

	err = s.endpoints.Subscribe(service, port, hostname, translator)
	if err != nil {
		log.Errorf("Failed to subscribe to %s: %s", dest.GetPath(), err)
		return err
	}
	defer s.endpoints.Unsubscribe(service, port, hostname, translator)

	select {
	case <-s.shutdown:
	case <-stream.Context().Done():
		log.Debugf("Get %s cancelled", dest.GetPath())
	}

	return nil
}

func (s *server) GetProfile(dest *pb.GetDestination, stream pb.Destination_GetProfileServer) error {
	log := s.log
	client, _ := peer.FromContext(stream.Context())
	if client != nil {
		log = log.WithField("remote", client.Addr)
	}
	log.Debugf("GetProfile(%+v)", dest)

	// We build up the pipeline of profile updaters backwards, starting from
	// the translator which takes profile updates, translates them to protobuf
	// and pushes them onto the gRPC stream.
	translator := newProfileTranslator(stream, log)

	service, port, _, err := watcher.GetServiceAndPort(dest.GetPath(), s.clusterDomain)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
	}

	// The adaptor merges profile updates with traffic split updates and
	// publishes the result to the translator.
	tsAdaptor := newTrafficSplitAdaptor(translator, service, port)

	// Subscribe the adaptor to traffic split updates.
	err = s.trafficSplits.Subscribe(service, tsAdaptor)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "Invalid authority [%s]: %s", dest.GetPath(), err)
	}
	defer s.trafficSplits.Unsubscribe(service, tsAdaptor)

	// The fallback accepts updates from a primary and secondary source and
	// passes the appropriate profile updates to the adaptor.
	primary, secondary := newFallbackProfileListener(tsAdaptor)

	// If we have a context token, we create two subscriptions: one with the
	// context token which sends updates to the primary listener and one without
	// the context token which sends updates to the secondary listener.  It is
	// up to the fallbackProfileListener to merge updates from the primary and
	// secondary listeners and send the appropriate updates to the stream.
	if dest.GetContextToken() != "" {
		profile, err := profileID(dest.GetPath(), dest.GetContextToken(), s.clusterDomain)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
		}

		err = s.profiles.Subscribe(profile, primary)
		if err != nil {
			log.Warnf("Failed to subscribe to profile %s: %s", dest.GetPath(), err)
			return err
		}
		defer s.profiles.Unsubscribe(profile, primary)
	}

	profile, err := profileID(dest.GetPath(), "", s.clusterDomain)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
	}
	err = s.profiles.Subscribe(profile, secondary)
	if err != nil {
		log.Warnf("Failed to subscribe to profile %s: %s", dest.GetPath(), err)
		return err
	}
	defer s.profiles.Unsubscribe(profile, secondary)

	select {
	case <-s.shutdown:
	case <-stream.Context().Done():
		log.Debugf("GetProfile(%+v) cancelled", dest)
	}

	return nil
}

func (s *server) Endpoints(ctx context.Context, params *discoveryPb.EndpointsParams) (*discoveryPb.EndpointsResponse, error) {
	s.log.Debugf("serving endpoints request")
	return nil, status.Error(codes.Unimplemented, "Not implemented")
}

////////////
/// util ///
////////////

func nsFromToken(token string) string {
	// ns:<namespace>
	parts := strings.Split(token, ":")
	if len(parts) == 2 && parts[0] == "ns" {
		return parts[1]
	}

	return ""
}

func profileID(authority string, contextToken string, clusterDomain string) (watcher.ProfileID, error) {
	service, _, _, err := watcher.GetServiceAndPort(authority, clusterDomain)
	if err != nil {
		return watcher.ProfileID{}, err
	}
	id := watcher.ProfileID{
		Name:      fmt.Sprintf("%s.%s.svc.%s", service.Name, service.Namespace, clusterDomain),
		Namespace: service.Namespace,
	}
	if contextNs := nsFromToken(contextToken); contextNs != "" {
		id.Namespace = contextNs
	}
	return id, nil
}
