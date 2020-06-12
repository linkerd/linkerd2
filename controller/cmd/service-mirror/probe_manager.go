package servicemirror

import (
	"fmt"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/tools/cache"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

const probeChanBufferSize = 500

// ProbeManager takes care of managing the lifecycle of probe workers
type ProbeManager struct {
	probeWorkers            map[string]*ProbeWorker
	mirroredGatewayInformer cache.SharedIndexInformer
	events                  chan interface{}
	metricVecs              *probeMetricVecs
	done                    chan struct{}
}

// GatewayMirrorCreated is observed when a mirror of a remote gateway is created locally
type GatewayMirrorCreated struct {
	gatewayName      string
	gatewayNamespace string
	clusterName      string
	probeSpec
}

// GatewayMirrorDeleted is emitted when a mirror of a remote gateway is deleted
type GatewayMirrorDeleted struct {
	gatewayName      string
	gatewayNamespace string
	clusterName      string
}

// GatewayMirrorUpdated is emitted when the mirror of a remote gateway has changed
type GatewayMirrorUpdated struct {
	gatewayName      string
	gatewayNamespace string
	clusterName      string
	probeSpec
}

// NewProbeManager creates a new probe manager
func NewProbeManager(mirroredGatewayInformer cache.SharedIndexInformer) *ProbeManager {
	metricVecs := newProbeMetricVecs()
	return &ProbeManager{
		mirroredGatewayInformer: mirroredGatewayInformer,
		probeWorkers:            make(map[string]*ProbeWorker),
		events:                  make(chan interface{}, probeChanBufferSize),
		metricVecs:              &metricVecs,
		done:                    make(chan struct{}),
	}
}

func eventTypeString(ev interface{}) string {
	switch ev.(type) {
	case *GatewayMirrorCreated:
		return "GatewayMirrorCreated"
	case *GatewayMirrorDeleted:
		return "GatewayMirrorDeleted"
	case *GatewayMirrorUpdated:
		return "GatewayMirrorUpdated"
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
	case *GatewayMirrorCreated:
		m.handleGatewayMirrorCreated(ev)
	case *GatewayMirrorUpdated:
		m.handleGatewayMirrorUpdated(ev)
	case *GatewayMirrorDeleted:
		m.handleGatewayMirrorDeleted(ev)
	default:
		log.Errorf("Received unknown event: %v", ev)
	}
}

func (m *ProbeManager) handleGatewayMirrorDeleted(event *GatewayMirrorDeleted) {
	probeKey := probeKey(event.gatewayNamespace, event.gatewayName, event.clusterName)
	m.stopProbe(probeKey)
}

func (m *ProbeManager) handleGatewayMirrorCreated(event *GatewayMirrorCreated) {
	probeKey := probeKey(event.gatewayNamespace, event.gatewayName, event.clusterName)
	worker, ok := m.probeWorkers[probeKey]
	if ok {
		log.Infof("There is already a probe worker for %s. Updating instead of creating", probeKey)
		worker.UpdateProbeSpec(&event.probeSpec)
	} else {
		log.Infof("Creating probe worker %s", probeKey)
		probeMetrics, err := m.metricVecs.newWorkerMetrics(event.gatewayNamespace, event.gatewayName, event.clusterName)
		if err != nil {
			log.Errorf("Could not crete probe metrics: %s", err)
		} else {
			localGatewayName := fmt.Sprintf("%s-%s", event.gatewayName, event.clusterName)
			worker = NewProbeWorker(localGatewayName, &event.probeSpec, probeMetrics, probeKey)
			m.probeWorkers[probeKey] = worker
			worker.Start()
		}
	}
}

func (m *ProbeManager) handleGatewayMirrorUpdated(event *GatewayMirrorUpdated) {
	probeKey := probeKey(event.gatewayNamespace, event.gatewayName, event.clusterName)
	worker, ok := m.probeWorkers[probeKey]
	if ok {
		if worker.probeSpec.port != event.port || worker.probeSpec.periodInSeconds != event.periodInSeconds || worker.probeSpec.path != event.path {
			worker.UpdateProbeSpec(&event.probeSpec)
		}
	} else {
		log.Infof("Could not find a worker for %s while handling GatewayMirrorUpdated event", probeKey)
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

func (m *ProbeManager) run() {
	for {
		select {
		case event := <-m.events:
			log.Infof("Probe Manager: received event: %s", event)
			m.metricVecs.dequeues.With(prometheus.Labels{eventTypeLabelName: eventTypeString(event)}).Inc()
			m.handleEvent(event)
		case <-m.done:
			log.Infof("Shutting down ProbeManager")
			for key := range m.probeWorkers {
				m.stopProbe(key)
			}
			return
		}
	}
}

func extractProbeSpec(svc *corev1.Service) (*probeSpec, error) {
	path, hasPath := svc.Annotations[consts.MirroredGatewayProbePath]
	if !hasPath {
		return nil, fmt.Errorf("mirrored Gateway service is missing %s annotation", consts.MirroredGatewayProbePath)
	}

	probePort, err := extractPort(svc.Spec.Ports, consts.ProbePortName)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", svc.Name, err)
	}

	period, hasPeriod := svc.Annotations[consts.MirroredGatewayProbePeriod]
	if !hasPeriod {
		return nil, fmt.Errorf("mirrored Gateway service is missing %s annotation", consts.MirroredGatewayProbePeriod)
	}

	probePeriod, err := strconv.ParseUint(period, 10, 32)
	if err != nil {
		return nil, err
	}

	return &probeSpec{
		path:            path,
		port:            probePort,
		periodInSeconds: uint32(probePeriod),
	}, nil

}

// Start starts the probe manager
func (m *ProbeManager) Start() {
	m.mirroredGatewayInformer.AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch object := obj.(type) {
				case *corev1.Service:
					_, isMirrorGateway := object.Labels[consts.MirroredGatewayLabel]
					return isMirrorGateway

				case cache.DeletedFinalStateUnknown:
					if svc, ok := object.Obj.(*corev1.Service); ok {
						_, isMirrorGateway := svc.Labels[consts.MirroredGatewayLabel]
						return isMirrorGateway
					}
					return false
				default:
					return false
				}
			},

			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					service := obj.(*corev1.Service)
					spec, err := extractProbeSpec(service)
					if err != nil {
						log.Errorf("Could not parse probe spec %s", err)
					} else {
						m.enqueueEvent(&GatewayMirrorCreated{
							gatewayName:      service.Annotations[consts.MirroredGatewayRemoteName],
							gatewayNamespace: service.Annotations[consts.MirroredGatewayRemoteNameSpace],
							clusterName:      service.Labels[consts.RemoteClusterNameLabel],
							probeSpec:        *spec,
						})
					}
				},
				DeleteFunc: func(obj interface{}) {
					service, ok := obj.(*corev1.Service)
					if !ok {
						tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
						if !ok {
							log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
							return
						}
						service, ok = tombstone.Obj.(*corev1.Service)
						if !ok {
							log.Errorf("DeletedFinalStateUnknown contained object that is not a Secret %#v", obj)
							return
						}
					}

					m.enqueueEvent(&GatewayMirrorDeleted{
						gatewayName:      service.Annotations[consts.MirroredGatewayRemoteName],
						gatewayNamespace: service.Annotations[consts.MirroredGatewayRemoteNameSpace],
						clusterName:      service.Labels[consts.RemoteClusterNameLabel],
					})
				},
				UpdateFunc: func(old, new interface{}) {
					oldService := old.(*corev1.Service)
					newService := new.(*corev1.Service)

					if oldService.ResourceVersion != newService.ResourceVersion {
						spec, err := extractProbeSpec(newService)
						if err != nil {
							log.Errorf("Could not parse probe spec %s", err)
						} else {
							m.enqueueEvent(&GatewayMirrorUpdated{
								gatewayName:      newService.Annotations[consts.MirroredGatewayRemoteName],
								gatewayNamespace: newService.Annotations[consts.MirroredGatewayRemoteNameSpace],
								clusterName:      newService.Labels[consts.RemoteClusterNameLabel],
								probeSpec:        *spec,
							})
						}
					}
				},
			},
		},
	)
	go m.run()
}

// Stop stops the probe manager
func (m *ProbeManager) Stop() {
	close(m.done)
}
