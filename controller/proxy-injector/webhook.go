package injector

import (
	"encoding/json"
	"fmt"
	"strings"

	yaml "github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/controller/k8s"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultNamespace                    = "default"
	envVarKeyProxyTLSPodIdentity        = "LINKERD2_PROXY_TLS_POD_IDENTITY"
	envVarKeyProxyTLSControllerIdentity = "LINKERD2_PROXY_TLS_CONTROLLER_IDENTITY"
	volumeSecretNameLinkerdSecrets      = "linkerd-secrets"
)

var errNilAdmissionReviewInput = fmt.Errorf("AdmissionReview input object can't be nil")

// Webhook is a Kubernetes mutating admission webhook that mutates pods admission requests by injecting sidecar container spec into the pod spec during pod creation.
type Webhook struct {
	deserializer        runtime.Decoder
	controllerNamespace string
	k8sAPI              *k8s.API
}

// NewWebhook returns a new instance of Webhook.
func NewWebhook(client kubernetes.Interface, controllerNamespace string) (*Webhook, error) {
	var (
		scheme = runtime.NewScheme()
		codecs = serializer.NewCodecFactory(scheme)
	)

	return &Webhook{
		deserializer:        codecs.UniversalDeserializer(),
		controllerNamespace: controllerNamespace,
		k8sAPI:              k8s.NewAPI(client, k8s.NS, k8s.CM),
	}, nil
}

