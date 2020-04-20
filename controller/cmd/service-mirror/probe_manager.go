package servicemirror

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const probeChanBufferSize = 500

// ProbeManager takes care of managing the lifecycle of probe workers
type ProbeManager struct {
	k8sAPI       *k8s.API
	probeWorkers map[string]*ProbeWorker

	events     chan interface{}
	metricVecs *probeMetricVecs
	done       chan struct{}
}

// MirroredServiceUnpaired is emitted when a service is no longer mirrored
type MirroredServiceUnpaired struct {
	serviceName      string
	serviceNamespace string
	gatewayName      string
	gatewayNs        string
	clusterName      string
}

// MirroredServicePaired is emitted when a new mirrored service is created
type MirroredServicePaired struct {
	serviceName      string
	serviceNamespace string
	GatewaySpec
}

// ClusterNotRegistered is is emitted when the cluster is not monitored anymore
type ClusterNotRegistered struct {
	clusterName string
}

// GatewayUpdated is emitted when something about the gateway is updated (i.e. its external IP(s))
type GatewayUpdated struct {
	GatewaySpec
}

// NewProbeManager creates a new probe manager
func NewProbeManager(k8sAPI *k8s.API) *ProbeManager {
	metricVecs := newProbeMetricVecs()
	return &ProbeManager{
		k8sAPI:       k8sAPI,
		probeWorkers: make(map[string]*ProbeWorker),
		events:       make(chan interface{}, probeChanBufferSize),
		metricVecs:   &metricVecs,
		done:         make(chan struct{}),
	}
}

func eventTypeString(ev interface{}) string {
	switch ev.(type) {
	case *MirroredServicePaired:
		return "MirroredServicePaired"
	case *MirroredServiceUnpaired:
		return "MirroredServiceUnpaired"
	case *GatewayUpdated:
		return "GatewayUpdated"
	case *ClusterNotRegistered:
		return "ClusterNotRegistered"
	default:
		return "Unknown"
	}
}

func (m *ProbeManager) enqueueEvent(event interface{}) {
	m.metricVecs.enqueues.With(prometheus.Labels{eventTypeLabelName: eventTypeString(event)}).Inc()
	m.events <- event
}

func probeKey(gatewayNamespace string, gatewayName string, clusterName string) string {
	return fmt.Sprintf("%s-%s-%s", gatewayNamespace, gatewayName, clusterName)
}

func (m *ProbeManager) handleEvent(ev interface{}) {
	switch ev := ev.(type) {
	case *MirroredServicePaired:
		m.handleMirroredServicePaired(ev)
	case *MirroredServiceUnpaired:
		m.handleMirroredServiceUnpaired(ev)
	case *GatewayUpdated:
		m.handleGatewayUpdated(ev)
	case *ClusterNotRegistered:
		m.handleClusterNotRegistered(ev)
	default:
		log.Errorf("Received unknown event: %v", ev)
	}
}

func endpointAddressesToIps(addrs []corev1.EndpointAddress) []string {
	result := []string{}

	for _, a := range addrs {

		result = append(result, a.IP)
	}

	return result
}

func gatewayToProbeSpec(gatewaySpec GatewaySpec) *probeSpec {

	if gatewaySpec.ProbeConfig == nil {
		return nil
	}
	return &probeSpec{
		ips:             endpointAddressesToIps(gatewaySpec.addresses),
		path:            gatewaySpec.path,
		port:            gatewaySpec.port,
		periodInSeconds: gatewaySpec.periodInSeconds,
	}
}

func (m *ProbeManager) handleMirroredServicePaired(event *MirroredServicePaired) {
	probeKey := probeKey(event.gatewayNamespace, event.gatewayName, event.clusterName)
	worker, ok := m.probeWorkers[probeKey]
	if ok {
		log.Debugf("Probe worker %s already exists", probeKey)
		worker.PairService(event.serviceName, event.serviceNamespace)
	} else {
		log.Debugf("Creating probe worker %s", probeKey)
		probeMetrics, err := m.metricVecs.newWorkerMetrics(event.gatewayNamespace, event.gatewayName, event.clusterName)
		if err != nil {
			log.Errorf("Could not crete probe metrics: %s", err)
		} else {
			probeSpec := gatewayToProbeSpec(event.GatewaySpec)
			if probeSpec != nil {
				worker = NewProbeWorker(probeSpec, probeMetrics, probeKey)
				worker.PairService(event.serviceName, event.serviceNamespace)
				m.probeWorkers[probeKey] = worker
				worker.Start()
			} else {
				log.Debugf("No probe spec for: %s", probeKey)
			}
		}
	}
}

func (m *ProbeManager) handleMirroredServiceUnpaired(event *MirroredServiceUnpaired) {
	probeKey := probeKey(event.gatewayNs, event.gatewayName, event.clusterName)
	worker, ok := m.probeWorkers[probeKey]
	if ok {
		worker.UnPairService(event.serviceName, event.serviceNamespace)
		if worker.NumPairedServices() < 1 {
			log.Debugf("Probe worker's %s associated services dropped to 0, cleaning up", probeKey)
			worker.Stop()
			delete(m.probeWorkers, probeKey)
		}
	} else {
		log.Debugf("Could not find a worker for %s while handling MirroredServiceUnpaired event", probeKey)
	}
}

func (m *ProbeManager) handleGatewayUpdated(event *GatewayUpdated) {
	probeKey := probeKey(event.gatewayNamespace, event.gatewayName, event.clusterName)
	worker, ok := m.probeWorkers[probeKey]
	if ok {
		probeSpec := gatewayToProbeSpec(event.GatewaySpec)
		if probeSpec != nil {
			worker.UpdateProbeSpec(probeSpec)
		} else {
			log.Debugf("No probe spec for: %s", probeKey)
		}
	} else {
		log.Debugf("Could not find a worker for %s while handling GatewayUpdated event", probeKey)
	}
}

func (m *ProbeManager) stopProbe(key string) {
	if worker, ok := m.probeWorkers[key]; ok {
		worker.Stop()
		delete(m.probeWorkers, key)
	} else {
		log.Infof("Could not find probe worker with key %s", key)
	}
}

func (m *ProbeManager) handleClusterNotRegistered(event *ClusterNotRegistered) {
	matchLabels := map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteClusterNameLabel: event.clusterName,
	}

	services, err := m.k8sAPI.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		log.Errorf("Was not able to unregister cluster %s, %s", event.clusterName, err)
	}

	stopped := make(map[string]bool)
	for _, svc := range services {
		probeKey := probeKey(svc.Labels[consts.RemoteGatewayNameLabel], svc.Labels[consts.RemoteGatewayNameLabel], event.clusterName)

		if _, ok := stopped[probeKey]; !ok {
			m.stopProbe(probeKey)
			stopped[probeKey] = true
		}
	}
}

func (m *ProbeManager) run() {
	for {
		select {
		case event := <-m.events:
			log.Debugf("Received event: %v", event)
			m.metricVecs.dequeues.With(prometheus.Labels{eventTypeLabelName: eventTypeString(event)}).Inc()
			m.handleEvent(event)
		case <-m.done:
			log.Debug("Shutting down ProbeManager")
			for key := range m.probeWorkers {
				m.stopProbe(key)
			}
			return
		}
	}
}

// Start starts the probe manager
func (m *ProbeManager) Start() {
	go m.run()
}

// Stop stops the probe manager
func (m *ProbeManager) Stop() {
	close(m.done)
}
