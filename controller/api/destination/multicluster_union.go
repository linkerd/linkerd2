package destination

import (
	"fmt"
	"sync"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
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
	// Get the name of the service import and create a listener
	smp, err := m.k8sAPI.Smp().Lister().ServiceImports(m.namespace).Get(m.name)
	if err != nil {
		return err
	}

	for _, cluster := range smp.Status.Clusters {
		watcher, cfg, ok := m.clusterStore.Get(cluster)
		if !ok {
			continue
		}

		m.log.Infof("building translator for %s", cluster)
		m.Lock()
		m.registeredWatchers[cluster] = watcher
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
		m.registeredListeners[cluster] = translator
		m.Unlock()
	}

	go func() {
		m.RLock()
		for cluster, translator := range m.registeredListeners {
			if watch, ok := m.registeredWatchers[cluster]; ok {
				translator.Start()
				// TODO: stopping will have to handle in a separate Stop function on
				// the union, simplified for now
				defer translator.Stop()
				err := watch.Subscribe(watcher.ServiceID{Namespace: m.namespace, Name: m.name}, m.port, "", translator)
				if err != nil {
					// TODO: handle this in a better way
					m.log.Errorf("error subscribing to watcher: %v", err)
				}
				defer watch.Unsubscribe(watcher.ServiceID{Namespace: m.namespace, Name: m.name}, m.port, "", translator)
			}
		}
		m.RUnlock()
		loop := true
		for {
			if !loop {
				break
			}

			select {
			case <-m.streamEnd:
				loop = false
				m.log.Infof("shutting down")

			}

		}
	}()

	return nil
}
