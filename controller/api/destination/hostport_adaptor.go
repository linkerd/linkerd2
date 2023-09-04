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
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

// hostPortAdaptor receives events from a podWatcher and forwards the pod and
// protocol updates to an endpointProfileTranslator. If required, it subscribes
// to associated Server updates. Pod updates are only taken into account to the
// extent they imply a change in its readiness
type hostPortAdaptor struct {
	servers    *watcher.ServerWatcher
	listener   *endpointProfileTranslator
	ip         string
	port       uint32
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
	log         *logging.Entry

	mu sync.Mutex
}

// hostIPMetrics is a prometheus gauge shared amongst hostPortAdaptor instances
var hostIPMetrics = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "host_port_subscribers",
		Help: "Counter of subscribes to Pod changes for a given hostIP and port",
	},
	[]string{"hostIP", "port"},
)

func newHostPortAdaptor(
	k8sAPI *k8s.API,
	metadataAPI *k8s.MetadataAPI,
	servers *watcher.ServerWatcher,
	enableH2Upgrade bool,
	controllerNS string,
	identityTrustDomain string,
	defaultOpaquePorts map[uint32]struct{},
	ip string,
	port uint32,
	listener *endpointProfileTranslator,
	address *watcher.Address,
	log *logging.Entry,
) *hostPortAdaptor {
	log = log.WithField("component", "hostport-adaptor").WithField("ip", ip)

	podReady := isRunningAndReady(address.Pod)

	// if the label map has already been created, it'll get reused
	metrics := hostIPMetrics.With(prometheus.Labels{
		"hostIP": ip,
		"port":   strconv.FormatUint(uint64(port), 10),
	})

	return &hostPortAdaptor{
		servers:             servers,
		listener:            listener,
		ip:                  ip,
		port:                port,
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

func (pt *hostPortAdaptor) Sync() error {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	opaquePorts := getAnnotatedOpaquePorts(pt.address.Pod, pt.defaultOpaquePorts)
	endpoint, err := pt.createEndpoint(*pt.address, opaquePorts)
	if err != nil {
		return fmt.Errorf("failed to create endpoint: %w", err)
	}
	pt.endpoint = endpoint
	pt.log.Debugf("Sync for endpoint %s", pt.endpoint)
	pt.subscribed = false

	// If the endpoint's port is annotated as opaque, we don't need to
	// subscribe for updates because it will always be opaque
	// regardless of any Servers that may select it.
	if _, ok := opaquePorts[pt.port]; ok {
		pt.UpdateProtocol(true)
	} else if pt.address.Pod == nil {
		pt.UpdateProtocol(false)
	} else {
		pt.UpdateProtocol(pt.address.OpaqueProtocol)
		pt.servers.Subscribe(pt.address.Pod, pt.port, pt)
		pt.subscribed = true
	}

	return nil
}

func (pt *hostPortAdaptor) Clean() {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if pt.subscribed {
		pt.log.Debugf("Clean for endpoint %s", pt.endpoint)
		pt.servers.Unsubscribe(pt.address.Pod, pt.port, pt)
		pt.subscribed = false
	}
}

func (pt *hostPortAdaptor) UpdateProtocol(opaqueProtocol bool) {
	pt.listener.UpdateProtocol(pt.address.Pod, pt.endpoint, opaqueProtocol)
}

// Update is an informer event handler - All operations should be non-blocking
func (pt *hostPortAdaptor) Update(pod *corev1.Pod) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if !pt.matchesIPPort(pod) {
		return
	}

	if pt.podReady && pt.address.Pod.UID != pod.UID {
		pt.log.Tracef("Current pod still ready, ignoring event on %s.%s", pod.Name, pod.Namespace)
		return
	}

	if pt.podReady && !isRunningAndReady(pod) {
		pt.log.Debugf("Pod %s.%s became unready - remove", pod.Name, pod.Namespace)
		pt.updateAddress(nil)
		return
	}

	if !pt.podReady && !isRunningAndReady(pod) {
		pt.log.Tracef("Ignore event on %s.%s until it becomes ready", pod.Name, pod.Namespace)
		return
	}

	if !pt.podReady && isRunningAndReady(pod) {
		pt.log.Debugf("Pod %s.%s became ready", pod.Name, pod.Namespace)
		pt.updateAddress(pod)
		return
	}

	pt.log.Tracef("Ignored event on pod %s.%s", pod.Name, pod.Namespace)
}

// Remove is an informer event handler - All operations should be non-blocking
func (pt *hostPortAdaptor) Remove(pod *corev1.Pod) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if !pt.matchesIPPort(pod) {
		return
	}

	if pt.address.Pod == nil {
		pt.log.Tracef("Pod %s.%s already removed; discard event", pod.Name, pod.Namespace)
		return
	}

	pt.log.Debugf("Remove pod %s.%s", pod.Name, pod.Namespace)
	pt.updateAddress(nil)
}

func (pt *hostPortAdaptor) MetricsInc() {
	pt.metrics.Inc()
}

func (pt *hostPortAdaptor) MetricsDec() {
	pt.metrics.Dec()
}

func (pt *hostPortAdaptor) updateAddress(pod *corev1.Pod) {
	go func() {
		pt.podReady = pod != nil
		address, err := watcher.CreateAddress(pt.k8sAPI, pt.metadataAPI, pod, pt.ip, pt.port)
		if err != nil {
			pt.log.Errorf("failed to create address: %s", err)
		} else {
			pt.Clean()
			pt.address = &address
			if err := pt.Sync(); err != nil {
				pt.log.Errorf("error syncing hostport adaptor: %s", err)
			}
		}
	}()
}

func (pt *hostPortAdaptor) createEndpoint(address watcher.Address, opaquePorts map[uint32]struct{},
) (*pb.WeightedAddr, error) {
	weightedAddr, err := createWeightedAddr(address, opaquePorts, pt.enableH2Upgrade, pt.identityTrustDomain, pt.controllerNS, pt.log)
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

func (pt *hostPortAdaptor) matchesIPPort(pod *corev1.Pod) bool {
	if pod.Status.HostIP != pt.ip {
		return false
	}
	for _, container := range pod.Spec.Containers {
		for _, containerPort := range container.Ports {
			if uint32(containerPort.HostPort) == pt.port {
				return true
			}
		}
	}

	return false
}

func isRunningAndReady(pod *corev1.Pod) bool {
	if pod == nil || pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}
