package destination

import (
	"fmt"
	"strconv"
	"sync"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
)

// hostPortAdaptor receives events from a podWatcher and if required,
// subscribes to associated Server updates. Pod updates are only taken into
// account to the extent they imply a change in its readiness. It translates
// protocol updates to DestinationProfiles for endpoints. When a Server on the
// cluster is updated it is possible that it selects an endpoint that is being
// watched; if that is the case then an update will be sent to the client if
// the Server has changed the endpoint's supported protocol—mainly being opaque
// or not.
type endpointProfileTranslator struct {
	servers    *watcher.ServerWatcher
	ip         string
	port       uint32
	stream     pb.Destination_GetProfileServer
	address    *watcher.Address
	endpoint   *pb.WeightedAddr
	subscribed bool
	podReady   bool

	enableH2Upgrade     bool
	controllerNS        string
	identityTrustDomain string
	defaultOpaquePorts  map[uint32]struct{}

	k8sAPI      *k8s.API
	metadataAPI *k8s.MetadataAPI
	metrics     prometheus.Gauge
	log         *logrus.Entry

	mu sync.Mutex
}

// hostIPMetrics is a prometheus gauge shared amongst endpointProfileTranslator instances
var hostIPMetrics = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "host_port_subscribers",
		Help: "Counter of subscribes to Pod changes for a given hostIP and port",
	},
	[]string{"hostIP", "port"},
)

func newEndpointProfileTranslator(
	k8sAPI *k8s.API,
	metadataAPI *k8s.MetadataAPI,
	servers *watcher.ServerWatcher,
	enableH2Upgrade bool,
	controllerNS string,
	identityTrustDomain string,
	defaultOpaquePorts map[uint32]struct{},
	ip string,
	port uint32,
	stream pb.Destination_GetProfileServer,
	address *watcher.Address,
	log *logrus.Entry,
) *endpointProfileTranslator {
	log = log.WithField("component", "endpointprofile-translator").WithField("ip", ip)

	podReady := isRunningAndReady(address.Pod)

	// if the label map has already been created, it'll get reused
	metrics := hostIPMetrics.With(prometheus.Labels{
		"hostIP": ip,
		"port":   strconv.FormatUint(uint64(port), 10),
	})

	return &endpointProfileTranslator{
		servers:             servers,
		ip:                  ip,
		port:                port,
		stream:              stream,
		address:             address,
		defaultOpaquePorts:  defaultOpaquePorts,
		podReady:            podReady,
		enableH2Upgrade:     enableH2Upgrade,
		controllerNS:        controllerNS,
		identityTrustDomain: identityTrustDomain,
		k8sAPI:              k8sAPI,
		metadataAPI:         metadataAPI,
		metrics:             metrics,
		log:                 log,
	}
}

func (ept *endpointProfileTranslator) Sync() error {
	ept.mu.Lock()
	defer ept.mu.Unlock()

	opaquePorts := getAnnotatedOpaquePorts(ept.address.Pod, ept.defaultOpaquePorts)
	endpoint, err := ept.createEndpoint(opaquePorts)
	if err != nil {
		return fmt.Errorf("failed to create endpoint: %w", err)
	}
	ept.endpoint = endpoint
	ept.log.Debugf("Sync for endpoint %s", ept.endpoint)
	ept.subscribed = false

	// If the endpoint's port is annotated as opaque, we don't need to
	// subscribe for updates because it will always be opaque
	// regardless of any Servers that may select it.
	if _, ok := opaquePorts[ept.port]; ok {
		ept.UpdateProtocol(true)
	} else if ept.address.Pod == nil {
		ept.UpdateProtocol(false)
	} else {
		ept.UpdateProtocol(ept.address.OpaqueProtocol)
		ept.servers.Subscribe(ept.address.Pod, ept.port, ept)
		ept.subscribed = true
	}

	return nil
}

func (ept *endpointProfileTranslator) Clean() {
	ept.mu.Lock()
	defer ept.mu.Unlock()

	if ept.subscribed {
		ept.log.Debugf("Clean for endpoint %s", ept.endpoint)
		ept.servers.Unsubscribe(ept.address.Pod, ept.port, ept)
		ept.subscribed = false
	}
}

