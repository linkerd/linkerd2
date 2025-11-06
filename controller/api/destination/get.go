package destination

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	labels "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/util"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

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
	updateQueue := newDestinationUpdateQueue(s.config.StreamQueueCapacity, streamEnd, log)
	queueStream := newQueueingGetServer(stream, updateQueue)
	forwardErrCh := make(chan error, 1)
	go func() {
		forwardErrCh <- updateQueue.Forward(stream.Context(), stream)
	}()
	defer func() {
		updateQueue.Close()
		if forwardErrCh != nil {
			if err := <-forwardErrCh; err != nil && !errors.Is(err, context.Canceled) {
				log.Debugf("Get stream forwarder exited: %v", err)
			}
		}
	}()
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

	if isFederatedService(svc) {
		// Federated service
		remoteDiscovery := svc.Annotations[labels.RemoteDiscoveryAnnotation]
		localDiscovery := svc.Annotations[labels.LocalDiscoveryAnnotation]
		log.Debugf("Federated service discovery, remote:[%s] local:[%s]", remoteDiscovery, localDiscovery)
		err := s.federatedServices.Subscribe(svc.Name, svc.Namespace, port, token.NodeName, instanceID, queueStream, streamEnd)
		if err != nil {
			log.Errorf("Failed to subscribe to federated service %q: %s", dest.GetPath(), err)
			return err
		}
		defer s.federatedServices.Unsubscribe(svc.Name, svc.Namespace, queueStream)
	} else if cluster, found := svc.Labels[labels.RemoteDiscoveryLabel]; found {
		log.Debug("Remote discovery service detected")
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
		translator, err := newEndpointTranslator(
			s.config.ControllerNS,
			remoteConfig.TrustDomain,
			s.config.ForceOpaqueTransport,
			s.config.EnableH2Upgrade,
			false, // Disable endpoint filtering for remote discovery.
			s.config.EnableIPv6,
			s.config.ExtEndpointZoneWeights,
			s.config.MeshedHttp2ClientParams,
			fmt.Sprintf("%s.%s.svc.%s:%d", remoteSvc, service.Namespace, remoteConfig.ClusterDomain, port),
			token.NodeName,
			s.config.DefaultOpaquePorts,
			s.metadataAPI,
			queueStream,
			streamEnd,
			log,
			s.config.StreamQueueCapacity,
		)
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to create endpoint translator: %s", err)
		}
		err = remoteWatcher.Subscribe(watcher.ServiceID{Namespace: service.Namespace, Name: remoteSvc}, port, instanceID, translator)
		if err != nil {
			var ise watcher.InvalidService
			if errors.As(err, &ise) {
				log.Debugf("Invalid remote discovery service %s", dest.GetPath())
				return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
			}
			log.Errorf("Failed to subscribe to remote discovery service %q in cluster %s: %s", dest.GetPath(), cluster, err)
			return err
		}
		defer remoteWatcher.Unsubscribe(watcher.ServiceID{Namespace: service.Namespace, Name: remoteSvc}, port, instanceID, translator)

	} else {
		log.Debug("Local discovery service detected")
		// Local discovery
		translator, err := newEndpointTranslator(
			s.config.ControllerNS,
			s.config.IdentityTrustDomain,
			s.config.ForceOpaqueTransport,
			s.config.EnableH2Upgrade,
			true,
			s.config.EnableIPv6,
			s.config.ExtEndpointZoneWeights,
			s.config.MeshedHttp2ClientParams,
			dest.GetPath(),
			token.NodeName,
			s.config.DefaultOpaquePorts,
			s.metadataAPI,
			queueStream,
			streamEnd,
			log,
			s.config.StreamQueueCapacity,
		)
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to create endpoint translator: %s", err)
		}
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
