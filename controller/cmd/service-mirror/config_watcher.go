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
)

// RemoteClusterConfigWatcher watches for secrets of type MirrorSecretType
// and upon the detection of such secret created starts a RemoteClusterServiceWatcher
type RemoteClusterConfigWatcher struct {
	k8sAPI          *k8s.API
	clusterWatchers map[string]*RemoteClusterServiceWatcher
	mutex           *sync.Mutex
	requeueLimit    int
}

// NewRemoteClusterConfigWatcher Creates a new config watcher
func NewRemoteClusterConfigWatcher(k8sAPI *k8s.API, requeueLimit int) *RemoteClusterConfigWatcher {
	rcw := &RemoteClusterConfigWatcher{
		k8sAPI:          k8sAPI,
		mutex:           &sync.Mutex{},
		clusterWatchers: map[string]*RemoteClusterServiceWatcher{},
		requeueLimit:    requeueLimit,
	}
	k8sAPI.Secret().Informer().AddEventHandler(
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
					if err := rcw.unregisterRemoteCluster(secret); err != nil {
						log.Errorf("Cannot unregister remote cluster: %s", err)
					}
				},
				UpdateFunc: func(_, obj interface{}) {
					//TODO: Handle update (it might be that the credentials have changed...)
				},
			},
		},
	)
	return rcw
}

// Stop Shuts down all created config and cluster watchers
func (rcw *RemoteClusterConfigWatcher) Stop() {
	rcw.mutex.Lock()
	defer rcw.mutex.Unlock()
	for _, watcher := range rcw.clusterWatchers {
		watcher.Stop()
	}
}

func (rcw *RemoteClusterConfigWatcher) registerRemoteCluster(secret *corev1.Secret) error {
	config, name, err := parseRemoteClusterSecret(secret)
	if err != nil {
		return err
	}

	clientConfig, err := clientcmd.RESTConfigFromKubeConfig(config)
	if err != nil {
		return fmt.Errorf("unable to parse kube config: %s", err)
	}

	rcw.mutex.Lock()
	defer rcw.mutex.Unlock()

	if _, ok := rcw.clusterWatchers[name]; ok {
		return fmt.Errorf("there is already a cluster with name %s being watcher. Please delete its config before attempting to register a new one", name)
	}

	watcher, err := NewRemoteClusterServiceWatcher(rcw.k8sAPI, clientConfig, name, rcw.requeueLimit)
	if err != nil {
		return err
	}

	rcw.clusterWatchers[name] = watcher
	watcher.Start()
	return nil

}

func (rcw *RemoteClusterConfigWatcher) unregisterRemoteCluster(secret *corev1.Secret) error {
	_, name, err := parseRemoteClusterSecret(secret)
	if err != nil {
		return err
	}
	rcw.mutex.Lock()
	defer rcw.mutex.Unlock()
	if watcher, ok := rcw.clusterWatchers[name]; ok {
		watcher.Stop()
	} else {
		return fmt.Errorf("cannot find watcher for cluser: %s", name)
	}
	delete(rcw.clusterWatchers, name)

	return nil
}

func parseRemoteClusterSecret(secret *corev1.Secret) ([]byte, string, error) {
	clusterName, hasClusterName := secret.Annotations[consts.RemoteClusterNameLabel]
	config, hasConfig := secret.Data[consts.ConfigKeyName]
	if !hasClusterName {
		return nil, "", fmt.Errorf("secret of type %s should contain key %s", consts.MirrorSecretType, consts.ConfigKeyName)
	}
	if !hasConfig {
		return nil, "", fmt.Errorf("secret should contain remote cluster name as annotation %s", consts.RemoteClusterNameLabel)
	}
	return config, clusterName, nil
}
