package ca

import (
	"fmt"
	"time"

	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type CertificateController struct {
	client    kubernetes.Interface
	namespace string

	syncHandler func(key string) error

	podLister       corelisters.PodLister
	podListerSynced cache.InformerSynced

	configMapLister       corelisters.ConfigMapLister
	configMapListerSynced cache.InformerSynced

	queue workqueue.RateLimitingInterface
}

func NewCertificateController(
	client kubernetes.Interface,
	conduitNamespace string,
	podInformer coreinformers.PodInformer,
	configMapInformer coreinformers.ConfigMapInformer,
) *CertificateController {
	c := &CertificateController{
		client:                client,
		namespace:             conduitNamespace,
		podLister:             podInformer.Lister(),
		podListerSynced:       podInformer.Informer().HasSynced,
		configMapLister:       configMapInformer.Lister(),
		configMapListerSynced: configMapInformer.Informer().HasSynced,
		queue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(), "certificates"),
	}

	podInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				c.handlePodUpdate(obj.(*v1.Pod))
			},
			UpdateFunc: func(_, obj interface{}) {
				c.handlePodUpdate(obj.(*v1.Pod))
			},
		},
	)

	configMapInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				c.handleConfigMapUpdate(obj.(*v1.ConfigMap))
			},
			UpdateFunc: func(_, obj interface{}) {
				c.handleConfigMapUpdate(obj.(*v1.ConfigMap))
			},
			DeleteFunc: c.handleConfigMapDelete,
		},
	)

	c.syncHandler = c.syncNamespace

	return c
}

func (c *CertificateController) Run(stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	log.Info("starting certificate controller")
	defer log.Info("shutting down certificate controller")

	if !cache.WaitForCacheSync(stopCh, c.podListerSynced, c.configMapListerSynced) {
		return fmt.Errorf("timed out waiting for cache to sync")
	}
	log.Info("caches are synced")

	go wait.Until(c.worker, time.Second, stopCh)

	<-stopCh
	return nil
}

func (c *CertificateController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *CertificateController) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	if err != nil {
		log.Errorf("error syncing config map: %s", err)
		c.queue.AddRateLimited(key)
		return true
	}

	c.queue.Forget(key)
	return true
}

func (c *CertificateController) syncNamespace(ns string) error {
	conduitConfigMap, err := c.configMapLister.ConfigMaps(c.namespace).
		Get(k8s.CertificateBundleName)
	if apierrors.IsNotFound(err) {
		log.Warnf("configmap [%s] not found in namespace [%s]",
			k8s.CertificateBundleName, c.namespace)
		return nil
	}
	if err != nil {
		return err
	}

	configMap := &v1.ConfigMap{
		ObjectMeta: meta.ObjectMeta{Name: k8s.CertificateBundleName},
		Data:       conduitConfigMap.Data,
	}

	log.Debugf("adding configmap [%s] to namespace [%s]",
		k8s.CertificateBundleName, ns)
	_, err = c.client.CoreV1().ConfigMaps(ns).Create(configMap)
	if apierrors.IsAlreadyExists(err) {
		_, err = c.client.CoreV1().ConfigMaps(ns).Update(configMap)
	}

	return err
}

func (c *CertificateController) handlePodUpdate(pod *v1.Pod) {
	if c.isInjectedPod(pod) && !c.filterNamespace(pod.Namespace) {
		c.queue.Add(pod.Namespace)
	}
}

func (c *CertificateController) handleConfigMapUpdate(cm *v1.ConfigMap) {
	if cm.Namespace == c.namespace && cm.Name == k8s.CertificateBundleName {
		namespaces, err := c.getInjectedNamespaces()
		if err != nil {
			log.Errorf("error getting namespaces: %s", err)
			return
		}

		for _, ns := range namespaces {
			c.queue.Add(ns)
		}
	}
}

func (c *CertificateController) handleConfigMapDelete(obj interface{}) {
	configMap, ok := obj.(*v1.ConfigMap)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Warnf("couldn't get object from tombstone: %+v", obj)
			return
		}
		configMap, ok = tombstone.Obj.(*v1.ConfigMap)
		if !ok {
			log.Warnf("object is not a configmap: %+v", tombstone.Obj)
			return
		}
	}

	if configMap.Name == k8s.CertificateBundleName && configMap.Namespace != c.namespace {
		injected, err := c.isInjectedNamespace(configMap.Namespace)
		if err != nil {
			log.Errorf("error getting pods in namespace [%s]: %s", configMap.Namespace, err)
			return
		}
		if injected {
			log.Infof("configmap [%s] in namespace [%s] deleted; recreating it",
				k8s.CertificateBundleName, configMap.Namespace)
			c.queue.Add(configMap.Namespace)
		}
	}
}

func (c *CertificateController) getInjectedNamespaces() ([]string, error) {
	pods, err := c.podLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	namespaces := make(sets.String)
	for _, pod := range pods {
		if !c.filterNamespace(pod.Namespace) && c.isInjectedPod(pod) {
			namespaces.Insert(pod.Namespace)
		}
	}

	return namespaces.List(), nil
}

func (c *CertificateController) filterNamespace(ns string) bool {
	for _, filter := range []string{c.namespace, "kube-system", "kube-public"} {
		if ns == filter {
			return true
		}
	}
	return false
}

func (c *CertificateController) isInjectedNamespace(ns string) (bool, error) {
	pods, err := c.podLister.Pods(ns).List(labels.Everything())
	if err != nil {
		return false, err
	}
	for _, pod := range pods {
		if c.isInjectedPod(pod) {
			return true, nil
		}
	}
	return false, nil
}

func (c *CertificateController) isInjectedPod(pod *v1.Pod) bool {
	_, ok := pod.Annotations[k8s.CreatedByAnnotation]
	return ok
}
