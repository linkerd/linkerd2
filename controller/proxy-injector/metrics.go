package injector

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	labelOwnerKind = "owner_kind"
	labelNamespace = "namespace"
	labelSkip      = "skip"
	labelReason    = "reason"
)

var (
	requestLabels  = []string{labelOwnerKind, labelNamespace}
	responseLabels = []string{labelOwnerKind, labelNamespace, labelSkip, labelReason}

	proxyInjectionAdmissionRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "proxy_inject_admission_requests_total",
		Help: "A counter for number of admission requests to proxy injector.",
	}, requestLabels)

	proxyInjectionAdmissionResponses = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "proxy_inject_admission_responses_total",
		Help: "A counter for number of admission responses from proxy injector.",
	}, responseLabels)
)

func admissionRequestLabels(ownerKind, namespace string) prometheus.Labels {
	return prometheus.Labels{
		labelOwnerKind: ownerKind,
		labelNamespace: namespace,
	}
}

func admissionResponseLabels(owner, namespace, skip, reason string) prometheus.Labels {
	return prometheus.Labels{
		labelOwnerKind: owner,
		labelNamespace: namespace,
		labelSkip:      skip,
		labelReason:    reason,
	}
}