// Mutate changes the given pod spec by injecting the proxy sidecar container into the spec. The admission review object returns contains the original request and the response with the mutated pod spec.
func (w *Webhook) Mutate(data []byte) *admissionv1beta1.AdmissionReview {
	admissionReview, err := w.decode(data)
	if err != nil {
		log.Error("failed to decode data. Reason: ", err)
		admissionReview.Response = &admissionv1beta1.AdmissionResponse{
			UID:     admissionReview.Request.UID,
			Allowed: false,
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
		return admissionReview
	}
	log.Infof("received admission review request %s", admissionReview.Request.UID)
	log.Debugf("admission request: %+v", admissionReview.Request)

	admissionResponse, err := w.inject(admissionReview.Request)
	if err != nil {
		log.Error("failed to inject sidecar. Reason: ", err)
		admissionReview.Response = &admissionv1beta1.AdmissionResponse{
			UID:     admissionReview.Request.UID,
			Allowed: false,
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
		return admissionReview
	}
	admissionReview.Response = admissionResponse

	if len(admissionResponse.Patch) > 0 {
		log.Infof("patch generated: %s", admissionResponse.Patch)
	}
	log.Info("done")

	return admissionReview
}

func (w *Webhook) decode(data []byte) (*admissionv1beta1.AdmissionReview, error) {
	var admissionReview admissionv1beta1.AdmissionReview
	err := yaml.Unmarshal(data, &admissionReview)
	return &admissionReview, err
}

func (w *Webhook) inject(request *admissionv1beta1.AdmissionRequest) (*admissionv1beta1.AdmissionResponse, error) {
	var deployment appsv1.Deployment
	if err := yaml.Unmarshal(request.Object.Raw, &deployment); err != nil {
		return nil, err
	}
	log.Infof("working on %s/%s %s..", request.Kind.Version, strings.ToLower(request.Kind.Kind), deployment.ObjectMeta.Name)

	ns := request.Namespace
	if ns == "" {
		ns = defaultNamespace
	}
	log.Infof("resource namespace: %s", ns)

	namespace, err := w.k8sAPI.NS().Lister().Get(ns)
	if err != nil {
		return nil, err
	}

	if w.ignore(&deployment) {
		log.Infof("ignoring deployment %s", deployment.ObjectMeta.Name)
		return &admissionv1beta1.AdmissionResponse{
			UID:     request.UID,
			Allowed: true,
		}, nil
	}

	identity := &k8sPkg.TLSIdentity{
		Name:                deployment.ObjectMeta.Name,
		Kind:                strings.ToLower(request.Kind.Kind),
		Namespace:           namespace.ObjectMeta.GetName(),
		ControllerNamespace: w.controllerNamespace,
	}
	proxy, proxyInit, err := w.containersSpec(identity)
	if err != nil {
		return nil, err
	}
	log.Infof("proxy image: %s", proxy.Image)
	log.Infof("proxy-init image: %s", proxyInit.Image)
	log.Debugf("proxy container: %+v", proxy)
	log.Debugf("init container: %+v", proxyInit)

	caBundle, tlsSecrets, err := w.volumesSpec(identity)
	if err != nil {
		return nil, err
	}
	log.Debugf("ca bundle volume: %+v", caBundle)
	log.Debugf("tls secrets volume: %+v", tlsSecrets)

	patch := NewPatch()
	patch.addContainer(proxy)

	if len(deployment.Spec.Template.Spec.InitContainers) == 0 {
		patch.addInitContainerRoot()
	}
	patch.addInitContainer(proxyInit)

	if len(deployment.Spec.Template.Spec.Volumes) == 0 {
		patch.addVolumeRoot()
	}
	patch.addVolume(caBundle)
	patch.addVolume(tlsSecrets)

	patch.addPodLabel(map[string]string{
		k8sPkg.ControllerNSLabel:    w.controllerNamespace,
		k8sPkg.ProxyDeploymentLabel: deployment.ObjectMeta.Name,
		k8sPkg.ProxyAutoInjectLabel: k8sPkg.ProxyAutoInjectCompleted,
	})

	var (
		image    = strings.Split(proxy.Image, ":")
		imageTag = ""
	)

	if len(image) < 2 {
		imageTag = "latest"
	} else {
		imageTag = image[1]
	}
	patch.addPodAnnotation(map[string]string{
		k8sPkg.CreatedByAnnotation:    fmt.Sprintf("linkerd/proxy-injector %s", imageTag),
		k8sPkg.ProxyVersionAnnotation: imageTag,
	})

	patchJSON, err := json.Marshal(patch.patchOps)
	if err != nil {
		return nil, err
	}

	patchType := admissionv1beta1.PatchTypeJSONPatch
	admissionResponse := &admissionv1beta1.AdmissionResponse{
		UID:       request.UID,
		Allowed:   true,
		Patch:     patchJSON,
		PatchType: &patchType,
	}

	return admissionResponse, nil
}

func (w *Webhook) ignore(deployment *appsv1.Deployment) bool {
	labels := deployment.Spec.Template.ObjectMeta.GetLabels()
	status, defined := labels[k8sPkg.ProxyAutoInjectLabel]
	if defined {
		switch status {
		case k8sPkg.ProxyAutoInjectDisabled:
			fallthrough
		case k8sPkg.ProxyAutoInjectCompleted:
			return true
		}
	}

	// check for known proxies and initContainers
	// same logic as the checkSidecars() function in cli/cmd/inject.go
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if strings.HasPrefix(container.Image, "gcr.io/linkerd-io/proxy:") ||
			strings.HasPrefix(container.Image, "gcr.io/istio-release/proxyv2:") ||
			strings.HasPrefix(container.Image, "gcr.io/heptio-images/contour:") ||
			strings.HasPrefix(container.Image, "docker.io/envoyproxy/envoy-alpine:") ||
			container.Name == "linkerd-proxy" ||
			container.Name == "istio-proxy" ||
			container.Name == "contour" ||
			container.Name == "envoy" {
			return true
		}
	}

	for _, ic := range deployment.Spec.Template.Spec.InitContainers {
		if strings.HasPrefix(ic.Image, "gcr.io/linkerd-io/proxy-init:") ||
			strings.HasPrefix(ic.Image, "gcr.io/istio-release/proxy_init:") ||
			strings.HasPrefix(ic.Image, "gcr.io/heptio-images/contour:") ||
			ic.Name == "linkerd-init" ||
			ic.Name == "istio-init" ||
			ic.Name == "envoy-initconfig" {
			return true
		}
	}

	return false
}

func (w *Webhook) containersSpec(identity *k8sPkg.TLSIdentity) (*corev1.Container, *corev1.Container, error) {
	configMap, err := w.k8sAPI.CM().Lister().ConfigMaps(identity.ControllerNamespace).Get(k8sPkg.ProxyInjectorSidecarConfig)
	if err != nil {
		return nil, nil, err
	}

	var proxy corev1.Container
	if err := yaml.Unmarshal([]byte(configMap.Data["proxy.yaml"]), &proxy); err != nil {
		return nil, nil, err
	}

	for index, env := range proxy.Env {
		if env.Name == envVarKeyProxyTLSPodIdentity {
			proxy.Env[index].Value = identity.ToDNSName()
		} else if env.Name == envVarKeyProxyTLSControllerIdentity {
			proxy.Env[index].Value = identity.ToControllerIdentity().ToDNSName()
		}
	}

	var proxyInit corev1.Container
	if err := yaml.Unmarshal([]byte(configMap.Data["proxy-init.yaml"]), &proxyInit); err != nil {
		return nil, nil, err
	}

	return &proxy, &proxyInit, nil
}

func (w *Webhook) volumesSpec(identity *k8sPkg.TLSIdentity) (*corev1.Volume, *corev1.Volume, error) {
	configMap, err := w.k8sAPI.CM().Lister().ConfigMaps(identity.ControllerNamespace).Get(k8sPkg.ProxyInjectorSidecarConfig)
	if err != nil {
		return nil, nil, err
	}

	var trustAnchors corev1.Volume
	if err := yaml.Unmarshal([]byte(configMap.Data["linkerd-trust-anchors"]), &trustAnchors); err != nil {
		return nil, nil, err
	}

	var linkerdSecrets corev1.Volume
	if err := yaml.Unmarshal([]byte(configMap.Data["linkerd-secrets"]), &linkerdSecrets); err != nil {
		return nil, nil, err
	}
	linkerdSecrets.VolumeSource.Secret.SecretName = identity.ToSecretName()

	return &trustAnchors, &linkerdSecrets, nil
}

// SyncAPI waits for the informers to sync.
func (w *Webhook) SyncAPI(ready chan struct{}) {
	w.k8sAPI.Sync(ready)
}
