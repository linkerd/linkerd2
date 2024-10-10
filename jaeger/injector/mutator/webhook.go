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
	l5dLabels "github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/yaml"
)

const (
	collectorSvcAddrAnnotation       = l5dLabels.ProxyConfigAnnotationsPrefix + "/trace-collector"
	collectorTraceProtocolAnnotation = l5dLabels.ProxyConfigAnnotationsPrefix + "/trace-collector-protocol"
	collectorSvcAccountAnnotation    = l5dLabels.ProxyConfigAnnotationsPrefixAlpha +
		"/trace-collector-service-account"
)

// Params holds the values used in the patch template
type Params struct {
	ProxyPath              string
	CollectorSvcAddr       string
	CollectorTraceProtocol string
	CollectorSvcAccount    string
	ClusterDomain          string
	LinkerdNamespace       string
}

// Mutate returns an AdmissionResponse containing the patch, if any, to apply
// to the proxy
func Mutate(collectorSvcAddr, collectorTraceProtocol, collectorSvcAccount, clusterDomain, linkerdNamespace string) webhook.Handler {
	return func(
		_ context.Context,
		api *k8s.MetadataAPI,
		request *admissionv1beta1.AdmissionRequest,
		_ record.EventRecorder,
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
			ProxyPath:              webhook.GetProxyContainerPath(pod.Spec),
			CollectorSvcAddr:       collectorSvcAddr,
			CollectorTraceProtocol: collectorTraceProtocol,
			CollectorSvcAccount:    collectorSvcAccount,
			ClusterDomain:          clusterDomain,
			LinkerdNamespace:       linkerdNamespace,
		}
		if params.ProxyPath == "" || labels.IsTracingEnabled(pod) {
			return admissionResponse, nil
		}

		namespace, err := api.Get(k8s.NS, request.Namespace)
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

func applyOverrides(ns metav1.Object, pod *corev1.Pod, params *Params) {
	ann := map[string]string{}
	for k, v := range ns.GetAnnotations() {
		ann[k] = v
	}
	for k, v := range pod.Annotations {
		ann[k] = v
	}
	if override, ok := ann[collectorSvcAddrAnnotation]; ok {
		params.CollectorSvcAddr = override
	}
	if override, ok := ann[collectorTraceProtocolAnnotation]; ok {
		params.CollectorTraceProtocol = override
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
