package injector

import (
	"fmt"

	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/inject"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/yaml"
)

// Webhook is a Kubernetes mutating admission webhook that mutates pods admission
// requests by injecting sidecar container spec into the pod spec during pod
// creation.
type Webhook struct {
	k8sAPI              *k8s.API
	deserializer        runtime.Decoder
	controllerNamespace string
	noInitContainer     bool
}

// NewWebhook returns a new instance of Webhook.
func NewWebhook(api *k8s.API, controllerNamespace string, noInitContainer bool) (*Webhook, error) {
	var (
		scheme = runtime.NewScheme()
		codecs = serializer.NewCodecFactory(scheme)
	)

	return &Webhook{
		k8sAPI:              api,
		deserializer:        codecs.UniversalDeserializer(),
		controllerNamespace: controllerNamespace,
		noInitContainer:     noInitContainer,
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

	globalConfig, err := config.Global(pkgK8s.MountPathGlobalConfig)
	if err != nil {
		return nil, err
	}

	proxyConfig, err := config.Proxy(pkgK8s.MountPathProxyConfig)
	if err != nil {
		return nil, err
	}

	namespaces, err := w.k8sAPI.GetObjects("", pkgK8s.Namespace, request.Namespace)
	if err != nil {
		return nil, err
	}
	if len(namespaces) == 0 {
		return nil, fmt.Errorf("namespace \"%s\" not found", request.Namespace)
	}
	nsAnnotations := namespaces[0].(*v1.Namespace).GetAnnotations()

	configs := &pb.All{Global: globalConfig, Proxy: proxyConfig}
	conf := inject.NewResourceConfig(configs).
		WithNsAnnotations(nsAnnotations).
		WithKind(request.Kind.Kind)
	nonEmpty, err := conf.ParseMeta(request.Object.Raw, request.Namespace)
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

	p, reports, err := conf.GetPatch(request.Object.Raw, inject.ShouldInjectWebhook)
	if err != nil {
		return nil, err
	}

	if p.IsEmpty() {
		return admissionResponse, nil
	}

	p.AddCreatedByPodAnnotation(fmt.Sprintf("linkerd/proxy-injector %s", version.Version))

	key, name, err := w.getLabelForParent(conf)
	if err != nil {
		return nil, err
	}
	if key != "" && name != "" {
		p.AddPodLabel(key, name)
	}

	patchJSON, err := p.Marshal()
	if err != nil {
		return nil, err
	}
	// TODO: refactor GetPatch() so it only returns one report item
	r := reports[0]
	log.Infof("patch generated for: %s", r.ResName())
	log.Debugf("patch: %s", patchJSON)

	patchType := admissionv1beta1.PatchTypeJSONPatch
	admissionResponse.Patch = patchJSON
	admissionResponse.PatchType = &patchType

	return admissionResponse, nil
}

func (w *Webhook) getLabelForParent(conf *inject.ResourceConfig) (string, string, error) {
	pod, err := conf.GetPod()
	if err != nil {
		return "", "", err
	}
	if kind, name := w.k8sAPI.GetOwnerKindAndName(pod); kind != pkgK8s.Pod {
		switch kind {
		case pkgK8s.Deployment:
			return pkgK8s.ProxyDeploymentLabel, name, nil
		case pkgK8s.ReplicationController:
			return pkgK8s.ProxyReplicationControllerLabel, name, nil
		case pkgK8s.ReplicaSet:
			return pkgK8s.ProxyReplicaSetLabel, name, nil
		case pkgK8s.Job:
			return pkgK8s.ProxyJobLabel, name, nil
		case pkgK8s.DaemonSet:
			return pkgK8s.ProxyDaemonSetLabel, name, nil
		case pkgK8s.StatefulSet:
			return pkgK8s.ProxyStatefulSetLabel, name, nil
		}
		return "", "", fmt.Errorf("unsupported parent kind \"%s\"", kind)
	}
	return "", "", nil
}
