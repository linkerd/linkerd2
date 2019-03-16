package injector

import (
	"fmt"

	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/inject"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// Webhook is a Kubernetes mutating admission webhook that mutates pods admission
// requests by injecting sidecar container spec into the pod spec during pod
// creation.
type Webhook struct {
	client              kubernetes.Interface
	deserializer        runtime.Decoder
	controllerNamespace string
	noInitContainer     bool
	tlsEnabled          bool
}

// NewWebhook returns a new instance of Webhook.
func NewWebhook(client kubernetes.Interface, controllerNamespace string, noInitContainer, tlsEnabled bool) (*Webhook, error) {
	var (
		scheme = runtime.NewScheme()
		codecs = serializer.NewCodecFactory(scheme)
	)

	return &Webhook{
		client:              client,
		deserializer:        codecs.UniversalDeserializer(),
		controllerNamespace: controllerNamespace,
		noInitContainer:     noInitContainer,
		tlsEnabled:          tlsEnabled,
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

	return admissionReview
}

func (w *Webhook) decode(data []byte) (*admissionv1beta1.AdmissionReview, error) {
	var admissionReview admissionv1beta1.AdmissionReview
	err := yaml.Unmarshal(data, &admissionReview)
	return &admissionReview, err
}

func (w *Webhook) inject(request *admissionv1beta1.AdmissionRequest) (*admissionv1beta1.AdmissionResponse, error) {
	log.Debugf("request object bytes: %s", request.Object.Raw)

	globalConfig, err := config.Global(k8s.MountPathGlobalConfig)
	if err != nil {
		return nil, err
	}

	proxyConfig, err := config.Proxy(k8s.MountPathProxyConfig)
	if err != nil {
		return nil, err
	}

	namespace, err := w.client.CoreV1().Namespaces().Get(request.Namespace, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	nsAnnotations := namespace.GetAnnotations()

	conf := inject.NewResourceConfig(globalConfig, proxyConfig).
		WithNsAnnotations(nsAnnotations).
		WithKind(request.Kind.Kind)
	nonEmpty, err := conf.ParseMeta(request.Object.Raw)
	if err != nil {
		return nil, err
	}

	admissionResponse := &admissionv1beta1.AdmissionResponse{
		UID:     request.UID,
		Allowed: true,
	}
	if !nonEmpty {
		return admissionResponse, nil
	}

	p, _, err := conf.GetPatch(request.Object.Raw, inject.ShouldInjectWebhook)
	if err != nil {
		return nil, err
	}

	if p.IsEmpty() {
		return admissionResponse, nil
	}

	p.AddCreatedByPodAnnotation(fmt.Sprintf("%s %s", k8s.CreatedByProxyInjector, version.Version))

	// When adding workloads through `kubectl apply` the spec template labels are
	// automatically copied to the workload's main metadata section.
	// This doesn't happen when adding labels through the webhook. So we manually
	// add them to remain consistent.
	conf.AddRootLabels(p)

	patchJSON, err := p.Marshal()
	if err != nil {
		return nil, err
	}
	log.Infof("patch generated for: %s", conf)
	log.Debugf("patch: %s", patchJSON)

	patchType := admissionv1beta1.PatchTypeJSONPatch
	admissionResponse.Patch = patchJSON
	admissionResponse.PatchType = &patchType

	return admissionResponse, nil
}
