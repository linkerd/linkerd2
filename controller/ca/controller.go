package ca

import (
	"fmt"
	"strings"
	"time"

	appsv1beta2 "k8s.io/api/apps/v1beta2"
	"github.com/runconduit/conduit/controller/k8s"
	pkgK8s "github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type CertificateController struct {
	namespace   string
	k8sAPI      *k8s.API
	ca *CA
	trustAnchorsPEM string
	syncHandler func(key string) error

	// The queue is keyed on a string. If the string doesn't contain any dots
	// then it is a namespace name and the task is to create the CA bundle
	// configmap in that namespace. Otherwise the string must be of the form
	// "$podOwner.$podKind.$podNamespace" and the task is to create the secret
	// for that pod owner.
	queue       workqueue.RateLimitingInterface
}

func NewCertificateController(conduitNamespace string, k8sAPI *k8s.API) (*CertificateController, error) {
	ca, err := NewCA()
	if err != nil {
		return nil, err
	}

	c := &CertificateController{
		namespace: conduitNamespace,
		k8sAPI:    k8sAPI,
		ca: ca,
		trustAnchorsPEM: ca.TrustAnchorPEM(),
		queue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(), "certificates"),
	}

	// TODO: Handle deletions.
	handlePodOwnerAddUpdate := cache.ResourceEventHandlerFuncs{
		AddFunc:    c.handlePodOwnerAdd,
		UpdateFunc: c.handlePodOwnerUpdate,
	}

	// Watch pod owners, instead of just pods, so that we can create the
	// secret for each pod owner as soon as the pod owner is created, instead
	// of later when the first pod that it owns is created. This creates a race
	// with the pod creation that we hope to win so that the pod starts up with
	// a valid TLS configuration.
	//
	// TODO: Other pod owner types
	k8sAPI.Deploy().Informer().AddEventHandler(handlePodOwnerAddUpdate)

	k8sAPI.CM().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.handleConfigMapAdd,
			UpdateFunc: c.handleConfigMapUpdate,
			DeleteFunc: c.handleConfigMapDelete,
		},
	)

	c.syncHandler = c.syncObject

	return c, nil
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
	log.Infof("processNextWorkItem()")

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

func (c *CertificateController) syncObject(key string) error {
	log.Debugf("syncObject(%s)", key)

	if key == "" {
		return c.syncAll()
	}
	if !strings.Contains(key, ".") {
		return c.syncNamespace(key)
	}
	return c.syncSecret(key)
}

func (c *CertificateController) syncNamespace(ns string) error {
	log.Debugf("syncNamespace(%s)", ns)
	configMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: pkgK8s.TLSTrustAnchorConfigMapName},
		Data:       map[string]string{
			pkgK8s.TLSTrustAnchorFileName: c.trustAnchorsPEM,
		},
	}

	log.Debugf("adding configmap [%s] to namespace [%s]",
		pkgK8s.TLSTrustAnchorConfigMapName, ns)
	_, err := c.k8sAPI.Client.CoreV1().ConfigMaps(ns).Create(configMap)
	if apierrors.IsAlreadyExists(err) {
		_, err = c.k8sAPI.Client.CoreV1().ConfigMaps(ns).Update(configMap)
	}

	return err
}

func (c *CertificateController) syncSecret(key string) error {
	log.Debugf("syncSecret(%s)", key)
	parts := strings.Split(key, ".")
	if len(parts) != 3 {
		return nil // TODO
	}
	identity := pkgK8s.TLSIdentity{
		Name: parts[0],
		Kind: parts[1],
		Namespace: parts[2],
		ControllerNamespace: c.namespace,
	}
	dnsName := identity.ToDNSName()
	secretName := identity.ToSecretName()
	certAndPrivateKey, err := c.ca.IssueEndEntityCertificate(dnsName)
	if err != nil {
		log.Errorf("Failed to issue certificate for %s", dnsName)
		return err
	}
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName},
		Data:       map[string][]byte{
			pkgK8s.TLSCertFileName: certAndPrivateKey.Certificate,
			pkgK8s.TLSPrivateKeyFileName: certAndPrivateKey.PrivateKey,
		},
	}
	secrets := c.k8sAPI.Client.CoreV1().Secrets(identity.Namespace)
	_, err = secrets.Create(secret)
	if apierrors.IsAlreadyExists(err) {
		_, err = secrets.Update(secret)
	}

	return err
}

func (c *CertificateController) handlePodOwnerAdd(obj interface{}) {
	owner, err := meta.Accessor(obj)
	if err != nil {
		log.Warnf("handlePodOwnerAdd couldn't get object from tombstone: %+v", obj)
		return
	}

	var podLabels map[string]string

	switch typed := obj.(type) {
	case *appsv1beta2.Deployment:
		podLabels = typed.Spec.Template.Labels
	default:
		log.Warnf("handlePodOwnerAdd skipping %s in %s because of type mismatch", owner.GetName(), owner.GetNamespace())
		return
	}

	controller_ns := pkgK8s.GetControllerNsFromLabels(podLabels)
	if 	controller_ns != c.namespace {
		if controller_ns == "" {
			controller_ns = "<no controller>"
		}
		log.Debugf("handlePodOwnerAdd skipping %s in %s controlled by %s", owner.GetName(), owner.GetNamespace(), controller_ns)
		return
	}

	ns := owner.GetNamespace()
	log.Debugf("enqueuing update of CA bundle configmap in %s", ns)
	c.queue.Add(ns) // The namespace name won't have dots in it.

	kind, name := pkgK8s.GetOwnerKindAndName(podLabels)

	// Serialize (name, kind, ns) as a string since the queue's element
	// type must be a valid map key (so it can't not a struct).
	item := fmt.Sprintf("%s.%s.%s", name, kind, ns)
	log.Debugf("enqueuing update of secret for %s %s in %s", kind, name, ns)
	c.queue.Add(item)
}

func (c *CertificateController) handlePodOwnerUpdate(oldObj, newObj interface{}) {
	c.handlePodOwnerAdd(newObj)
}

func (c *CertificateController) handleConfigMapAdd(obj interface{}) {
}

func (c *CertificateController) handleConfigMapUpdate(oldObj, newObj interface{}) {
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

	if configMap.Name == pkgK8s.TLSTrustAnchorConfigMapName {
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

func (c *CertificateController) syncAll() error {
	log.Infof("syncAll() start")

	// TODO: error handling
	// TODO: other types of pod owners
	podOwners, err := c.k8sAPI.Deploy().Lister().List(labels.Everything())
	if err != nil {
		log.Errorf("error getting pod owners %s", err)
		return err
	}

	for _, podOwner := range(podOwners) {
		c.handlePodOwnerAdd(podOwner)
	}

	return nil
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
