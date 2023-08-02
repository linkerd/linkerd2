package watcher

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
)

type (
	// EndpointsWatcherCache holds all EndpointsWatchers used by the destination
	// service to perform service discovery. Each cluster (including the one the
	// controller is running in) that may be looked-up for service discovery has
	// an associated EndpointsWatcher in the cache, along with a set of
	// immutable cluster configuration primitives (i.e. identity and cluster
	// domains).
	EndpointsWatcherCache struct {
		// Protects against illegal accesses
		sync.RWMutex

		k8sAPI               *k8s.API
		store                map[string]watchStore
		enableEndpointSlices bool
		log                  *logging.Entry
	}

	// watchStore is a helper struct that represents a cache item.
	watchStore struct {
		watcher       *EndpointsWatcher
		trustDomain   string
		clusterDomain string

		// Used to signal shutdown to the watcher.
		// Warning: it should be the same channel that was used to sync the
		// informers, otherwise the informers won't stop.
		stopCh chan<- struct{}
	}

	// configDecoder is a function type that given a secret, decodes it and
	// instantiates the API Server clients. Clients are dynamically created,
	// configDecoder allows some degree of isolation between the cache and
	// client bootstrapping.
	configDecoder = func(secret *v1.Secret, enableEndpointSlices bool) (*k8s.API, *k8s.MetadataAPI, error)
)

const (
	// LocalClusterKey represents the look-up key that returns an
	// EndpointsWatcher associated with the "local" cluster.
	LocalClusterKey         = "local"
	clusterNameLabel        = "multicluster.linkerd.io/cluster-name"
	trustDomainAnnotation   = "multicluster.linkerd.io/trust-domain"
	clusterDomainAnnotation = "multicluster.linkerd.io/cluster-domain"
)

// NewEndpointsWatcherCache creates a new (empty) EndpointsWatcherCache. It
// requires a Kubernetes API Server client instantiated for the local cluster.
func NewEndpointsWatcherCache(k8sAPI *k8s.API, enableEndpointSlices bool) *EndpointsWatcherCache {
	return &EndpointsWatcherCache{
		store: make(map[string]watchStore),
		log: logging.WithFields(logging.Fields{
			"component": "endpoints-watcher-cache",
		}),
		enableEndpointSlices: enableEndpointSlices,
		k8sAPI:               k8sAPI,
	}
}

// Start will register a pair of event handlers against a `Secret` informer.
//
// EndpointsWatcherCache will watch multicluster specific `Secret` objects (to
// create watchers that allow for remote service discovery).
//
// Valid secrets will create and start a new EndpointsWatcher for a remote
// cluster. When a secret is removed, the watcher is automatically stopped and
// cleaned-up.
func (ewc *EndpointsWatcherCache) Start() error {
	return ewc.startWithDecoder(decodeK8sConfigFromSecret)
}

func (ewc *EndpointsWatcherCache) startWithDecoder(decodeFn configDecoder) error {
	_, err := ewc.k8sAPI.Secret().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			secret, ok := obj.(*v1.Secret)
			if !ok {
				ewc.log.Errorf("Error processing 'Secret' object: got %#v, expected *corev1.Secret", secret)
				return
			}

			if secret.Type != consts.MirrorSecretType {
				ewc.log.Tracef("Skipping Add event for 'Secret' object %s/%s: invalid type %s", secret.Namespace, secret.Name, secret.Type)
				return

			}

			clusterName, found := secret.GetLabels()[clusterNameLabel]
			if !found {
				ewc.log.Tracef("Skipping Add event for 'Secret' object %s/%s: missing \"%s\" label", secret.Namespace, secret.Name, clusterNameLabel)
				return
			}

			if err := ewc.addWatcher(clusterName, secret, decodeFn); err != nil {
				ewc.log.Errorf("Error adding watcher for cluster %s: %v", clusterName, err)
			}

		},
		DeleteFunc: func(obj interface{}) {
			secret, ok := obj.(*v1.Secret)
			if !ok {
				// If the Secret was deleted when the watch was disconnected
				// (for whatever reason) and the event was missed, the object is
				// added with 'DeletedFinalStateUnknown'. Its state may be
				// stale, so it should be cleaned-up.
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					ewc.log.Debugf("unable to get object from DeletedFinalStateUnknown %#v", obj)
					return
				}
				// If the zombie object is a `Secret` we can delete it.
				secret, ok = tombstone.Obj.(*v1.Secret)
				if !ok {
					ewc.log.Debugf("DeletedFinalStateUnknown contained object that is not a Secret %#v", obj)
					return
				}
			}

			clusterName, found := secret.GetLabels()[clusterNameLabel]
			if !found {
				ewc.log.Tracef("Skipping Delete event for 'Secret' object %s/%s: missing \"%s\" label", secret.Namespace, secret.Name, clusterNameLabel)
				return
			}

			ewc.removeWatcher(clusterName)

		},
	})

	if err != nil {
		return err
	}

	return err
}

// Get safely retrieves a watcher from the cache given a cluster name. It
// returns the watcher, cluster configuration, if the entry exists in the cache.
func (ewc *EndpointsWatcherCache) Get(clusterName string) (*EndpointsWatcher, string, string, bool) {
	ewc.RLock()
	defer ewc.RUnlock()
	s, found := ewc.store[clusterName]
	return s.watcher, s.trustDomain, s.clusterDomain, found
}

