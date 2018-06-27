package ca

import (
	"time"

	"github.com/runconduit/conduit/controller/k8s"
	pkgK8s "github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type CertificateController struct {
	namespace   string
	k8sAPI      *k8s.API
	syncHandler func(key string) error
	queue       workqueue.RateLimitingInterface
}

func NewCertificateController(conduitNamespace string, k8sAPI *k8s.API) *CertificateController {
	c := &CertificateController{
		namespace: conduitNamespace,
		k8sAPI:    k8sAPI,
		queue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(), "certificates"),
	}

	k8sAPI.Pod().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.handlePodAdd,
			UpdateFunc: c.handlePodUpdate,
		},
	)

	k8sAPI.CM().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.handleConfigMapAdd,
			UpdateFunc: c.handleConfigMapUpdate,
			DeleteFunc: c.handleConfigMapDelete,
		},
	)

	c.syncHandler = c.syncNamespace

	return c
}

func (c *CertificateController) Run(stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	log.Info("starting certificate controller")
	defer log.Info("shutting down certificate controller")

	go wait.Until(c.worker, time.Second, stopCh)

	<-stopCh
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
	conduitConfigMap, err := c.k8sAPI.CM().Lister().ConfigMaps(c.namespace).
		Get(pkgK8s.TLSTrustAnchorConfigMapName)
	if apierrors.IsNotFound(err) {
		log.Warnf("configmap [%s] not found in namespace [%s]",
			pkgK8s.TLSTrustAnchorConfigMapName, c.namespace)
		return nil
	}
	if err != nil {
		return err
	}

	configMap := &v1.ConfigMap{
		ObjectMeta: meta.ObjectMeta{Name: pkgK8s.TLSTrustAnchorConfigMapName},
		Data:       conduitConfigMap.Data,
	}

	log.Debugf("adding configmap [%s] to namespace [%s]",
		pkgK8s.TLSTrustAnchorConfigMapName, ns)
	_, err = c.k8sAPI.Client.CoreV1().ConfigMaps(ns).Create(configMap)
	if apierrors.IsAlreadyExists(err) {
		_, err = c.k8sAPI.Client.CoreV1().ConfigMaps(ns).Update(configMap)
	}

	return err
}

func (c *CertificateController) handlePodAdd(obj interface{}) {
	pod := obj.(*v1.Pod)
	if c.isInjectedPod(pod) && !c.filterNamespace(pod.Namespace) {
		c.queue.Add(pod.Namespace)
	}
}

func (c *CertificateController) handlePodUpdate(oldObj, newObj interface{}) {
	c.handlePodAdd(newObj)
}

func (c *CertificateController) handleConfigMapAdd(obj interface{}) {
	cm := obj.(*v1.ConfigMap)
	if cm.Namespace == c.namespace && cm.Name == pkgK8s.TLSTrustAnchorConfigMapName {
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

func (c *CertificateController) handleConfigMapUpdate(oldObj, newObj interface{}) {
	c.handleConfigMapAdd(newObj)
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

	if configMap.Name == pkgK8s.TLSTrustAnchorConfigMapName && configMap.Namespace != c.namespace {
		injected, err := c.isInjectedNamespace(configMap.Namespace)
		if err != nil {
			log.Errorf("error getting pods in namespace [%s]: %s", configMap.Namespace, err)
			return
		}
		if injected {
			log.Infof("configmap [%s] in namespace [%s] deleted; recreating it",
				pkgK8s.TLSTrustAnchorConfigMapName, configMap.Namespace)
			c.queue.Add(configMap.Namespace)
		}
	}
}

func (c *CertificateController) getInjectedNamespaces() ([]string, error) {
	pods, err := c.k8sAPI.Pod().Lister().List(labels.Everything())
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
	pods, err := c.k8sAPI.Pod().Lister().Pods(ns).List(labels.Everything())
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
	return pkgK8s.GetControllerNs(pod.ObjectMeta) == c.namespace
}
