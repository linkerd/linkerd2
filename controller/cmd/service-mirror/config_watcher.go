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

type RemoteClusterConfigWatcher struct {
	k8sAPI          *k8s.API
	clusterWatchers map[string]*RemoteClusterServiceWatcher
	mutex           *sync.Mutex
}

func NewRemoteClusterConfigWatcher(k8sAPI *k8s.API) *RemoteClusterConfigWatcher {
	rcw := &RemoteClusterConfigWatcher{
		k8sAPI:          k8sAPI,
		mutex:           &sync.Mutex{},
		clusterWatchers: map[string]*RemoteClusterServiceWatcher{},
	}
	k8sAPI.Secret().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
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
			},
		},
	)
	return rcw
}

func (rcw *RemoteClusterConfigWatcher) Stop() {
	rcw.mutex.Lock()
	defer rcw.mutex.Unlock()
	for _, watcher := range rcw.clusterWatchers {
		watcher.Stop()
	}
}

func (rcw *RemoteClusterConfigWatcher) registerRemoteCluster(obj interface{}) error {
	if secret := asConfigSecret(obj); secret != nil {
		config, name, err := parseRemoteClusterSecret(secret)
		if err != nil {
			return err
		}

		clientConfig, err := clientcmd.RESTConfigFromKubeConfig(config)
		if err != nil {
			return fmt.Errorf("unable to parse kube config: %s", err)
		}

		watcher, err := NewRemoteClusterServiceWatcher(rcw.k8sAPI, clientConfig, name)
		if err != nil {
			return err
		}

		rcw.mutex.Lock()
		defer rcw.mutex.Unlock()

		rcw.clusterWatchers[name] = watcher
		watcher.Start()
		return nil

	}
	return nil
}

func (rcw *RemoteClusterConfigWatcher) unregisterRemoteCluster(obj interface{}) error {
	if secret := asConfigSecret(obj); secret != nil {
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

	}
	return nil
}

func asConfigSecret(obj interface{}) *corev1.Secret {
	switch secret := obj.(type) {
	case *corev1.Secret:
		{
			if secret.Type == MirrorSecretType {
				return secret
			}
			return nil
		}
	default:
		return nil
	}
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