// GetWatcher is a convenience method that given a cluster name only returns the
// associated EndpointsWatcher if it exists in the cache.
func (ewc *EndpointsWatcherCache) GetWatcher(clusterName string) (*EndpointsWatcher, bool) {
	ewc.RLock()
	defer ewc.RUnlock()
	s, found := ewc.store[clusterName]
	return s.watcher, found
}

// GetLocalWatcher is a convenience method that retrieves the watcher associated
// with the local cluster. Its existence is assumed.
func (ewc *EndpointsWatcherCache) GetLocalWatcher() *EndpointsWatcher {
	ewc.RLock()
	defer ewc.RUnlock()
	return ewc.store[LocalClusterKey].watcher
}

// GetClusterConfig is a convenience method that given a cluster name retrieves
// its associated configuration strings if the entry exists in the cache.
func (ewc *EndpointsWatcherCache) GetClusterConfig(clusterName string) (string, string, bool) {
	ewc.RLock()
	defer ewc.RUnlock()
	s, found := ewc.store[clusterName]
	return s.trustDomain, s.clusterDomain, found
}

// AddLocalWatcher adds a watcher to the cache using the local cluster key.
func (ewc *EndpointsWatcherCache) AddLocalWatcher(stopCh chan<- struct{}, watcher *EndpointsWatcher, trustDomain, clusterDomain string) {
	ewc.Lock()
	defer ewc.Unlock()
	ewc.store[LocalClusterKey] = watchStore{
		watcher,
		trustDomain,
		clusterDomain,
		stopCh,
	}
}

// removeWatcher is triggered by the cache's Secret informer when a secret is
// removed. Given a cluster name, it removes the entry from the cache after
// stopping the associated watcher.
func (ewc *EndpointsWatcherCache) removeWatcher(clusterName string) {
	ewc.Lock()
	defer ewc.Unlock()
	if s, found := ewc.store[clusterName]; found {
		// For good measure, close the channel after stopping to ensure
		// informers are shut down.
		defer close(s.stopCh)
		s.watcher.Stop(s.stopCh)
		delete(ewc.store, clusterName)
		ewc.log.Tracef("Removed cluster %s from EndpointsWatcherCache", clusterName)
	}
}

// addWatcher is triggered by the cache's Secret informer when a secret is
// added, or during the initial informer list. Given a cluster name and a Secret
// object, it creates an EndpointsWatcher for a remote cluster and syncs its
// informers before returning.
func (ewc *EndpointsWatcherCache) addWatcher(clusterName string, secret *v1.Secret, decodeFn configDecoder) error {
	clusterDomain, found := secret.GetAnnotations()[clusterDomainAnnotation]
	if !found {
		return fmt.Errorf("missing \"%s\" annotation", clusterDomainAnnotation)
	}

	trustDomain, found := secret.GetAnnotations()[trustDomainAnnotation]
	if !found {
		return fmt.Errorf("missing \"%s\" annotation", trustDomainAnnotation)
	}

	remoteAPI, metadataAPI, err := decodeFn(secret, ewc.enableEndpointSlices)
	if err != nil {
		return err
	}

	watcher, err := NewEndpointsWatcher(
		remoteAPI,
		metadataAPI,
		logging.WithFields(logging.Fields{
			"remote-cluster": clusterName,
		}),
		ewc.enableEndpointSlices,
	)
	if err != nil {
		return err
	}

	ewc.Lock()
	stopCh := make(chan struct{}, 1)
	ewc.store[clusterName] = watchStore{
		watcher,
		trustDomain,
		clusterDomain,
		stopCh,
	}
	ewc.Unlock()

	remoteAPI.Sync(stopCh)
	metadataAPI.Sync(stopCh)
	ewc.log.Tracef("Added cluster %s to EndpointsWatcherCache", clusterName)

	return nil
}

// decodeK8sConfigFromSecret implements the decoder function type and creates
// the necessary configuration from a secret.
func decodeK8sConfigFromSecret(secret *v1.Secret, enableEndpointSlices bool) (*k8s.API, *k8s.MetadataAPI, error) {
	data, found := secret.Data[consts.ConfigKeyName]
	if !found {
		return nil, nil, errors.New("missing kubeconfig file")
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return nil, nil, err
	}

	ctx := context.Background()
	var remoteAPI *k8s.API
	if enableEndpointSlices {
		remoteAPI, err = k8s.InitializeAPIForConfig(
			ctx,
			cfg,
			true,
			k8s.Endpoint, k8s.ES, k8s.Pod, k8s.Svc, k8s.SP, k8s.Job, k8s.Srv,
		)
	} else {
		remoteAPI, err = k8s.InitializeAPIForConfig(
			ctx,
			cfg,
			true,
			k8s.Endpoint, k8s.Pod, k8s.Svc, k8s.SP, k8s.Job, k8s.Srv,
		)
	}
	if err != nil {
		return nil, nil, err
	}

	metadataAPI, err := k8s.InitializeMetadataAPIForConfig(cfg, k8s.Node, k8s.RS)
	if err != nil {
		return nil, nil, err
	}

	return remoteAPI, metadataAPI, nil
}

func (ewc *EndpointsWatcherCache) len() int {
	ewc.RLock()
	defer ewc.RUnlock()
	return len(ewc.store)
}
