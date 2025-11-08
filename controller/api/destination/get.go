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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

func (s *server) Get(dest *pb.GetDestination, stream pb.Destination_GetServer) error {
	ctx := stream.Context()
	log := s.log.WithField("method", "Get")

	var token contextToken
	if dest.GetContextToken() != "" {
		log.Debugf("Dest token: %q", dest.GetContextToken())
		token = s.parseContextToken(dest.GetContextToken())
		log = log.
			WithField("context-pod", token.Pod).
			WithField("context-ns", token.Ns)
	}

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
	log = log.WithField("service", host).WithField("port", port)

	svc, err := s.k8sAPI.Svc().Lister().Services(service.Namespace).Get(service.Name)
	if err != nil {
		if kerrors.IsNotFound(err) {
			log.Debugf("Service not found %s", service)
			return status.Errorf(codes.NotFound, "Service %s.%s not found", service.Name, service.Namespace)
		}
		log.Debugf("Failed to get service %s: %v", service, err)
		return status.Errorf(codes.Internal, "Failed to get service %s", dest.GetPath())
	}
	if svc != nil && svc.Spec.Type == corev1.ServiceTypeExternalName {
		log.Debugf("ExternalName service %s not supported", dest.GetPath())
		return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
	}

	nodeTopologyZone, err := getNodeTopologyZone(s.metadataAPI, token.NodeName)
	if err != nil {
		log.Errorf("Failed to get node topology zone for node %s: %s", token.NodeName, err)
	}

	ctx, reset := context.WithCancel(ctx)
	dispatcher := newEndpointStreamDispatcher(s.config.StreamSendTimeout, reset)
	defer dispatcher.close()

	// Send updates on a dedicated task so that Send may block. This task
	// processes queued endpoint events, builds protobuf updates, and forwards
	// them to the client.
	go func() {
		if err := dispatcher.process(stream.Send); err != nil {
			log.Debugf("Failed to send update for %s: %s", dest.GetPath(), err)
		}
	}()

	if isFederatedService(svc) {
		remoteDiscovery := svc.Annotations[labels.RemoteDiscoveryAnnotation]
		localDiscovery := svc.Annotations[labels.LocalDiscoveryAnnotation]
		log.Debugf("Federated service discovery, remote:[%s] local:[%s]", remoteDiscovery, localDiscovery)
		err := s.federatedServices.Subscribe(ctx, svc.Name, svc.Namespace, port, token.NodeName, nodeTopologyZone, instanceID, dispatcher)
		if err != nil {
			log.Errorf("Failed to subscribe to federated service %q: %s", dest.GetPath(), err)
			return err
		}
	} else if cluster, found := svc.Labels[labels.RemoteDiscoveryLabel]; found {
		log.Debug("Remote discovery service detected")
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

		remoteService := fmt.Sprintf("%s.%s.svc.%s:%d", remoteSvc, service.Namespace, remoteConfig.ClusterDomain, port)
		viewCfg := newEndpointTranslatorConfig(
			&s.config,
			remoteConfig.TrustDomain,
			token.NodeName,
			nodeTopologyZone,
			remoteService,
			false, // endpoint filtering is disabled remotely
		)

		topic, err := remoteWatcher.Topic(watcher.ServiceID{Namespace: service.Namespace, Name: remoteSvc}, port, instanceID)
		if err != nil {
			var ise watcher.InvalidService
			if errors.As(err, &ise) {
				log.Debugf("Invalid remote discovery service %s", dest.GetPath())
				return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
			}
			log.Errorf("Failed to resolve topic for remote discovery service %q in cluster %s: %s", dest.GetPath(), cluster, err)
			return err
		}

		view, err := dispatcher.newEndpointView(ctx, topic, viewCfg, log)
		if err != nil {
			log.Errorf("Failed to create endpoint view for remote discovery service %q: %s", dest.GetPath(), err)
			return status.Errorf(codes.Internal, "Failed to create endpoint view: %s", err)
		}
		defer view.Close()
	} else {
		log.Debug("Local discovery service detected")
		localCfg := newEndpointTranslatorConfig(
			&s.config,
			s.config.IdentityTrustDomain,
			token.NodeName,
			nodeTopologyZone,
			dest.GetPath(),
			true, // endpoint filtering is enabled locally
		)

		topic, err := s.endpoints.Topic(service, port, instanceID)
		if err != nil {
			var ise watcher.InvalidService
			if errors.As(err, &ise) {
				log.Debugf("Invalid service %s", dest.GetPath())
				return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
			}
			log.Errorf("Failed to resolve topic for %s: %s", dest.GetPath(), err)
			return status.Errorf(codes.Internal, "Failed to resolve topic for %s", dest.GetPath())
		}

		view, err := dispatcher.newEndpointView(ctx, topic, localCfg, log)
		if err != nil {
			log.Errorf("Failed to create endpoint view for %s: %s", dest.GetPath(), err)
			return status.Errorf(codes.Internal, "Failed to create endpoint view: %s", err)
		}
		defer view.Close()
	}

	select {
	case <-s.shutdown:
	case <-ctx.Done():
		log.Debugf("Reset")
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
