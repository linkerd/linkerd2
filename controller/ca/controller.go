package ca

import (
	"fmt"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/controller/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// CertificateController listens for added and updated meshed pods, and then
// provides certificates in the form of secrets.
type CertificateController struct {
	namespace   string
	k8sAPI      *k8s.API
	ca          *tls.CA
	syncHandler func(key string) error

	// The queue is keyed on a string. If the string doesn't contain any dots
	// then it is a namespace name and the task is to create the CA bundle
	// configmap in that namespace. Otherwise the string must be of the form
	// "$podOwner.$podKind.$podNamespace" and the task is to create the secret
	// for that pod owner.
	queue workqueue.RateLimitingInterface
}

// NewCertificateController initializes a CertificateController and its
// internal Certificate Authority.
func NewCertificateController(controllerNamespace string, k8sAPI *k8s.API) (*CertificateController, error) {
	ca, err := tls.GenerateRootCAWithDefaults("Cluster-local Managed Pod CA")
	if err != nil {
		return nil, err
	}

	c := &CertificateController{
		namespace: controllerNamespace,
		k8sAPI:    k8sAPI,
		ca:        ca,
		queue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(), "certificates"),
	}

	k8sAPI.Pod().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.handlePodAdd,
			UpdateFunc: c.handlePodUpdate,
		},
	)

	c.syncHandler = c.syncObject

	return c, nil
}

// Run kicks off CertificateController queue processing.
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
		log.Errorf("error syncing object: %s", err)
		c.queue.AddRateLimited(key)
		return true
	}

	c.queue.Forget(key)
	return true
}

func (c *CertificateController) syncObject(key string) error {
	log.Debugf("syncObject(%s)", key)
	if !strings.Contains(key, ".") {
		return c.syncNamespace(key)
	}
	return c.syncSecret(key)
}

func (c *CertificateController) syncNamespace(ns string) error {
	log.Debugf("syncNamespace(%s)", ns)
	configMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: pkgK8s.TLSTrustAnchorConfigMapName},
		Data: map[string]string{
			pkgK8s.TLSTrustAnchorFileName: c.ca.Cred.EncodeCertificatePEM(),
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
		log.Errorf("Failed to parse secret sync request %s", key)
		return nil // TODO
	}
	identity := pkgK8s.TLSIdentity{
		Name:                parts[0],
		Kind:                parts[1],
		Namespace:           parts[2],
		ControllerNamespace: c.namespace,
	}

	dnsName := identity.ToDNSName()
	secretName := identity.ToSecretName()
	cred, err := c.ca.GenerateEndEntityCred(dnsName)
	if err != nil {
		log.Errorf("Failed to issue certificate for %s", dnsName)
		return err
	}
	pk, err := cred.EncodePrivateKeyP8()
	if err != nil {
		log.Errorf("Failed to issue certificate for %s", dnsName)
		return err
	}
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName},
		Data: map[string][]byte{
			pkgK8s.TLSCertFileName:       cred.Crt.Certificate.Raw,
			pkgK8s.TLSPrivateKeyFileName: pk,
		},
	}
	_, err = c.k8sAPI.Client.CoreV1().Secrets(identity.Namespace).Create(secret)
	if apierrors.IsAlreadyExists(err) {
		_, err = c.k8sAPI.Client.CoreV1().Secrets(identity.Namespace).Update(secret)
	}

	return err
}

func (c *CertificateController) handlePodAdd(obj interface{}) {
	pod := obj.(*v1.Pod)
	if pkgK8s.IsMeshed(pod, c.namespace) {
		log.Debugf("enqueuing update of CA bundle configmap in %s", pod.Namespace)
		c.queue.Add(pod.Namespace)

		ownerKind, ownerName := c.k8sAPI.GetOwnerKindAndName(pod)
		item := fmt.Sprintf("%s.%s.%s", ownerName, ownerKind, pod.Namespace)
		log.Debugf("enqueuing secret write for %s", item)
		c.queue.Add(item)
	}
}

func (c *CertificateController) handlePodUpdate(oldObj, newObj interface{}) {
	c.handlePodAdd(newObj)
}
