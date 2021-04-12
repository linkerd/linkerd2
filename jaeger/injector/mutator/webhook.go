package mutator

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"strings"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/controller/webhook"
	"github.com/linkerd/linkerd2/jaeger/pkg/labels"
	l5dLables "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/common/log"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/yaml"
)

const (
	collectorSvcAddrAnnotation    = l5dLables.ProxyConfigAnnotationsPrefix + "/trace-collector"
	collectorSvcAccountAnnotation = l5dLables.ProxyConfigAnnotationsPrefixAlpha +
		"/trace-collector-service-account"
)

// Params holds the values used in the patch template
type Params struct {
	ProxyIndex          int
	CollectorSvcAddr    string
	CollectorSvcAccount string
}

// Mutate returns an AdmissionResponse containing the patch, if any, to apply
// to the proxy
func Mutate(collectorSvcAddr, collectorSvcAccount string) webhook.Handler {
	return func(
		ctx context.Context,
		api *k8s.API,
		request *admissionv1beta1.AdmissionRequest,
		recorder record.EventRecorder,
	) (*admissionv1beta1.AdmissionResponse, error) {
		log.Debugf("request object bytes: %s", request.Object.Raw)

		admissionResponse := &admissionv1beta1.AdmissionResponse{
			UID:     request.UID,
			Allowed: true,
		}

		if collectorSvcAddr == "" {
			return admissionResponse, nil
		}

		var pod *corev1.Pod
		if err := yaml.Unmarshal(request.Object.Raw, &pod); err != nil {
			return nil, err
		}
		params := Params{
			ProxyIndex:          webhook.GetProxyContainerIndex(pod.Spec.Containers),
			CollectorSvcAddr:    collectorSvcAddr,
			CollectorSvcAccount: collectorSvcAccount,
		}
		if params.ProxyIndex < 0 || labels.IsTracingEnabled(pod) {
			return admissionResponse, nil
		}

		namespace, err := api.NS().Lister().Get(request.Namespace)
		if err != nil {
			return nil, err
		}
		applyOverrides(namespace, pod, &params)
		amendSvcAccount(pod.Namespace, &params)

		t, err := template.New("tpl").Parse(tpl)
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

func applyOverrides(ns *corev1.Namespace, pod *corev1.Pod, params *Params) {
	ann := ns.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	for k, v := range pod.Annotations {
		ann[k] = v
	}
	if override, ok := ann[collectorSvcAddrAnnotation]; ok {
		params.CollectorSvcAddr = override
	}
	if override, ok := ann[collectorSvcAccountAnnotation]; ok {
		params.CollectorSvcAccount = override
	}
}

func amendSvcAccount(ns string, params *Params) {
	hostAndPort := strings.Split(params.CollectorSvcAddr, ":")
	hostname := strings.Split(hostAndPort[0], ".")
	if len(hostname) > 1 {
		ns = hostname[1]
	}
	params.CollectorSvcAccount = fmt.Sprintf("%s.%s", params.CollectorSvcAccount, ns)
}
