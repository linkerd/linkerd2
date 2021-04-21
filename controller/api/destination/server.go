package destination

import (
	"context"
	"encoding/json"
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
	coreinformers "k8s.io/client-go/informers/core/v1"
)

type (
	server struct {
		endpoints     *watcher.EndpointsWatcher
		opaquePorts   *watcher.OpaquePortsWatcher
		profiles      *watcher.ProfileWatcher
		trafficSplits *watcher.TrafficSplitWatcher
		nodes         coreinformers.NodeInformer

		enableH2Upgrade     bool
		controllerNS        string
		identityTrustDomain string
		clusterDomain       string
		defaultOpaquePorts  map[uint32]struct{}

		k8sAPI   *k8s.API
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
	enableEndpointSlices bool,
	k8sAPI *k8s.API,
	clusterDomain string,
	defaultOpaquePorts map[uint32]struct{},
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

	endpoints := watcher.NewEndpointsWatcher(k8sAPI, log, enableEndpointSlices)
	opaquePorts := watcher.NewOpaquePortsWatcher(k8sAPI, log, defaultOpaquePorts)
	profiles := watcher.NewProfileWatcher(k8sAPI, log)
	trafficSplits := watcher.NewTrafficSplitWatcher(k8sAPI, log)

	srv := server{
		endpoints,
		opaquePorts,
		profiles,
		trafficSplits,
		k8sAPI.Node(),
		enableH2Upgrade,
		controllerNS,
		identityTrustDomain,
		clusterDomain,
		defaultOpaquePorts,
		k8sAPI,
		log,
		shutdown,
	}

	s := prometheus.NewGrpcServer()
	// linkerd2-proxy-api/destination.Destination (proxy-facing)
	pb.RegisterDestinationServer(s, &srv)
	return s, nil
}

func (s *server) Get(dest *pb.GetDestination, stream pb.Destination_GetServer) error {
	client, _ := peer.FromContext(stream.Context())
	log := s.log
	if client != nil {
		log = s.log.WithField("remote", client.Addr)
	}
	log.Debugf("Get %s", dest.GetPath())

	var token contextToken
	if dest.GetContextToken() != "" {
		token = s.parseContextToken(dest.GetContextToken())
		log.Debugf("Dest token: %v", token)
	}

	translator := newEndpointTranslator(
		s.controllerNS,
		s.identityTrustDomain,
		s.enableH2Upgrade,
		dest.GetPath(),
		token.NodeName,
		s.defaultOpaquePorts,
		s.nodes,
		stream,
		log,
	)

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

	service, instanceID, err := parseK8sServiceName(host, s.clusterDomain)
	if err != nil {
		log.Debugf("Invalid service %s", dest.GetPath())
		return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
	}

	err = s.endpoints.Subscribe(service, port, instanceID, translator)
	if err != nil {
		if _, ok := err.(watcher.InvalidService); ok {
			log.Debugf("Invalid service %s", dest.GetPath())
			return status.Errorf(codes.InvalidArgument, "Invalid authority: %s", dest.GetPath())
		}
		log.Errorf("Failed to subscribe to %s: %s", dest.GetPath(), err)
		return err
	}
	defer s.endpoints.Unsubscribe(service, port, instanceID, translator)

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

	path := dest.GetPath()
	// The host must be fully-qualified or be an IP address.
	host, port, err := getHostAndPort(path)
	if err != nil {
		log.Debugf("Invalid authority %s", path)
		return status.Errorf(codes.InvalidArgument, "invalid authority: %s", err)
	}

	// The stream will subscribe to profile updates for `service`.
	var service watcher.ServiceID
	// If `host` is an IP, `fqn` must be constructed from the namespace and
	// name of the service that the IP maps to.
	var fqn string

	if ip := net.ParseIP(host); ip != nil {
		// Get the service that the IP currently maps to.
		svcID, err := getSvcID(s.k8sAPI, ip.String(), log)
		if err != nil {
			return err
		}
		if svcID != nil {
			service = *svcID
			fqn = fmt.Sprintf("%s.%s.svc.%s", service.Name, service.Namespace, s.clusterDomain)
		} else {
			// If the IP does not map to a service, check if it maps to a pod
			pod, err := getPod(s.k8sAPI, ip.String(), port, log)
			if err != nil {
				return err
			}
			var endpoint *pb.WeightedAddr
			opaquePorts := make(map[uint32]struct{})
			if pod != nil {
				// If the IP maps to a pod, we create a single endpoint and
				// return it in the DestinationProfile response
				podSet := podToAddressSet(s.k8sAPI, pod).WithPort(port)
				podID := watcher.PodID{
					Namespace: pod.Namespace,
					Name:      pod.Name,
				}
				if err != nil {
					log.Errorf("failed to set opaque port annotation on pod: %s", err)
				}
				var ok bool
				opaquePorts, ok, err = getPodOpaquePortsAnnotations(pod)
				if err != nil {
					log.Errorf("failed to get opaque ports annotation for pod: %s", err)
				}
				// If the opaque ports annotation was not set, then set the
				// endpoint's opaque ports to the default value.
				if !ok {
					opaquePorts = s.defaultOpaquePorts
				}

				skippedInboundPorts, err := getPodSkippedInboundPortsAnnotations(pod)
				if err != nil {
					log.Errorf("failed to get ignored inbound ports annotation for pod: %s", err)
				}

				endpoint, err = toWeightedAddr(podSet.Addresses[podID], opaquePorts, skippedInboundPorts, s.enableH2Upgrade, s.identityTrustDomain, s.controllerNS, log)
				if err != nil {
					return err
				}
				// `Get` doesn't include the namespace in the per-endpoint
				// metadata, so it needs to be special-cased.
				endpoint.MetricLabels["namespace"] = pod.Namespace
			}

			// When the IP does not map to a service, the default profile is
			// sent without subscribing for future updates. If the IP mapped
			// to a pod, then the endpoint will be set in the response.
			translator := newProfileTranslator(stream, log, "", port, endpoint)

			// If there are opaque ports then update the profile translator
			// with a service profile that has those values
			//
			// TODO: Remove endpoint from profileTranslator and set it here
			// similar to opaque ports
			if len(opaquePorts) != 0 {
				sp := sp.ServiceProfile{}
				sp.Spec.OpaquePorts = opaquePorts
				translator.Update(&sp)
			} else {
				translator.Update(nil)
			}

			select {
			case <-s.shutdown:
			case <-stream.Context().Done():
				log.Debugf("GetProfile(%+v) cancelled", dest)
			}

			return nil
		}
	} else {
		service, _, err = parseK8sServiceName(host, s.clusterDomain)
		if err != nil {
			log.Debugf("Invalid service %s", path)
			return status.Errorf(codes.InvalidArgument, "invalid service: %s", err)
		}
		fqn = host
	}

	// We build up the pipeline of profile updaters backwards, starting from
	// the translator which takes profile updates, translates them to protobuf
	// and pushes them onto the gRPC stream.
	translator := newProfileTranslator(stream, log, fqn, port, nil)

	// The traffic split adaptor merges profile updates with traffic split
	// updates and publishes the result to the profile translator.
	tsAdaptor := newTrafficSplitAdaptor(translator, service, port, s.clusterDomain)

	// Subscribe the adaptor to traffic split updates.
	err = s.trafficSplits.Subscribe(service, tsAdaptor)
	if err != nil {
		log.Warnf("Failed to subscribe to traffic split for %s: %s", path, err)
		return err
	}
	defer s.trafficSplits.Unsubscribe(service, tsAdaptor)

	// The opaque ports adaptor merges profile updates with service opaque
	// port annotation updates; it then publishes the result to the traffic
	// split adaptor.
	opaquePortsAdaptor := newOpaquePortsAdaptor(tsAdaptor)

	// Subscribe the adaptor to service updates.
	err = s.opaquePorts.Subscribe(service, opaquePortsAdaptor)
	if err != nil {
		log.Warnf("Failed to subscribe to service updates for %s: %s", service, err)
		return err
	}
	defer s.opaquePorts.Unsubscribe(service, opaquePortsAdaptor)

	// The fallback accepts updates from a primary and secondary source and
	// passes the appropriate profile updates to the adaptor.
	primary, secondary := newFallbackProfileListener(opaquePortsAdaptor)

	// If we have a context token, we create two subscriptions: one with the
	// context token which sends updates to the primary listener and one without
	// the context token which sends updates to the secondary listener.  It is
	// up to the fallbackProfileListener to merge updates from the primary and
	// secondary listeners and send the appropriate updates to the stream.
	if dest.GetContextToken() != "" {
		ctxToken := s.parseContextToken(dest.GetContextToken())

		profile, err := profileID(fqn, ctxToken, s.clusterDomain)
		if err != nil {
			log.Debugf("Invalid service %s", path)
			return status.Errorf(codes.InvalidArgument, "invalid profile ID: %s", err)
		}

		err = s.profiles.Subscribe(profile, primary)
		if err != nil {
			log.Warnf("Failed to subscribe to profile %s: %s", path, err)
			return err
		}
		defer s.profiles.Unsubscribe(profile, primary)
	}

	profile, err := profileID(fqn, contextToken{}, s.clusterDomain)
	if err != nil {
		log.Debugf("Invalid service %s", path)
		return status.Errorf(codes.InvalidArgument, "invalid profile ID: %s", err)
	}
	err = s.profiles.Subscribe(profile, secondary)
	if err != nil {
		log.Warnf("Failed to subscribe to profile %s: %s", path, err)
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

// getPod returns a pod that maps to the given IP address. The pod can either
// be in the host network or the pod network. If the pod is in the host
// network, then it must have a container port that exposes `port` as a host
// port.
func getPod(k8sAPI *k8s.API, podIP string, port uint32, log *logging.Entry) (*corev1.Pod, error) {
	// First we check if the address maps to a pod in the host network.
	addr := fmt.Sprintf("%s:%d", podIP, port)
	hostIPPods, err := getIndexedPods(k8sAPI, watcher.HostIPIndex, addr)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if len(hostIPPods) == 1 {
		log.Debugf("found %s:%d on the host network", podIP, port)
		return hostIPPods[0], nil
	}
	if len(hostIPPods) > 1 {
		conflictingPods := []string{}
		for _, pod := range hostIPPods {
			conflictingPods = append(conflictingPods, fmt.Sprintf("%s:%s", pod.Namespace, pod.Name))
		}
		log.Warnf("found conflicting %s:%d endpoint on the host network: %s", podIP, port, strings.Join(conflictingPods, ","))
		return nil, status.Errorf(codes.FailedPrecondition, "found %d pods with a conflicting host network endpoint %s:%d", len(hostIPPods), podIP, port)
	}

	// The address did not map to a pod in the host network, so now we check
	// if the IP maps to a pod IP in the pod network.
	podIPPods, err := getIndexedPods(k8sAPI, watcher.PodIPIndex, podIP)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}
	if len(podIPPods) == 1 {
		log.Debugf("found %s on the pod network", podIP)
		return podIPPods[0], nil
	}
	if len(podIPPods) > 1 {
		conflictingPods := []string{}
		for _, pod := range podIPPods {
			conflictingPods = append(conflictingPods, fmt.Sprintf("%s:%s", pod.Namespace, pod.Name))
		}
		log.Warnf("found conflicting %s IP on the pod network: %s", podIP, strings.Join(conflictingPods, ","))
		return nil, status.Errorf(codes.FailedPrecondition, "found %d pods with a conflicting pod network IP %s", len(podIPPods), podIP)
	}

	log.Debugf("no pod found for %s:%d", podIP, port)
	return nil, nil
}

func getIndexedPods(k8sAPI *k8s.API, indexName string, podIP string) ([]*corev1.Pod, error) {
	objs, err := k8sAPI.Pod().Informer().GetIndexer().ByIndex(indexName, podIP)
	if err != nil {
		return nil, fmt.Errorf("failed getting %s indexed pods: %s", indexName, err)
	}
	pods := make([]*corev1.Pod, 0)
	for _, obj := range objs {
		pod := obj.(*corev1.Pod)
		if !podReceivingTraffic(pod) {
			continue
		}
		pods = append(pods, pod)
	}
	return pods, nil
}

func podReceivingTraffic(pod *corev1.Pod) bool {
	phase := pod.Status.Phase
	podTerminated := phase == corev1.PodSucceeded || phase == corev1.PodFailed
	podTerminating := pod.DeletionTimestamp != nil

	return !podTerminating && !podTerminated
}

// podToAddressSet converts a Pod spec into a set of Addresses.
func podToAddressSet(k8sAPI *k8s.API, pod *corev1.Pod) *watcher.AddressSet {
	ownerKind, ownerName := k8sAPI.GetOwnerKindAndName(context.Background(), pod, true)
	return &watcher.AddressSet{
		Addresses: map[watcher.PodID]watcher.Address{
			{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			}: {
				IP:        pod.Status.PodIP,
				Port:      0, // Will be set by individual subscriptions
				Pod:       pod,
				OwnerName: ownerName,
				OwnerKind: ownerKind,
			},
		},
		Labels: map[string]string{"namespace": pod.Namespace},
	}
}

////////////
/// util ///
////////////

type contextToken struct {
	Ns       string `json:"ns,omitempty"`
	NodeName string `json:"nodeName,omitempty"`
}

func (s *server) parseContextToken(token string) contextToken {
	ctxToken := contextToken{}
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
		return watcher.ProfileID{}, fmt.Errorf("invalid authority: %s", err)
	}
	service, _, err := parseK8sServiceName(host, clusterDomain)
	if err != nil {
		return watcher.ProfileID{}, fmt.Errorf("invalid k8s service name: %s", err)
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
	hostPort := strings.Split(authority, ":")
	if len(hostPort) > 2 {
		return "", 0, fmt.Errorf("Invalid destination %s", authority)
	}
	host := hostPort[0]
	port := 80
	if len(hostPort) == 2 {
		var err error
		port, err = strconv.Atoi(hostPort[1])
		if err != nil {
			return "", 0, fmt.Errorf("Invalid port %s", hostPort[1])
		}
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

func getPodOpaquePortsAnnotations(pod *corev1.Pod) (map[uint32]struct{}, bool, error) {
	annotation, ok := pod.Annotations[labels.ProxyOpaquePortsAnnotation]
	if !ok {
		return nil, false, nil
	}
	opaquePorts := make(map[uint32]struct{})
	if annotation != "" {
		for _, portStr := range util.ParseContainerOpaquePorts(annotation, pod.Spec.Containers) {
			port, err := strconv.ParseUint(portStr, 10, 32)
			if err != nil {
				return nil, true, err
			}
			opaquePorts[uint32(port)] = struct{}{}
		}
	}
	return opaquePorts, true, nil
}

func getPodSkippedInboundPortsAnnotations(pod *corev1.Pod) (map[uint32]struct{}, error) {
	annotation, ok := pod.Annotations[labels.ProxyIgnoreInboundPortsAnnotation]
	if !ok || annotation == "" {
		return nil, nil
	}

	return util.ParsePorts(annotation)
}
