package injector

import (
	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/inject"
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
	log.Debugf("request object bytes: %v", string(request.Object.Raw))
	conf, err := inject.NewResourceConfig(request.Object.Raw, request)
	if err != nil {
		return nil, err
	}

	// TODO: Fetch GlobalConfig and ProxyConfig from the ConfigMap/API
	globalConfig := &pb.GlobalConfig{
		LinkerdNamespace: "linkerd",
		CniEnabled:       false,
		IdentityContext:  nil,
	}
	proxyConfig := &pb.ProxyConfig{
		ProxyImage:              &pb.Image{ImageName: "gcr.io/linkerd-io/proxy", PullPolicy: "IfNotPresent", Registry: "gcr.io/linkerd-io"},
		ProxyInitImage:          &pb.Image{ImageName: "gcr.io/linkerd-io/proxy-init", PullPolicy: "IfNotPresent", Registry: "gcr.io/linkerd-io"},
		ApiPort:                 &pb.Port{Port: 8086},
		ControlPort:             &pb.Port{Port: 4190},
		IgnoreInboundPorts:      []*pb.Port{},
		IgnoreOutboundPorts:     []*pb.Port{},
		InboundPort:             &pb.Port{Port: 4143},
		MetricsPort:             &pb.Port{Port: 4191},
		OutboundPort:            &pb.Port{Port: 4140},
		Resource:                &pb.ResourceRequirements{RequestCpu: "100m", RequestMemory: "200Mi"},
		ProxyUid:                2102,
		LogLevel:                &pb.LogLevel{Level: "warn,linkerd2_proxy=info"},
		DisableExternalProfiles: false,
	}

	patchJSON, _, err := conf.Transform(globalConfig, proxyConfig)
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
