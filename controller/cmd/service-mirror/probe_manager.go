package servicemirror

import (
	"fmt"

	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"
)

// ProbeManager takes care of managing the lifecycle of probe workers
type ProbeManager struct {
	k8sAPI       *k8s.API
	probeWorkers map[string]*ProbeWorker

	events     chan interface{}
	metricVecs *probeMetricVecs
	done       chan struct{}
}

// GatewayProbeSpec is the specification of a probe that will be executed by the worker
type GatewayProbeSpec struct {
	clusterName   string
	gatewayName   string
	gatewayNs     string
	gatewayIps    []string
	port          int32
	path          string
	periodSeconds int32
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
	*GatewayProbeSpec
}

// ClusterRegistered is emitted when a new cluster becomes registered for mirroring
type ClusterRegistered struct {
	clusterName   string
	port          int32
	path          string
	periodSeconds int32
}

// ClusterNotRegistered is is emitted when the cluster is not monitored anymore
type ClusterNotRegistered struct {
	clusterName string
}

// GatewayUpdated is emitted when something about the gateway is updated (i.e. its external IP(s))
type GatewayUpdated struct {
	*GatewayProbeSpec
}

// NewProbeManager creates a new probe manager
func NewProbeManager(events chan interface{}, k8sAPI *k8s.API) *ProbeManager {
	metricVecs := newProbeMetricVecs()
	return &ProbeManager{
		k8sAPI:       k8sAPI,
		probeWorkers: make(map[string]*ProbeWorker),
		events:       events,
		metricVecs:   &metricVecs,
		done:         make(chan struct{}),
	}
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
	case *ClusterRegistered:
		m.handleClusterRegistered(ev)
	case *ClusterNotRegistered:
		m.handleClusterNotRegistered(ev)
	default:
		log.Errorf("Received unknown event: %v", ev)
	}
}

func (m *ProbeManager) handleMirroredServicePaired(event *MirroredServicePaired) {
	probeKey := probeKey(event.gatewayNs, event.gatewayName, event.clusterName)
	worker, ok := m.probeWorkers[probeKey]
	if ok {
		log.Debugf("Probe worker %s already exists", probeKey)
		worker.PairService(event.serviceName, event.serviceNamespace)
	} else {
		log.Debugf("Creating probe worker %s", probeKey)
		probeMetrics := m.metricVecs.newMetrics(event.gatewayNs, event.gatewayName, event.clusterName)
		worker = NewProbeWorker(event.GatewayProbeSpec, &probeMetrics, probeKey)
		worker.PairService(event.serviceName, event.serviceNamespace)
		m.probeWorkers[probeKey] = worker
		worker.Start()
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
	probeKey := probeKey(event.gatewayNs, event.gatewayName, event.clusterName)
	worker, ok := m.probeWorkers[probeKey]
	if ok {
		worker.UpdateProbeSpec(event.GatewayProbeSpec)
	} else {
		log.Debugf("Could not find a worker for %s while handling MirroredServiceUnpaired event", probeKey)
	}
}

func (m *ProbeManager) handleClusterRegistered(event *ClusterRegistered) {
	matchLabels := map[string]string{
		consts.MirroredResourceLabel:  "true",
		consts.RemoteClusterNameLabel: event.clusterName,
	}

	services, err := m.k8sAPI.Svc().Lister().List(labels.Set(matchLabels).AsSelector())
	if err != nil {
		log.Errorf("Was not able to sync cluster %s, %s", event.clusterName, err)
	}

	for _, svc := range services {
		ips := []string{}
		if endp, err := m.k8sAPI.Endpoint().Lister().Endpoints(svc.Namespace).Get(svc.Name); err == nil {
			if len(endp.Subsets) == 1 {
				for _, addr := range endp.Subsets[0].Addresses {
					ips = append(ips, addr.IP)
				}
			}
		}

		log.Debugf("Syncing service %s", svc.Name)

		paired := MirroredServicePaired{
			serviceName:      svc.Name,
			serviceNamespace: svc.Namespace,
			GatewayProbeSpec: &GatewayProbeSpec{
				clusterName:   event.clusterName,
				gatewayName:   svc.Labels[consts.RemoteGatewayNameLabel],
				gatewayNs:     svc.Labels[consts.RemoteGatewayNsLabel],
				gatewayIps:    ips,
				port:          event.port,
				path:          event.path,
				periodSeconds: event.periodSeconds,
			},
		}
		m.events <- &paired
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
