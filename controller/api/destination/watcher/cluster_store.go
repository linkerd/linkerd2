package watcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

type (
	// ClusterStore indexes clusters in which remote service discovery may be
	// performed. For each store item, an EndpointsWatcher is created to read
	// state directly from the respective cluster's API Server. In addition,
	// each store item has some associated and immutable configuration that is
	// required for service discovery.
	ClusterStore struct {
		// Protects against illegal accesses
		sync.RWMutex

		secrets              cache.SharedIndexInformer
		store                map[string]remoteCluster
		enableEndpointSlices bool
		log                  *logging.Entry

		// Function used to parse a kubeconfig from a byte buffer. Based on the
		// kubeconfig, it creates API Server clients
		decodeFn configDecoder
	}

	// remoteCluster is a helper struct that represents a store item
	remoteCluster struct {
		watcher *EndpointsWatcher
		config  clusterConfig

		// Used to signal shutdown to the associated watcher's informers
		stopCh chan<- struct{}
	}

	// clusterConfig holds immutable configuration for a given cluster
	clusterConfig struct {
		TrustDomain   string
		ClusterDomain string
	}

	// configDecoder is the type of a function that given a byte buffer, returns
	// a pair of API Server clients. The cache uses this function to dynamically
	// create clients after discovering a Secret.
	configDecoder = func(data []byte, enableEndpointSlices bool) (*k8s.API, *k8s.MetadataAPI, error)
)

const (
	clusterNameLabel        = "multicluster.linkerd.io/cluster-name"
	trustDomainAnnotation   = "multicluster.linkerd.io/trust-domain"
	clusterDomainAnnotation = "multicluster.linkerd.io/cluster-domain"
)

// NewClusterStore creates a new (empty) ClusterStore. It
// requires a Kubernetes API Server client instantiated for the local cluster.
//
// When created, a pair of event handlers are registered for the local cluster's
// Secret informer. The event handlers are responsible for driving the discovery
// of remote clusters and their configuration
func NewClusterStore(client kubernetes.Interface, namespace string, enableEndpointSlices bool) (*ClusterStore, error) {
	return newClusterStoreWithDecoder(client, namespace, enableEndpointSlices, decodeK8sConfigFromSecret)
}

func (cs *ClusterStore) Sync(stopCh <-chan struct{}) {
	go func() {
		cs.secrets.Run(stopCh)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cs.log.Infof("waiting for cluster store to sync")
	if !cache.WaitForCacheSync(ctx.Done(), cs.secrets.HasSynced) {
		cs.log.Fatal("failed to sync cluster store")
	}
	cs.log.Infof("cluster store synced")
}

// newClusterStoreWithDecoder is a helper function that allows the creation of a
// store with an arbitrary `configDecoder` function.
func newClusterStoreWithDecoder(client kubernetes.Interface, namespace string, enableEndpointSlices bool, decodeFn configDecoder) (*ClusterStore, error) {
	secretsInformerFactory := informers.NewSharedInformerFactoryWithOptions(client, k8s.ResyncTime, informers.WithNamespace(namespace))
	secrets := secretsInformerFactory.Core().V1().Secrets().Informer()

	cs := &ClusterStore{
		store: make(map[string]remoteCluster),
		log: logging.WithFields(logging.Fields{
			"component": "cluster-store",
		}),
		enableEndpointSlices: enableEndpointSlices,
		secrets:              secrets,
		decodeFn:             decodeFn,
	}

	_, err := cs.secrets.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			secret, ok := obj.(*v1.Secret)
			if !ok {
				cs.log.Errorf("Error processing 'Secret' object: got %#v, expected *corev1.Secret", secret)
				return
			}

			if secret.Type != pkgK8s.MirrorSecretType {
				cs.log.Tracef("Skipping Add event for 'Secret' object %s/%s: invalid type %s", secret.Namespace, secret.Name, secret.Type)
				return

			}

			clusterName, found := secret.GetLabels()[clusterNameLabel]
			if !found {
				cs.log.Tracef("Skipping Add event for 'Secret' object %s/%s: missing \"%s\" label", secret.Namespace, secret.Name, clusterNameLabel)
				return
			}

			if err := cs.addCluster(clusterName, secret); err != nil {
				cs.log.Errorf("Error adding cluster %s to store: %v", clusterName, err)
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
					cs.log.Debugf("Unable to get object from DeletedFinalStateUnknown %#v", obj)
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

			cs.removeCluster(clusterName)

		},
	})

	if err != nil {
		return nil, err
	}

	return cs, nil
}

// Get safely retrieves a store item given a cluster name.
func (cs *ClusterStore) Get(clusterName string) (*EndpointsWatcher, clusterConfig, bool) {
	cs.RLock()
	defer cs.RUnlock()
	cw, found := cs.store[clusterName]
	return cw.watcher, cw.config, found
}

// removeCluster is triggered by the cache's Secret informer when a secret is
// removed. Given a cluster name, it removes the entry from the cache after
// stopping the associated watcher.
func (cs *ClusterStore) removeCluster(clusterName string) {
	cs.Lock()
	defer cs.Unlock()
	r, found := cs.store[clusterName]
	if !found {
		return
	}
	r.watcher.removeHandlers()
	close(r.stopCh)
	delete(cs.store, clusterName)
	cs.log.Infof("Removed cluster %s from ClusterStore", clusterName)
}

// addCluster is triggered by the cache's Secret informer when a secret is
// discovered for the first time. Given a cluster name and a Secret
// object, it creates an EndpointsWatcher for a remote cluster and syncs its
// informers before returning.
func (cs *ClusterStore) addCluster(clusterName string, secret *v1.Secret) error {
	data, found := secret.Data[pkgK8s.ConfigKeyName]
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
	cs.store[clusterName] = remoteCluster{
		watcher,
		clusterConfig{
			trustDomain,
			clusterDomain,
		},
		stopCh,
	}

	go func() {
		remoteAPI.Sync(stopCh)
		metadataAPI.Sync(stopCh)
	}()

	cs.log.Infof("Added cluster %s to ClusterStore", clusterName)

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
