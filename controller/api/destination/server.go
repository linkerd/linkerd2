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
)

type (
	server struct {
		pb.UnimplementedDestinationServer

		endpoints   *watcher.EndpointsWatcher
		opaquePorts *watcher.OpaquePortsWatcher
		profiles    *watcher.ProfileWatcher
		servers     *watcher.ServerWatcher

		enableH2Upgrade     bool
		controllerNS        string
		identityTrustDomain string
		clusterDomain       string
		defaultOpaquePorts  map[uint32]struct{}

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
	controllerNS string,
	identityTrustDomain string,
	enableH2Upgrade bool,
	enableEndpointSlices bool,
	k8sAPI *k8s.API,
	metadataAPI *k8s.MetadataAPI,
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

	endpoints, err := watcher.NewEndpointsWatcher(k8sAPI, metadataAPI, log, enableEndpointSlices)
	if err != nil {
		return nil, err
	}
	opaquePorts, err := watcher.NewOpaquePortsWatcher(k8sAPI, log, defaultOpaquePorts)
	if err != nil {
		return nil, err
	}
	profiles, err := watcher.NewProfileWatcher(k8sAPI, log)
	if err != nil {
		return nil, err
	}
	servers, err := watcher.NewServerWatcher(k8sAPI, log)
	if err != nil {
		return nil, err
	}

	srv := server{
		pb.UnimplementedDestinationServer{},
		endpoints,
		opaquePorts,
		profiles,
		servers,
		enableH2Upgrade,
		controllerNS,
		identityTrustDomain,
		clusterDomain,
		defaultOpaquePorts,
		k8sAPI,
		metadataAPI,
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
		s.metadataAPI,
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
		var ise watcher.InvalidService
		if errors.As(err, &ise) {
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
	log.Debugf("Getting profile for %s with token %q", dest.GetPath(), dest.GetContextToken())

	// The host must be fully-qualified or be an IP address.
	host, port, err := getHostAndPort(dest.GetPath())
	if err != nil {
		log.Debugf("Invalid address %q", dest.GetPath())
		return status.Errorf(codes.InvalidArgument, "invalid authority: %q: %q", dest.GetPath(), err)
	}

	if ip := net.ParseIP(host); ip != nil {
		return s.getProfileByIP(dest.GetContextToken(), ip, port, stream)
	}

	return s.getProfileByName(dest.GetContextToken(), host, port, stream)
}

func (s *server) getProfileByIP(
	token string,
	ip net.IP,
	port uint32,
	stream pb.Destination_GetProfileServer,
) error {
	// Get the service that the IP currently maps to.
	svcID, err := getSvcID(s.k8sAPI, ip.String(), s.log)
	if err != nil {
		return err
	}

	if svcID == nil {
		// If the IP does not map to a service, check if it maps to a pod
		pod, err := getPodByIP(s.k8sAPI, ip.String(), port, s.log)
		if err != nil {
			return err
		}
		address, err := s.createAddress(pod, ip.String(), port)
		if err != nil {
			return fmt.Errorf("failed to create address: %w", err)
		}
		return s.subscribeToEndpointProfile(&address, port, stream)
	}

	fqn := fmt.Sprintf("%s.%s.svc.%s", svcID.Name, svcID.Namespace, s.clusterDomain)
	return s.subscribeToServiceProfile(*svcID, token, fqn, port, stream)
}

func (s *server) getProfileByName(
	token, host string,
	port uint32,
	stream pb.Destination_GetProfileServer,
) error {
	service, hostname, err := parseK8sServiceName(host, s.clusterDomain)
	if err != nil {
		s.log.Debugf("Invalid service %s", host)
		return status.Errorf(codes.InvalidArgument, "invalid service %q: %q", host, err)
	}

	// If the pod name (instance ID) is not empty, it means we parsed a DNS
	// name. When we fetch the profile using a pod's DNS name, we want to
	// return an endpoint in the profile response.
	if hostname != "" {
		address, err := s.getEndpointByHostname(s.k8sAPI, hostname, service, port)
		if err != nil {
			return fmt.Errorf("failed to get pod for hostname %s: %w", hostname, err)
		}
		return s.subscribeToEndpointProfile(address, port, stream)
	}

	return s.subscribeToServiceProfile(service, token, host, port, stream)
}

// Resolves a profile for a service, sending updates to the provided stream.
//
// This function does not return until the stream is closed.
func (s *server) subscribeToServiceProfile(
	service watcher.ID,
	token, fqn string,
	port uint32,
	stream pb.Destination_GetProfileServer,
) error {
	log := s.log.
		WithField("ns", service.Namespace).
		WithField("svc", service.Name).
		WithField("port", port)

	canceled := stream.Context().Done()

	// We build up the pipeline of profile updaters backwards, starting from
	// the translator which takes profile updates, translates them to protobuf
	// and pushes them onto the gRPC stream.
	translator := newProfileTranslator(stream, log, fqn, port)

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
	tok := s.parseContextToken(token)
	if tok.Ns == "" {
		return s.subscribeToServiceWithoutContext(fqn, listener, canceled, log)
	}
	return s.subscribeToServicesWithContext(fqn, tok, listener, canceled, log)
}

// subscribeToServiceWithContext establishes two profile watches: a "backup"
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
) error {
	// We ned to support two subscriptions:
	// - First, a backup subscription that assumes the context of the server
	//   namespace.
	// - And then, a primary subscription that assumes the context of the client
	//   namespace.
	primary, backup := newFallbackProfileListener(listener, log)

	// The backup lookup ignores the context token to lookup any
	// server-namespace-hosted profiles.
	backupID, err := profileID(fqn, contextToken{}, s.clusterDomain)
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

	primaryID, err := profileID(fqn, token, s.clusterDomain)
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
		log.Debug("Cancelled")
	}
	return nil
}

// subscribeToServiceWithoutContext establishes a single profile watch, assuming
// no client context. All udpates are published to the provided listener.
func (s *server) subscribeToServiceWithoutContext(
	fqn string,
	listener watcher.ProfileUpdateListener,
	cancel <-chan struct{},
	log *logging.Entry,
) error {
	id, err := profileID(fqn, contextToken{}, s.clusterDomain)
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
	case <-cancel:
		log.Debug("Cancelled")
	}
	return nil
}