// UpdateProtocol is part of the ServerUpdateListener interface
func (ept *endpointProfileTranslator) UpdateProtocol(opaqueProtocol bool) {
	// The protocol for an endpoint should only be updated if there is a pod,
	// endpoint, and the endpoint has a protocol hint. If there is an endpoint
	// but it does not have a protocol hint, that means we could not determine
	// if it has a peer proxy so a opaque traffic would not be supported.
	if ept.address.Pod != nil && ept.endpoint != nil && ept.endpoint.ProtocolHint != nil {
		if !opaqueProtocol {
			ept.endpoint.ProtocolHint.OpaqueTransport = nil
		} else if ept.endpoint.ProtocolHint.OpaqueTransport == nil {
			port, err := getInboundPort(&ept.address.Pod.Spec)
			if err != nil {
				ept.log.Error(err)
			} else {
				ept.endpoint.ProtocolHint.OpaqueTransport = &pb.ProtocolHint_OpaqueTransport{
					InboundPort: port,
				}
			}
		}

	}
	profile := ept.createDefaultProfile(ept.endpoint, opaqueProtocol)
	ept.log.Debugf("sending protocol update: %+v", profile)
	if err := ept.stream.Send(profile); err != nil {
		ept.log.Errorf("failed to send protocol update: %s", err)
	}
}

// Update is part of the PodUpdateListener interface. As an informer event
// handler, all operations should be non-blocking
func (ept *endpointProfileTranslator) Update(pod *v1.Pod) {
	ept.mu.Lock()
	defer ept.mu.Unlock()

	if !ept.matchesIPPort(pod) {
		return
	}

	if ept.podReady && ept.address.Pod.UID != pod.UID {
		ept.log.Tracef("Current pod still ready, ignoring event on %s.%s", pod.Name, pod.Namespace)
		return
	}

	if ept.podReady && !isRunningAndReady(pod) {
		ept.log.Debugf("Pod %s.%s became unready - remove", pod.Name, pod.Namespace)
		go ept.updateAddress(nil)
		return
	}

	if !ept.podReady && !isRunningAndReady(pod) {
		ept.log.Tracef("Ignore event on %s.%s until it becomes ready", pod.Name, pod.Namespace)
		return
	}

	if !ept.podReady && isRunningAndReady(pod) {
		ept.log.Debugf("Pod %s.%s became ready", pod.Name, pod.Namespace)
		go ept.updateAddress(pod)
		return
	}

	ept.log.Tracef("Ignored event on pod %s.%s", pod.Name, pod.Namespace)
}

// Remove is part of the PodUpdateListener interface. As an informer event
// handler, all operations should be non-blocking
func (ept *endpointProfileTranslator) Remove(pod *v1.Pod) {
	ept.mu.Lock()
	defer ept.mu.Unlock()

	if !ept.matchesIPPort(pod) {
		return
	}

	if ept.address.Pod == nil {
		ept.log.Tracef("Pod %s.%s already removed; discard event", pod.Name, pod.Namespace)
		return
	}

	ept.log.Debugf("Remove pod %s.%s", pod.Name, pod.Namespace)
	go ept.updateAddress(nil)
}

// MetricsInc is part of the PodUpdateListener interface
func (ept *endpointProfileTranslator) MetricsInc() {
	ept.metrics.Inc()
}

// MetricsDec is part of the PodUpdateListener interface
func (ept *endpointProfileTranslator) MetricsDec() {
	ept.metrics.Dec()
}

func (ept *endpointProfileTranslator) createEndpoint(opaquePorts map[uint32]struct{}) (*pb.WeightedAddr, error) {
	weightedAddr, err := createWeightedAddr(*ept.address, opaquePorts, ept.enableH2Upgrade, ept.identityTrustDomain, ept.controllerNS, ept.log)
	if err != nil {
		return nil, err
	}

	// `Get` doesn't include the namespace in the per-endpoint
	// metadata, so it needs to be special-cased.
	if ept.address.Pod != nil {
		weightedAddr.MetricLabels["namespace"] = ept.address.Pod.Namespace
	}

	return weightedAddr, err
}

func (ept *endpointProfileTranslator) createDefaultProfile(endpoint *pb.WeightedAddr, opaqueProtocol bool) *pb.DestinationProfile {
	return &pb.DestinationProfile{
		RetryBudget:    defaultRetryBudget(),
		Endpoint:       endpoint,
		OpaqueProtocol: opaqueProtocol,
	}
}

func (ept *endpointProfileTranslator) updateAddress(pod *v1.Pod) {
	ept.podReady = pod != nil
	address, err := watcher.CreateAddress(ept.k8sAPI, ept.metadataAPI, pod, ept.ip, ept.port)
	if err != nil {
		ept.log.Errorf("failed to create address: %s", err)
	} else {
		ept.Clean()
		ept.address = &address
		if err := ept.Sync(); err != nil {
			ept.log.Errorf("error syncing hostport adaptor: %s", err)
		}
	}
}

func (ept *endpointProfileTranslator) matchesIPPort(pod *v1.Pod) bool {
	if pod.Status.HostIP != ept.ip {
		return false
	}
	for _, container := range pod.Spec.Containers {
		for _, containerPort := range container.Ports {
			if uint32(containerPort.HostPort) == ept.port {
				return true
			}
		}
	}

	return false
}

func isRunningAndReady(pod *v1.Pod) bool {
	if pod == nil || pod.Status.Phase != v1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
			return true
		}
	}

	return false
}
