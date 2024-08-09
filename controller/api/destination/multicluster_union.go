package destination

import (
	"fmt"
	"sync"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

type serviceImportID struct {
	name      string
	namespace string
	port      uint32
}

// MulticlusterUnion indexes watchers and translators
//
// We will need to do 2 things:
// 1. Ensure that we can update our state based on the state of the svc import
// 2. Ensure that we can update our state based on the store state (need to be
// notified when a cluster is removed or added)
type MulticlusterUnion struct {
	log         *logging.Entry
	k8sAPI      *k8s.API
	metadataAPI *k8s.MetadataAPI

	clusterStore *watcher.ClusterStore
	serviceImportID
	config Config

	stream    RequestStream
	streamEnd chan struct{}

	registeredWatchers  map[string]*watcher.EndpointsWatcher
	registeredListeners map[string]*endpointTranslator
	sync.RWMutex
}

func NewMulticlusterUnion(
	log *logging.Entry,
	k8sAPI *k8s.API,
	metadataAPI *k8s.MetadataAPI,
	stream RequestStream,
	streamEnd chan struct{},
	clusterStore *watcher.ClusterStore,
	config Config,
	svcName,
	svcNamespace string,
	svcPort uint32) *MulticlusterUnion {
	return &MulticlusterUnion{
		log:          log,
		k8sAPI:       k8sAPI,
		metadataAPI:  metadataAPI,
		clusterStore: clusterStore,
		serviceImportID: serviceImportID{
			name:      svcName,
			namespace: svcNamespace,
			port:      svcPort,
		},
		config:              config,
		stream:              stream,
		streamEnd:           streamEnd,
		registeredWatchers:  map[string]*watcher.EndpointsWatcher{},
		registeredListeners: map[string]*endpointTranslator{},
	}
}

type RequestStream = pb.Destination_GetServer

func (m *MulticlusterUnion) InitializeAndRunUnion() error {

	go func() {
		// Get the name of the service import and create a listener
		smp, err := m.k8sAPI.Smp().Lister().ServiceImports(m.namespace).Get(m.name)
		if err != nil {
			if !kerrors.IsNotFound(err) {
				m.log.Errorf("error retrieving list of service imports, %s", err)
			}
		}

		var clusters []string
		if smp != nil {
			clusters = smp.Status.Clusters
		}

		for _, cluster := range clusters {
			m.addCluster(cluster)
		}

		// Start watching for state changes
		add, rm := m.clusterStore.Subscribe()
		loop := true
		for {
			if !loop {
				break
			}

			select {
			case <-m.streamEnd:
				loop = false
				m.log.Infof("shutting down")

			case clusterName := <-add:
				smp, err := m.k8sAPI.Smp().Lister().ServiceImports(m.namespace).Get(m.name)
				if err != nil {
					m.log.Errorf("error creating new cluster-agnostic translator: %s", err)
				}

				for _, cluster := range smp.Status.Clusters {
					if cluster == clusterName {
						if err := m.addCluster(clusterName); err != nil {
							m.log.Errorf("error creating new cluster-agnostic translator: %s", err)
						}
					}
				}

			case clusterName := <-rm:
				m.removeCluster(clusterName)
			}

		}
	}()

	return nil
}

func (m *MulticlusterUnion) Stop() {
	m.RLock()
	defer m.RUnlock()
	for cluster, translator := range m.registeredListeners {
		if watch, ok := m.registeredWatchers[cluster]; ok {
			translator.Stop()
			watch.Unsubscribe(watcher.ServiceID{Namespace: m.namespace, Name: m.name}, m.port, "", translator)
		}
	}
}

func (m *MulticlusterUnion) addCluster(clusterName string) error {
	m.log.Infof("adding new cluster %s and building translator", clusterName)
	m.Lock()
	defer m.Unlock()
	watch, cfg, ok := m.clusterStore.Get(clusterName)
	if !ok {
		return fmt.Errorf("Cluster %s not found in store", clusterName)
	}

	m.registeredWatchers[clusterName] = watch
	// Create listener
	translator := newEndpointTranslator(
		m.config.ControllerNS,
		cfg.TrustDomain,
		m.config.EnableH2Upgrade,
		false, // Disable endpoint filtering for remote discovery.
		m.config.EnableIPv6,
		m.config.ExtEndpointZoneWeights,
		m.config.MeshedHttp2ClientParams,
		fmt.Sprintf("%s.%s.svc.%s:%d", m.name, m.namespace, m.config.ClusterDomain, m.port),
		//token.NodeName,
		"",
		m.config.DefaultOpaquePorts,
		m.metadataAPI,
		m.stream,
		m.streamEnd,
		m.log,
	)
	m.registeredListeners[clusterName] = translator
	translator.Start()
	return watch.Subscribe(watcher.ServiceID{Namespace: m.namespace, Name: m.name}, m.port, "", translator)
}

func (m *MulticlusterUnion) removeCluster(clusterName string) {
	m.log.Infof("removing cluster %s", clusterName)
	m.Lock()
	defer m.Unlock()

	translator, ok := m.registeredListeners[clusterName]
	if !ok {
		return
	}

	translator.Stop()
	if watch, ok := m.registeredWatchers[clusterName]; ok {
		watch.Unsubscribe(watcher.ServiceID{Namespace: m.namespace, Name: m.name}, m.port, "", translator)
	}

	delete(m.registeredWatchers, clusterName)
	delete(m.registeredListeners, clusterName)
}
