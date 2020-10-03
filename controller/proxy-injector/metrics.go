package injector

import (
	"strings"

	"github.com/linkerd/linkerd2/pkg/inject"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	labelOwnerKind    = "owner_kind"
	labelNamespace    = "namespace"
	labelSkip         = "skip"
	labelAnnotationAt = "annotation_at"
	labelReason       = "skip_reason"
)

var (
	requestLabels  = []string{labelOwnerKind, labelNamespace, labelAnnotationAt}
	responseLabels = []string{labelOwnerKind, labelNamespace, labelSkip, labelReason, labelAnnotationAt}

	proxyInjectionAdmissionRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "proxy_inject_admission_requests_total",
		Help: "A counter for number of admission requests to proxy injector.",
	}, append(requestLabels, validLabelNames(inject.ProxyAnnotations)...))

	proxyInjectionAdmissionResponses = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "proxy_inject_admission_responses_total",
		Help: "A counter for number of admission responses from proxy injector.",
	}, append(responseLabels, validLabelNames(inject.ProxyAnnotations)...))
)

func admissionRequestLabels(ownerKind, namespace, annotationAt string, configLabels prometheus.Labels) prometheus.Labels {
	configLabels[labelOwnerKind] = ownerKind
	configLabels[labelNamespace] = namespace
	configLabels[labelAnnotationAt] = annotationAt
	return configLabels
}

func admissionResponseLabels(owner, namespace, skip, reason, annotationAt string, configLabels prometheus.Labels) prometheus.Labels {
	configLabels[labelOwnerKind] = owner
	configLabels[labelNamespace] = namespace
	configLabels[labelSkip] = skip
	configLabels[labelReason] = reason
	configLabels[labelAnnotationAt] = annotationAt
	return configLabels
}

func configToPrometheusLabels(conf *inject.ResourceConfig) prometheus.Labels {
	labels := conf.GetOverriddenConfiguration()
	promLabels := map[string]string{}

	for label, value := range labels {
		promLabels[validProxyConfigurationLabel(label)] = value

	}
	return promLabels
}

func validLabelNames(labels []string) []string {
	var validLabels []string

	for _, label := range labels {
		validLabels = append(validLabels, validProxyConfigurationLabel(label))
	}
	return validLabels
}

func validProxyConfigurationLabel(label string) string {
	return strings.Replace(label[len(k8s.ProxyConfigAnnotationsPrefix)+1:], "-", "_", -1)
}