// Resolves a profile for a single endpoitn, sending updates to the provided
// stream.
//
// This function does not return until the stream is closed.
func (s *server) subscribeToEndpointProfile(
	address *watcher.Address,
	port uint32,
	stream pb.Destination_GetProfileServer,
) error {
	log := s.log
	if address.Pod != nil {
		log = log.WithField("ns", address.Pod.Namespace).WithField("pod", address.Pod.Name)
	} else {
		log = log.WithField("ip", address.IP)
	}

	opaquePorts := getAnnotatedOpaquePorts(address.Pod, s.defaultOpaquePorts)
	var endpoint *pb.WeightedAddr
	endpoint, err := s.createEndpoint(*address, opaquePorts)
	if err != nil {
		return fmt.Errorf("failed to create endpoint: %w", err)
	}
	translator := newEndpointProfileTranslator(address.Pod, port, endpoint, stream, log)

	// If the endpoint's port is annotated as opaque, we don't need to
	// subscribe for updates because it will always be opaque
	// regardless of any Servers that may select it.
	if _, ok := opaquePorts[port]; ok {
		translator.UpdateProtocol(true)
	} else if address.Pod == nil {
		translator.UpdateProtocol(false)
	} else {
		translator.UpdateProtocol(address.OpaqueProtocol)
		s.servers.Subscribe(address.Pod, port, translator)
		defer s.servers.Unsubscribe(address.Pod, port, translator)
	}

	select {
	case <-s.shutdown:
	case <-stream.Context().Done():
		log.Debugf("Cancelled")
	}
	return nil
}

// getPortForPod returns the port that a `pod` is listening on.
//
// Proxies usually receive traffic targeting `podIp:containerPort`.
// However, they may be receiving traffic on `nodeIp:nodePort`. In this
// case, we convert the port to the containerPort for discovery. In k8s parlance,
// this is the 'HostPort' mapping.
func (s *server) getPortForPod(pod *corev1.Pod, targetIP string, port uint32) (uint32, error) {
	if pod == nil {
		return port, fmt.Errorf("getPortForPod passed a nil pod")
	}

	if net.ParseIP(targetIP) == nil {
		return port, fmt.Errorf("failed to parse hostIP into net.IP: %s", targetIP)
	}

	if containsIP(pod.Status.PodIPs, targetIP) {
		return port, nil
	}

	if targetIP == pod.Status.HostIP {
		for _, container := range pod.Spec.Containers {
			for _, containerPort := range container.Ports {
				if uint32(containerPort.HostPort) == port {
					return uint32(containerPort.ContainerPort), nil
				}
			}
		}
	}

	s.log.Warnf("unable to find container port as host (%s) matches neither PodIP nor HostIP (%s)", targetIP, pod)
	return port, nil
}

func (s *server) createAddress(pod *corev1.Pod, targetIP string, port uint32) (watcher.Address, error) {
	var ip, ownerKind, ownerName string
	var err error
	if pod != nil {
		ownerKind, ownerName, err = s.metadataAPI.GetOwnerKindAndName(context.Background(), pod, true)
		if err != nil {
			return watcher.Address{}, err
		}

		port, err = s.getPortForPod(pod, targetIP, port)
		if err != nil {
			return watcher.Address{}, fmt.Errorf("failed to find Port for Pod: %w", err)
		}

		ip = pod.Status.PodIP
	} else {
		ip = targetIP
	}

	address := watcher.Address{
		IP:        ip,
		Port:      port,
		Pod:       pod,
		OwnerName: ownerName,
		OwnerKind: ownerKind,
	}

	if address.Pod != nil {
		if err := watcher.SetToServerProtocol(s.k8sAPI, &address, port); err != nil {
			return watcher.Address{}, fmt.Errorf("failed to set address OpaqueProtocol: %w", err)
		}
	}

	return address, nil
}

