package importreconciler

import (
	"context"
	"fmt"

	linkv1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"
	sm "github.com/linkerd/linkerd2/pkg/servicemirror"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

/* Events */
type (
	clusterRegistration struct {
		link *linkv1alpha1.Link
	}
	clusterUpdate struct {
		link    *linkv1alpha1.Link
		deleted bool
	}

	clusterDelete struct {
		clusterName string
	}

	serviceUpdate struct {
		cluster  string
		svc      *corev1.Service
		exported bool
	}
)

func (sw *ServiceImportWatcher) updateOrDeleteCluster(link *linkv1alpha1.Link, deleted bool) error {
	sw.log.Debugf("registering cluster %s", link.Spec.TargetClusterName)
	// When the cluster is deleted, we remove it from the cache and remove it
	// from all existing ServiceImports.
	if deleted {
		clusterName := link.Spec.TargetClusterName
		sw.RLock()
		defer sw.RUnlock()
		cluster, found := sw.clusters[clusterName]
		if !found {
			return nil
		}

		selector, err := metav1.LabelSelectorAsSelector(&link.Spec.ClusterAgnosticSelector)
		if err != nil {
			return err
		}

		exportedServices, err := cluster.client.Svc().Lister().List(selector)
		// Queue up services for deletion
		for _, svc := range exportedServices {
			sw.eventsQueue.Add(&serviceUpdate{cluster.name, svc, false})
		}
		sw.eventsQueue.Add(&clusterDelete{cluster.name})
		return nil
	}

	// When the link has been updated (e.g. to point to a new secret), we simply
	// re-init it without changing the existing state.
	return sw.createOrUpdateCluster(link)
}

func (sw *ServiceImportWatcher) createOrUpdateCluster(link *linkv1alpha1.Link) error {
	sw.log.Debugf("registering cluster %s", link.Spec.TargetClusterName)
	remoteAPI, err := createK8sClientFromConfig(sw.localClient.Client, sw.multiclusterNamespace, link)
	if err != nil {
		return err
	}

	cluster := &remoteCluster{
		name:   link.Spec.TargetClusterName,
		link:   link,
		client: remoteAPI,
		log: sw.log.WithFields(logging.Fields{
			"cluster": link.Spec.TargetClusterName,
		}),
	}

	cluster.svcHandler, err = cluster.client.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			sw.queueServiceCreated(obj, cluster)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			sw.queueServiceUpdated(oldObj, newObj, cluster)
		},
		DeleteFunc: func(obj interface{}) {
			sw.queueServiceDeleted(obj, cluster)
		},
	})
	if err != nil {
		return fmt.Errorf("failed to register callbacks for link %s: %w", link.Name, err)
	}

	sw.Lock()
	defer sw.Unlock()
	sw.clusters[cluster.name] = cluster
	sw.log.Infof("registered cluster %s", link.Spec.TargetClusterName)
	return nil
}

func (sw *ServiceImportWatcher) queueServiceCreated(obj interface{}, cluster *remoteCluster) {
	svc := obj.(*corev1.Service)
	if !cluster.isClusterAgnostic(svc.Labels) {
		// We should
		return
	}

	sw.eventsQueue.Add(&serviceUpdate{cluster.name, svc, true})
}

func (sw *ServiceImportWatcher) queueServiceDeleted(obj interface{}, cluster *remoteCluster) {
	svc := obj.(*corev1.Service)
	if !cluster.isClusterAgnostic(svc.Labels) {
		// We should
		return
	}

	sw.eventsQueue.Add(&serviceUpdate{cluster.name, svc, false})
}

func (sw *ServiceImportWatcher) queueServiceUpdated(oldObj, newObj interface{}, cluster *remoteCluster) {
	oldSvc := oldObj.(*corev1.Service)
	newSvc := newObj.(*corev1.Service)
	exported := cluster.isClusterAgnostic(newSvc.Labels)
	// When the service switched to being unexported, it means we need to queue
	// up an unexport event
	if !exported && cluster.isClusterAgnostic(oldSvc.Labels) {
		sw.eventsQueue.Add(&serviceUpdate{cluster.name, newSvc, false})
		return
	}

	sw.eventsQueue.Add(&serviceUpdate{cluster.name, newSvc, true})
}

func (rc *remoteCluster) isClusterAgnostic(l map[string]string) bool {
	if len(rc.link.Spec.ClusterAgnosticSelector.MatchExpressions)+len(rc.link.Spec.ClusterAgnosticSelector.MatchLabels) == 0 {
		return false
	}

	clusterAgnosticSel, err := metav1.LabelSelectorAsSelector(&rc.link.Spec.ClusterAgnosticSelector)
	if err != nil {
		rc.log.Errorf("Invalid selector: %s", err)
		return false
	}

	return clusterAgnosticSel.Matches(labels.Set(l))
}

func (sw *ServiceImportWatcher) registerCallbacks() error {
	sw.log.Info("registering callbacks")
	sw.Lock()
	defer sw.Unlock()
	var err error
	sw.linkHandler, err = sw.localClient.Link().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(link interface{}) {
			sw.eventsQueue.Add(&clusterRegistration{link.(*linkv1alpha1.Link)})
		},
		UpdateFunc: func(_, newL interface{}) {
			sw.eventsQueue.Add(&clusterUpdate{newL.(*linkv1alpha1.Link), false})
		},
		DeleteFunc: func(link interface{}) {
			sw.eventsQueue.Add(&clusterUpdate{link.(*linkv1alpha1.Link), true})
		},
	})
	return err
}

func (sw *ServiceImportWatcher) deregisterCallbacks() error {
	sw.Lock()
	defer sw.Unlock()
	var err error
	if sw.linkHandler != nil {
		err = sw.localClient.Link().Informer().RemoveEventHandler(sw.linkHandler)
		if err != nil {
			return err
		}
	}
	return nil
}

func createK8sClientFromConfig(client kubernetes.Interface, secretNamespace string, link *linkv1alpha1.Link) (*k8s.API, error) {
	secret, err := client.CoreV1().Secrets(secretNamespace).Get(context.TODO(), link.Spec.ClusterCredentialsSecret, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to load credentials secret %s: %w", link.Spec.ClusterCredentialsSecret, err)
	}
	creds, err := sm.ParseRemoteClusterSecret(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to parse credentials %s: %w", link.Name, err)
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(creds)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kube config %s: %w", link.Name, err)
	}

	remoteAPI, err := k8s.InitializeAPIForConfig(context.TODO(), cfg, false, link.Spec.TargetClusterName, k8s.Svc)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize api for target cluster %s: %w", link.Spec.TargetClusterName, err)
	}

	go func() {
		remoteAPI.Sync(nil)
	}()

	return remoteAPI, nil
}
