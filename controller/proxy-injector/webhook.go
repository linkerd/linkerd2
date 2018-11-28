package injector

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"

	yaml "github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
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
)

// Webhook is a Kubernetes mutating admission webhook that mutates pods admission
// requests by injecting sidecar container spec into the pod spec during pod
// creation.
type Webhook struct {
	deserializer        runtime.Decoder
	controllerNamespace string
	resources           *WebhookResources
}

// NewWebhook returns a new instance of Webhook.
func NewWebhook(client kubernetes.Interface, resources *WebhookResources, controllerNamespace string) (*Webhook, error) {
	var (
		scheme = runtime.NewScheme()
		codecs = serializer.NewCodecFactory(scheme)
	)

	return &Webhook{
		deserializer:        codecs.UniversalDeserializer(),
		controllerNamespace: controllerNamespace,
		resources:           resources,
	}, nil
}

// Mutate changes the given pod spec by injecting the proxy sidecar container
// into the spec. The admission review object returns contains the original
// request and the response with the mutated pod spec.
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
		Namespace:           ns,
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

	if deployment.Spec.Template.Labels == nil {
		deployment.Spec.Template.Labels = map[string]string{}
	}

	deployment.Spec.Template.Labels[k8sPkg.ControllerNSLabel] = w.controllerNamespace
	deployment.Spec.Template.Labels[k8sPkg.ProxyDeploymentLabel] = deployment.ObjectMeta.Name
	patch.addPodLabels(deployment.Spec.Template.Labels)

	if deployment.Labels == nil {
		deployment.Labels = map[string]string{}
	}

	deployment.Labels[k8sPkg.ControllerNSLabel] = w.controllerNamespace
	deployment.Labels[k8sPkg.ProxyDeploymentLabel] = deployment.ObjectMeta.Name
	patch.addDeploymentLabels(deployment.Labels)

	var (
		image    = strings.Split(proxy.Image, ":")
		imageTag = ""
	)

	if len(image) < 2 {
		imageTag = "latest"
	} else {
		imageTag = image[1]
	}

	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations[k8sPkg.CreatedByAnnotation] = fmt.Sprintf("linkerd/proxy-injector %s", imageTag)
	deployment.Spec.Template.Annotations[k8sPkg.ProxyVersionAnnotation] = imageTag
	patch.addPodAnnotations(deployment.Spec.Template.Annotations)

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
		case k8sPkg.ProxyAutoInjectDisabled, k8sPkg.ProxyAutoInjectCompleted:
			return true
		}
	}

	return healthcheck.HasExistingSidecars(&deployment.Spec.Template.Spec)
}

func (w *Webhook) containersSpec(identity *k8sPkg.TLSIdentity) (*corev1.Container, *corev1.Container, error) {
	proxySpec, err := ioutil.ReadFile(w.resources.FileProxySpec)
	if err != nil {
		return nil, nil, err
	}

	var proxy corev1.Container
	if err := yaml.Unmarshal(proxySpec, &proxy); err != nil {
		return nil, nil, err
	}

	for index, env := range proxy.Env {
		if env.Name == envVarKeyProxyTLSPodIdentity {
			proxy.Env[index].Value = identity.ToDNSName()
		} else if env.Name == envVarKeyProxyTLSControllerIdentity {
			proxy.Env[index].Value = identity.ToControllerIdentity().ToDNSName()
		}
	}

	proxyInitSpec, err := ioutil.ReadFile(w.resources.FileProxyInitSpec)
	if err != nil {
		return nil, nil, err
	}

	var proxyInit corev1.Container
	if err := yaml.Unmarshal(proxyInitSpec, &proxyInit); err != nil {
		return nil, nil, err
	}

	return &proxy, &proxyInit, nil
}

func (w *Webhook) volumesSpec(identity *k8sPkg.TLSIdentity) (*corev1.Volume, *corev1.Volume, error) {
	trustAnchorVolumeSpec, err := ioutil.ReadFile(w.resources.FileTLSTrustAnchorVolumeSpec)
	if err != nil {
		return nil, nil, err
	}

	var trustAnchors corev1.Volume
	if err := yaml.Unmarshal(trustAnchorVolumeSpec, &trustAnchors); err != nil {
		return nil, nil, err
	}

	tlsVolumeSpec, err := ioutil.ReadFile(w.resources.FileTLSIdentityVolumeSpec)
	if err != nil {
		return nil, nil, err
	}

	var linkerdSecrets corev1.Volume
	if err := yaml.Unmarshal(tlsVolumeSpec, &linkerdSecrets); err != nil {
		return nil, nil, err
	}
	linkerdSecrets.VolumeSource.Secret.SecretName = identity.ToSecretName()

	return &trustAnchors, &linkerdSecrets, nil
}

// WebhookResources contain paths to all the needed file resources.
type WebhookResources struct {
	// FileProxySpec is the path to the proxy spec.
	FileProxySpec string

	// FileProxyInitSpec is the path to the proxy-init spec.
	FileProxyInitSpec string

	// FileTLSTrustAnchorVolumeSpec is the path to the trust anchor volume spec.
	FileTLSTrustAnchorVolumeSpec string

	// FileTLSIdentityVolumeSpec is the path to the TLS identity volume spec.
	FileTLSIdentityVolumeSpec string
}
