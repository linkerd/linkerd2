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
	requeueLimit    int
	sync.RWMutex
	probeEvents chan interface{}
}

// NewRemoteClusterConfigWatcher Creates a new config watcher
func NewRemoteClusterConfigWatcher(k8sAPI *k8s.API, requeueLimit int, probeEvents chan interface{}) *RemoteClusterConfigWatcher {
	rcw := &RemoteClusterConfigWatcher{
		k8sAPI:          k8sAPI,
		clusterWatchers: map[string]*RemoteClusterServiceWatcher{},
		requeueLimit:    requeueLimit,
		probeEvents:     probeEvents,
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
	config, err := parseRemoteClusterSecret(secret)
	if err != nil {
		return err
	}

	clientConfig, err := clientcmd.RESTConfigFromKubeConfig(config.apiConfig)
	if err != nil {
		return fmt.Errorf("unable to parse kube config: %s", err)
	}

	rcw.Lock()
	defer rcw.Unlock()

	if _, ok := rcw.clusterWatchers[config.clusterName]; ok {
		return fmt.Errorf("there is already a cluster with name %s being watcher. Please delete its config before attempting to register a new one", config.clusterName)
	}

	watcher, err := NewRemoteClusterServiceWatcher(rcw.k8sAPI, clientConfig, config.clusterName, rcw.requeueLimit, config.clusterDomain, config.probePort, config.probePath, config.probePeriodSeconds, rcw.probeEvents)
	if err != nil {
		return err
	}

	rcw.clusterWatchers[config.clusterName] = watcher
	watcher.Start()
	return nil

}

func (rcw *RemoteClusterConfigWatcher) unregisterRemoteCluster(secret *corev1.Secret, cleanState bool) error {
	config, err := parseRemoteClusterSecret(secret)
	if err != nil {
		return err
	}
	rcw.Lock()
	defer rcw.Unlock()
	if watcher, ok := rcw.clusterWatchers[config.clusterName]; ok {
		watcher.Stop(cleanState)
	} else {
		return fmt.Errorf("cannot find watcher for cluser: %s", config.clusterName)
	}
	delete(rcw.clusterWatchers, config.clusterName)

	return nil
}

type watchedClusterConfig struct {
	apiConfig          []byte
	clusterName        string
	clusterDomain      string
	probePort          int32
	probePath          string
	probePeriodSeconds int32
}

func parseRemoteClusterSecret(secret *corev1.Secret) (*watchedClusterConfig, error) {
	clusterName, hasClusterName := secret.Annotations[consts.RemoteClusterNameLabel]
	config, hasConfig := secret.Data[consts.ConfigKeyName]
	domain, hasDomain := secret.Annotations[consts.RemoteClusterDomainAnnotation]
	if !hasClusterName {
		return nil, fmt.Errorf("secret of type %s should contain key %s", consts.MirrorSecretType, consts.ConfigKeyName)
	}
	if !hasConfig {
		return nil, fmt.Errorf("secret should contain remote cluster name as annotation %s", consts.RemoteClusterNameLabel)
	}
	if !hasDomain {
		return nil, fmt.Errorf("secret should contain remote cluster domain as annotation %s", consts.RemoteClusterDomainAnnotation)
	}

	//TODO Externalize the port,path and period through  the get-credentials command
	return &watchedClusterConfig{
		apiConfig:          config,
		clusterName:        clusterName,
		clusterDomain:      domain,
		probePort:          80,
		probePath:          "/nginx-health",
		probePeriodSeconds: 3,
	}, nil
}
