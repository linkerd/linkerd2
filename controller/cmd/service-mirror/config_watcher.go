package servicemirror

import (
	"fmt"
	"sync"

	"github.com/linkerd/linkerd2/controller/k8s"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
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
					return object.Type == MirrorSecretType

				case cache.DeletedFinalStateUnknown:
					if secret, ok := object.Obj.(*corev1.Secret); ok {
						return secret.Type == MirrorSecretType
					}
					return false
				default:
					return false
				}
			},

			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					if err := rcw.registerRemoteCluster(obj); err != nil {
						log.Errorf("Cannot register remote cluster: %s", err)
					}
				},
				DeleteFunc: func(obj interface{}) {
					if err := rcw.unregisterRemoteCluster(obj); err != nil {
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

func (rcw *RemoteClusterConfigWatcher) registerRemoteCluster(obj interface{}) error {
	secret := obj.(*corev1.Secret)
	config, name, err := parseRemoteClusterSecret(secret)
	if err != nil {
		return err
	}

	clientConfig, err := clientcmd.RESTConfigFromKubeConfig(config)
	if err != nil {
		return fmt.Errorf("unable to parse kube config: %s", err)
	}

	watcher, err := NewRemoteClusterServiceWatcher(rcw.k8sAPI, clientConfig, name, rcw.requeueLimit)
	if err != nil {
		return err
	}

	rcw.mutex.Lock()
	defer rcw.mutex.Unlock()

	rcw.clusterWatchers[name] = watcher
	watcher.Start()
	return nil

}

func (rcw *RemoteClusterConfigWatcher) unregisterRemoteCluster(obj interface{}) error {
	secret := obj.(*corev1.Secret)
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
	clusterName, hasClusterName := secret.Annotations[RemoteClusterNameLabel]
	config, hasConfig := secret.Data[ConfigKeyName]
	if !hasClusterName {
		return nil, "", fmt.Errorf("secret of type %s should contain key %s", MirrorSecretType, ConfigKeyName)
	}
	if !hasConfig {
		return nil, "", fmt.Errorf("secret should contain remote cluster name as annotation %s", RemoteClusterNameLabel)
	}
	return config, clusterName, nil
}
