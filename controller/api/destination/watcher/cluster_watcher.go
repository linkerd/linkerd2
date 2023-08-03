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
	// ClusterStore holds all EndpointsWatchers used by the destination
	// service to perform service discovery. Each cluster (including the one the
	// controller is running in) that may be looked-up for service discovery has
	// an associated EndpointsWatcher in the cache, along with a set of
	// immutable cluster configuration primitives (i.e. identity and cluster
	// domains).
	ClusterStore struct {
		// Protects against illegal accesses
		sync.RWMutex

		k8sAPI               *k8s.API
		store                map[string]cluster
		enableEndpointSlices bool
		log                  *logging.Entry

		// Function used to parse a kubeconfig from a byte buffer. Based on the
		// kubeconfig, it creates API Server clients
		decodeFn configDecoder
	}

	// cluster is a helper struct that represents a cache item
	cluster struct {
		watcher       *EndpointsWatcher
		trustDomain   string
		clusterDomain string

		// Used to signal shutdown to the associated watcher's informers
		stopCh chan<- struct{}
	}

	// configDecoder is the type of a function that given a byte buffer, returns
	// a pair of API Server clients. The cache uses this function to dynamically
	// create clients after discovering a Secret.
	configDecoder = func(data []byte, enableEndpointSlices bool) (*k8s.API, *k8s.MetadataAPI, error)
)

const (
	// LocalClusterKey represents the look-up key that returns an
	// EndpointsWatcher associated with the "local" cluster.
	LocalClusterKey         = "local"
	clusterNameLabel        = "multicluster.linkerd.io/cluster-name"
	trustDomainAnnotation   = "multicluster.linkerd.io/trust-domain"
	clusterDomainAnnotation = "multicluster.linkerd.io/cluster-domain"
)

// NewClusterStore creates a new (empty) ClusterStore. It
// requires a Kubernetes API Server client instantiated for the local cluster.
//
// Upon creation, a pair of event handlers will be registered against the API
// Server client's Secret informer. The event handlers will add, or remove
// watcher entries from the cache by watching Secrets in the namespace the
// controller is running in.
//
// A new watcher is created for a remote cluster when its Secret is valid (contains
// necessary configuration, including a kubeconfig file). When a Secret is
// deleted from the cluster, if there is a corresponding watcher in the cache,
// it will be gracefully shutdown to allow for the memory to be freed.
func NewClusterStore(k8sAPI *k8s.API, enableEndpointSlices bool) (*ClusterStore, error) {
	return newClusterStoreWithDecoder(k8sAPI, enableEndpointSlices, decodeK8sConfigFromSecret)
}

// newWatcherCacheWithDecoder is a helper function that allows the creation of a
// cache with an arbitrary `configDecoder` function.
func newClusterStoreWithDecoder(k8sAPI *k8s.API, enableEndpointSlices bool, decodeFn configDecoder) (*ClusterStore, error) {
	cs := &ClusterStore{
		store: make(map[string]cluster),
		log: logging.WithFields(logging.Fields{
			"component": "endpoints-watcher-cache",
		}),
		enableEndpointSlices: enableEndpointSlices,
		k8sAPI:               k8sAPI,
		decodeFn:             decodeFn,
	}

	_, err := cs.k8sAPI.Secret().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			secret, ok := obj.(*v1.Secret)
			if !ok {
				cs.log.Errorf("Error processing 'Secret' object: got %#v, expected *corev1.Secret", secret)
				return
			}

			if secret.Type != consts.MirrorSecretType {
				cs.log.Tracef("Skipping Add event for 'Secret' object %s/%s: invalid type %s", secret.Namespace, secret.Name, secret.Type)
				return

			}

			clusterName, found := secret.GetLabels()[clusterNameLabel]
			if !found {
				cs.log.Tracef("Skipping Add event for 'Secret' object %s/%s: missing \"%s\" label", secret.Namespace, secret.Name, clusterNameLabel)
				return
			}

			if err := cs.addWatcher(clusterName, secret); err != nil {
				cs.log.Errorf("Error adding watcher for cluster %s: %v", clusterName, err)
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
					cs.log.Debugf("unable to get object from DeletedFinalStateUnknown %#v", obj)
					return
				}
				// If the zombie object is a `Secret` we can delete it.
				secret, ok = tombstone.Obj.(*v1.Secret)
				if !ok {
					cs.log.Debugf("DeletedFinalStateUnknown contained object that is not a Secret %#v", obj)
					return
				}
			}

			clusterName, found := secret.GetLabels()[clusterNameLabel]
			if !found {
				cs.log.Tracef("Skipping Delete event for 'Secret' object %s/%s: missing \"%s\" label", secret.Namespace, secret.Name, clusterNameLabel)
				return
			}

			cs.removeWatcher(clusterName)

		},
	})

	if err != nil {
		return nil, err
	}

	return cs, nil
}

