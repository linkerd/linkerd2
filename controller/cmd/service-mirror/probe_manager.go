package servicemirror

import (
	"fmt"
	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"
)

// NewProbeManager creates a new probe manager
func NewProbeManager(events chan interface{}, k8sAPI *k8s.API) *ProbeManager {
	metricVecs := newProbeMetricVecs()
	return &ProbeManager{
		k8sAPI:       k8sAPI,
		probeWorkers: make(map[string]*ProbeWorker),
		events:       events,
		metricVecs:   &metricVecs,
		done:         make(chan struct{}, 1),
	}
}

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

// ClusterRegistered is emitted when a new cluster becomes registred for mirroring
type ClusterRegistered struct {
	clusterName   string
	port          int32
	path          string
	periodSeconds int32
}

// GatewayUpdated is emitted when something about the gateway is updated (i.e. its external IP(s))
type GatewayUpdated struct {
	*GatewayProbeSpec
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
	}
}

func (m *ProbeManager) handleMirroredServicePaired(event *MirroredServicePaired) {
	probeKey := probeKey(event.gatewayNs, event.gatewayName, event.clusterName)
	worker, ok := m.probeWorkers[probeKey]
	if ok {
		log.Debugf("Probe worker %s already exists", probeKey)
		worker.IncNumServices()
	} else {
		log.Debugf("Creating probe worker %s", probeKey)
		probeMetrics := m.metricVecs.newMetrics(event.gatewayNs, event.gatewayName, event.clusterName)
		worker = NewProbeWorker(event.GatewayProbeSpec, &probeMetrics, probeKey)
		worker.IncNumServices()
		m.probeWorkers[probeKey] = worker
		worker.Start()
	}
}

func (m *ProbeManager) handleMirroredServiceUnpaired(event *MirroredServiceUnpaired) {
	probeKey := probeKey(event.gatewayNs, event.gatewayName, event.clusterName)
	worker, ok := m.probeWorkers[probeKey]
	if ok {
		worker.DcrNumServices()
		if worker.numAssociatedServices < 1 {
			log.Debugf("Probe worker's %s associated services dropped to 0, cleaning up", probeKey)
			worker.Stop()
			delete(m.probeWorkers, probeKey)
		}
	}
}

func (m *ProbeManager) handleGatewayUpdated(event *GatewayUpdated) {
	probeKey := probeKey(event.gatewayNs, event.gatewayName, event.clusterName)
	worker, ok := m.probeWorkers[probeKey]
	if ok {
		worker.UpdateProbeSpec(event.GatewayProbeSpec)
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

func (m *ProbeManager) run() {
	for {
		select {
		case event := <-m.events:
			log.Debugf("Received event: %v", event)
			m.handleEvent(event)
		case <-m.done:
			log.Debug("Shutting down ProbeManager")
			for key, worker := range m.probeWorkers {
				worker.Stop()
				delete(m.probeWorkers, key)
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