func (s *server) createEndpoint(address watcher.Address, opaquePorts map[uint32]struct{}) (*pb.WeightedAddr, error) {
	weightedAddr, err := createWeightedAddr(address, opaquePorts, s.enableH2Upgrade, s.identityTrustDomain, s.controllerNS, s.log)
	if err != nil {
		return nil, err
	}

	// `Get` doesn't include the namespace in the per-endpoint
	// metadata, so it needs to be special-cased.
	if address.Pod != nil {
		weightedAddr.MetricLabels["namespace"] = address.Pod.Namespace
	}

	return weightedAddr, err
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

// getEndpointByHostname returns a pod that maps to the given hostname (or an
// instanceID). The hostname is generally the prefix of the pod's DNS name;
// since it may be arbitrary we need to look at the corresponding service's
// Endpoints object to see whether the hostname matches a pod.
func (s *server) getEndpointByHostname(k8sAPI *k8s.API, hostname string, svcID watcher.ServiceID, port uint32) (*watcher.Address, error) {
	ep, err := k8sAPI.Endpoint().Lister().Endpoints(svcID.Namespace).Get(svcID.Name)
	if err != nil {
		return nil, err
	}

	for _, subset := range ep.Subsets {
		for _, addr := range subset.Addresses {

			if hostname == addr.Hostname {
				if addr.TargetRef != nil && addr.TargetRef.Kind == "Pod" {
					podName := addr.TargetRef.Name
					podNamespace := addr.TargetRef.Namespace
					pod, err := k8sAPI.Pod().Lister().Pods(podNamespace).Get(podName)
					if err != nil {
						return nil, err
					}
					address, err := s.createAddress(pod, addr.IP, port)
					if err != nil {
						return nil, err
					}
					return &address, nil
				}
				return &watcher.Address{
					IP:   addr.IP,
					Port: port,
				}, nil

			}
		}
	}

	return nil, fmt.Errorf("no pod found in Endpoints %s/%s for hostname %s", svcID.Namespace, svcID.Name, hostname)
}

// getPodByIP returns a pod that maps to the given IP address. The pod can either
// be in the host network or the pod network. If the pod is in the host
// network, then it must have a container port that exposes `port` as a host
// port.
func getPodByIP(k8sAPI *k8s.API, podIP string, port uint32, log *logging.Entry) (*corev1.Pod, error) {
	// First we check if the address maps to a pod in the host network.
	addr := net.JoinHostPort(podIP, fmt.Sprintf("%d", port))
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
		return nil, fmt.Errorf("failed getting %s indexed pods: %w", indexName, err)
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

////////////
/// util ///
////////////

type contextToken struct {
	Ns       string `json:"ns,omitempty"`
	NodeName string `json:"nodeName,omitempty"`
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
	hostPort := strings.Split(authority, ":")
	if len(hostPort) > 2 {
		return "", 0, fmt.Errorf("invalid destination %s", authority)
	}
	host := hostPort[0]
	port := 80
	if len(hostPort) == 2 {
		var err error
		port, err = strconv.Atoi(hostPort[1])
		if err != nil || port <= 0 || port > 65535 {
			return "", 0, fmt.Errorf("invalid port %s", hostPort[1])
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

func getAnnotatedOpaquePorts(pod *corev1.Pod, defaultPorts map[uint32]struct{}) map[uint32]struct{} {
	if pod == nil {
		return defaultPorts
	}
	annotation, ok := pod.Annotations[labels.ProxyOpaquePortsAnnotation]
	if !ok {
		return defaultPorts
	}
	opaquePorts := make(map[uint32]struct{})
	if annotation != "" {
		for _, pr := range util.ParseContainerOpaquePorts(annotation, pod.Spec.Containers) {
			for _, port := range pr.Ports() {
				opaquePorts[uint32(port)] = struct{}{}
			}
		}
	}
	return opaquePorts
}

func getPodSkippedInboundPortsAnnotations(pod *corev1.Pod) (map[uint32]struct{}, error) {
	annotation, ok := pod.Annotations[labels.ProxyIgnoreInboundPortsAnnotation]
	if !ok || annotation == "" {
		return nil, nil
	}

	return util.ParsePorts(annotation)
}

// Given a list of PodIP, determine is `targetIP` is a member
func containsIP(podIPs []corev1.PodIP, targetIP string) bool {
	for _, ip := range podIPs {
		if ip.IP == targetIP {
			return true
		}
	}

	return false
}
