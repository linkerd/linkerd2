package injector

import (
	"bytes"
	"context"
	"html/template"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/controller/webhook"
	vizLabels "github.com/linkerd/linkerd2/viz/pkg/labels"
	"github.com/prometheus/common/log"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
)

// Params holds the values used in the patch template.
type Params struct {
	ProxyIndex      int
	ProxyTapSvcName string
}

// Mutate mutates an AdmissionRequest and adds the LINKERD2_PROXY_TAP_SVC_NAME
// env var to a pod's proxy container if tap is not disabled via annotation on the
// pod or the namespace.
func Mutate(tapSvcName string) webhook.Handler {
	return func(
		ctx context.Context,
		k8sAPI *k8s.API,
		request *admissionv1beta1.AdmissionRequest,
		recorder record.EventRecorder,
	) (*admissionv1beta1.AdmissionResponse, error) {
		log.Debugf("request object bytes: %s", request.Object.Raw)
		admissionResponse := &admissionv1beta1.AdmissionResponse{
			UID:     request.UID,
			Allowed: true,
		}
		var pod *corev1.Pod
		if err := yaml.Unmarshal(request.Object.Raw, &pod); err != nil {
			return nil, err
		}
		params := Params{
			ProxyIndex:      webhook.GetProxyContainerIndex(pod.Spec.Containers),
			ProxyTapSvcName: tapSvcName,
		}
		if params.ProxyIndex < 0 || vizLabels.IsTapEnabled(pod) {
			return admissionResponse, nil
		}
		namespace, err := k8sAPI.NS().Lister().Get(request.Namespace)
		if err != nil {
			return nil, err
		}
		var t *template.Template
		if vizLabels.IsTapDisabled(namespace) || vizLabels.IsTapDisabled(pod) {
			return admissionResponse, nil
		}
		t, err = template.New("tpl").Parse(tpl)
		if err != nil {
			return nil, err
		}
		var patchJSON bytes.Buffer
		if err = t.Execute(&patchJSON, params); err != nil {
			return nil, err
		}
		patchType := admissionv1beta1.PatchTypeJSONPatch
		admissionResponse.Patch = patchJSON.Bytes()
		admissionResponse.PatchType = &patchType
		return admissionResponse, nil
	}
}
