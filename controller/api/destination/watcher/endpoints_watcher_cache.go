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
	EndpointsWatcherCache struct {
		sync.RWMutex

		watchers map[string]struct {
			store         *EndpointsWatcher
			trustDomain   string
			clusterDomain string
		}

		enableEndpointSlices bool
		log                  *logging.Entry
	}
)

const (
	clusterNameLabel        = "multicluster.linkerd.io/cluster-name"
	trustDomainAnnotation   = "multicluster.linkerd.io/trust-domain"
	clusterDomainAnnotation = "multicluster.linkerd.io/cluster-domain"
)

func NewEndpointsWatcherCache(k8sAPI *k8s.API, enableEndpointSlices bool) (*EndpointsWatcherCache, error) {
	ewc := &EndpointsWatcherCache{
		watchers: make(map[string]struct {
			store         *EndpointsWatcher
			trustDomain   string
			clusterDomain string
		}),
		log: logging.WithFields(logging.Fields{
			"component": "endpoints-watcher-cache",
		}),
		enableEndpointSlices: enableEndpointSlices,
	}

	_, err := k8sAPI.Secret().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			secret, ok := obj.(*v1.Secret)
			if !ok {
				ewc.log.Errorf("Error processing 'Secret' object: got %#v, expected *corev1.Secret", secret)
				return
			}

			clusterName, found := secret.GetLabels()[clusterNameLabel]
			if !found {
				ewc.log.Tracef("Skipping Add event for 'Secret' object: missing \"%s\" label", clusterNameLabel)
				return
			}

			if err := ewc.addWatcher(clusterName, secret); err != nil {
				ewc.log.Errorf("Error processing 'Secret' object: %w", err)
			}

		},
		DeleteFunc: func(obj interface{}) {
			secret, ok := obj.(*v1.Secret)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					ewc.log.Errorf("unable to get object from DeletedFinalStateUnknown %#v", obj)
					return
				}
				secret, ok = tombstone.Obj.(*v1.Secret)
				if !ok {
					ewc.log.Errorf("DeletedFinalStateUnknown contained object that is not a Secret %#v", obj)
					return
				}
			}

			clusterName, found := secret.GetLabels()[clusterNameLabel]
			if !found {
				ewc.log.Tracef("Skipping Delete event for 'Secret' object: missing \"%s\" label", clusterNameLabel)
				return
			}

			ewc.removeWatcher(clusterName)

		},
	})

	if err != nil {
		return nil, err
	}

	return ewc, nil
}

func (ewc *EndpointsWatcherCache) removeWatcher(clusterName string) {
	ewc.Lock()
	defer ewc.Unlock()
	delete(ewc.watchers, clusterName)
}

func (ewc *EndpointsWatcherCache) addWatcher(clusterName string, secret *v1.Secret) error {
	clusterDomain, found := secret.GetAnnotations()[clusterDomainAnnotation]
	if !found {
		return fmt.Errorf("missing \"%s\" annotation", clusterDomainAnnotation)
	}

	trustDomain, found := secret.GetAnnotations()[trustDomainAnnotation]
	if !found {
		return fmt.Errorf("missing \"%s\" annotation", trustDomainAnnotation)
	}

	data, found := secret.Data[consts.ConfigKeyName]
	if !found {
		return errors.New("missing kubeconfig file")
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return fmt.Errorf("unable to parse kubeconfig: %w", err)
	}

	ctx := context.Background()
	var remoteAPI *k8s.API
	if ewc.enableEndpointSlices {
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
		return fmt.Errorf("unable to initialize api for remote cluster %s: %w", clusterName, err)
	}

	metadataAPI, err := k8s.InitializeMetadataAPIForConfig(cfg, k8s.Node, k8s.RS)
	if err != nil {
		return fmt.Errorf("unable to initialize metadata api for remote cluster %s: %w", clusterName, err)
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
		return fmt.Errorf("unable to initialize endpoints watcher for remote cluster %s: %w", clusterName, err)
	}

	ewc.Lock()
	ewc.watchers[clusterName] = struct {
		store         *EndpointsWatcher
		trustDomain   string
		clusterDomain string
	}{
		store:         watcher,
		trustDomain:   trustDomain,
		clusterDomain: clusterDomain,
	}
	ewc.Unlock()

	remoteAPI.Sync(nil)
	metadataAPI.Sync(nil)

	return nil

}
