package servicemirror

import (
	"fmt"
	"sync"

	"github.com/linkerd/linkerd2/controller/k8s"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
	sm "github.com/linkerd/linkerd2/pkg/servicemirror"
)

// RemoteClusterConfigWatcher watches for secrets of type MirrorSecretType
// and upon the detection of such secret created starts a RemoteClusterServiceWatcher
type RemoteClusterConfigWatcher struct {
	serviceMirrorNamespace string
	k8sAPI                 *k8s.API
	clusterWatchers        map[string]*RemoteClusterServiceWatcher
	requeueLimit           int
	sync.RWMutex
}

// NewRemoteClusterConfigWatcher Creates a new config watcher
func NewRemoteClusterConfigWatcher(serviceMirrorNamespace string, secretsInformer cache.SharedIndexInformer, k8sAPI *k8s.API, requeueLimit int) *RemoteClusterConfigWatcher {
	rcw := &RemoteClusterConfigWatcher{
		serviceMirrorNamespace: serviceMirrorNamespace,
		k8sAPI:                 k8sAPI,
		clusterWatchers:        map[string]*RemoteClusterServiceWatcher{},
		requeueLimit:           requeueLimit,
	}
	secretsInformer.AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch object := obj.(type) {
				case *corev1.Secret:
					return object.Type == consts.MirrorSecretType

				case cache.DeletedFinalStateUnknown:
					if secret, ok := object.Obj.(*corev1.Secret); ok {
						return secret.Type == consts.MirrorSecretType
					}
					return false
				default:
					return false
				}
			},

			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					secret := obj.(*corev1.Secret)
					if err := rcw.registerRemoteCluster(secret); err != nil {
						log.Errorf("Cannot register remote cluster: %s", err)
					}
				},
				DeleteFunc: func(obj interface{}) {
					secret, ok := obj.(*corev1.Secret)
					if !ok {
						tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
						if !ok {
							log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
							return
						}
						secret, ok = tombstone.Obj.(*corev1.Secret)
						if !ok {
							log.Errorf("DeletedFinalStateUnknown contained object that is not a Secret %#v", obj)
							return
						}
					}
					if err := rcw.unregisterRemoteCluster(secret, true); err != nil {
						log.Errorf("Cannot unregister remote cluster: %s", err)
					}
				},
				UpdateFunc: func(old, new interface{}) {
					oldSecret := old.(*corev1.Secret)
					newSecret := new.(*corev1.Secret)

					if oldSecret.ResourceVersion != newSecret.ResourceVersion {
						if err := rcw.unregisterRemoteCluster(oldSecret, false); err != nil {
							log.Errorf("Cannot unregister remote cluster: %s", err)
							return
						}

						if err := rcw.registerRemoteCluster(newSecret); err != nil {
							log.Errorf("Cannot register remote cluster: %s", err)
						}

					}

					//TODO: Handle update (it might be that the credentials have changed...)
				},
			},
		},
	)
	return rcw
}

// Stop Shuts down all created config and cluster watchers
func (rcw *RemoteClusterConfigWatcher) Stop() {
	rcw.Lock()
	defer rcw.Unlock()
	for _, watcher := range rcw.clusterWatchers {
		watcher.Stop(false)
	}
}

func (rcw *RemoteClusterConfigWatcher) registerRemoteCluster(secret *corev1.Secret) error {
	config, err := sm.ParseRemoteClusterSecret(secret)

	if err != nil {
		return err
	}

	clientConfig, err := clientcmd.RESTConfigFromKubeConfig(config.APIConfig)
	if err != nil {
		return fmt.Errorf("unable to parse kube config: %s", err)
	}

	rcw.Lock()
	defer rcw.Unlock()

	if _, ok := rcw.clusterWatchers[config.ClusterName]; ok {
		return fmt.Errorf("there is already a cluster with name %s being watcher. Please delete its config before attempting to register a new one", config.ClusterName)
	}

	watcher, err := NewRemoteClusterServiceWatcher(rcw.serviceMirrorNamespace, rcw.k8sAPI, clientConfig, config.ClusterName, rcw.requeueLimit, config.ClusterDomain)
	if err != nil {
		return err
	}

	rcw.clusterWatchers[config.ClusterName] = watcher
	if err := watcher.Start(); err != nil {
		return err
	}
	return nil

}

func (rcw *RemoteClusterConfigWatcher) unregisterRemoteCluster(secret *corev1.Secret, cleanState bool) error {
	config, err := sm.ParseRemoteClusterSecret(secret)

	if err != nil {
		return err
	}
	rcw.Lock()
	defer rcw.Unlock()
	if watcher, ok := rcw.clusterWatchers[config.ClusterName]; ok {
		watcher.Stop(cleanState)
	} else {
		return fmt.Errorf("cannot find watcher for cluser: %s", config.ClusterName)
	}
	delete(rcw.clusterWatchers, config.ClusterName)

	return nil
}