// Get safely retrieves a watcher from the cache given a cluster name. It
// returns the watcher, cluster configuration, if the entry exists in the cache.
func (cs *ClusterStore) Get(clusterName string) (*EndpointsWatcher, string, string, bool) {
	cs.RLock()
	defer cs.RUnlock()
	s, found := cs.store[clusterName]
	return s.watcher, s.trustDomain, s.clusterDomain, found
}

// GetWatcher is a convenience method that given a cluster name only returns the
// associated EndpointsWatcher if it exists in the cache.
func (cs *ClusterStore) GetWatcher(clusterName string) (*EndpointsWatcher, bool) {
	cs.RLock()
	defer cs.RUnlock()
	s, found := cs.store[clusterName]
	return s.watcher, found
}

// GetLocalWatcher is a convenience method that retrieves the watcher associated
// with the local cluster. Its existence is assumed.
func (cs *ClusterStore) GetLocalWatcher() *EndpointsWatcher {
	cs.RLock()
	defer cs.RUnlock()
	return cs.store[LocalClusterKey].watcher
}

// GetClusterConfig is a convenience method that given a cluster name retrieves
// its associated configuration strings if the entry exists in the cache.
func (cs *ClusterStore) GetClusterConfig(clusterName string) (string, string, bool) {
	cs.RLock()
	defer cs.RUnlock()
	s, found := cs.store[clusterName]
	return s.trustDomain, s.clusterDomain, found
}

// AddLocalWatcher adds a watcher to the cache using the local cluster key.
func (cs *ClusterStore) AddLocalWatcher(stopCh chan<- struct{}, watcher *EndpointsWatcher, trustDomain, clusterDomain string) {
	cs.Lock()
	defer cs.Unlock()
	cs.store[LocalClusterKey] = cluster{
		watcher,
		trustDomain,
		clusterDomain,
		stopCh,
	}
}

// Len returns the number of entries in the cache
func (cs *ClusterStore) Len() int {
	cs.RLock()
	defer cs.RUnlock()
	return len(cs.store)
}

// removeWatcher is triggered by the cache's Secret informer when a secret is
// removed. Given a cluster name, it removes the entry from the cache after
// stopping the associated watcher.
func (cs *ClusterStore) removeWatcher(clusterName string) {
	cs.Lock()
	defer cs.Unlock()
	s, found := cs.store[clusterName]
	if !found {
		return
	}
	// For good measure, close the channel after stopping to ensure
	// informers are shut down.
	defer close(s.stopCh)
	s.watcher.Stop(s.stopCh)
	delete(cs.store, clusterName)
	cs.log.Tracef("Removed cluster %s from ClusterStore", clusterName)
}

// addWatcher is triggered by the cache's Secret informer when a secret is
// discovered for the first time. Given a cluster name and a Secret
// object, it creates an EndpointsWatcher for a remote cluster and syncs its
// informers before returning.
func (cs *ClusterStore) addWatcher(clusterName string, secret *v1.Secret) error {
	data, found := secret.Data[consts.ConfigKeyName]
	if !found {
		return errors.New("missing kubeconfig file")
	}

	clusterDomain, found := secret.GetAnnotations()[clusterDomainAnnotation]
	if !found {
		return fmt.Errorf("missing \"%s\" annotation", clusterDomainAnnotation)
	}

	trustDomain, found := secret.GetAnnotations()[trustDomainAnnotation]
	if !found {
		return fmt.Errorf("missing \"%s\" annotation", trustDomainAnnotation)
	}

	remoteAPI, metadataAPI, err := cs.decodeFn(data, cs.enableEndpointSlices)
	if err != nil {
		return err
	}

	stopCh := make(chan struct{}, 1)

	watcher, err := NewEndpointsWatcher(
		remoteAPI,
		metadataAPI,
		logging.WithFields(logging.Fields{
			"remote-cluster": clusterName,
		}),
		cs.enableEndpointSlices,
	)
	if err != nil {
		return err
	}

	cs.Lock()
	defer cs.Unlock()
	cs.store[clusterName] = cluster{
		watcher,
		trustDomain,
		clusterDomain,
		stopCh,
	}

	remoteAPI.Sync(stopCh)
	metadataAPI.Sync(stopCh)

	cs.log.Tracef("Added cluster %s to ClusterStore", clusterName)

	return nil
}

// decodeK8sConfigFromSecret implements the decoder function type. Given a byte
// buffer, it attempts to parse it as a kubeconfig file. If successful, returns
// a pair of API Server clients.
func decodeK8sConfigFromSecret(data []byte, enableEndpointSlices bool) (*k8s.API, *k8s.MetadataAPI, error) {
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
			k8s.ES, k8s.Pod, k8s.Svc, k8s.SP, k8s.Job, k8s.Srv,
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
