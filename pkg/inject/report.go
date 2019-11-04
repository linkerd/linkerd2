package inject

import (
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	v1 "k8s.io/api/core/v1"
)

const (
	hostNetworkEnabled             = "host_network_enabled"
	sidecarExists                  = "sidecar_already_exists"
	unsupportedResource            = "unsupported_resource"
	injectEnableAnnotationAbsent   = "injection_enable_annotation_absent"
	injectDisableAnnotationPresent = "injection_disable_annotation_present"
	annotationAtNamespace          = "namespace"
	annotationAtWorkload           = "workload"
)

var (
	// Reasons is a map of inject skip reasons with human readable sentences
	Reasons = map[string]string{
		hostNetworkEnabled:             "hostNetwork is enabled",
		sidecarExists:                  "pod has a sidecar injected already",
		unsupportedResource:            "this resource kind is unsupported",
		injectEnableAnnotationAbsent:   fmt.Sprintf("neither the namespace nor the pod have the annotation \"%s:%s\"", k8s.ProxyInjectAnnotation, k8s.ProxyInjectEnabled),
		injectDisableAnnotationPresent: fmt.Sprintf("pod has the annotation \"%s:%s\"", k8s.ProxyInjectAnnotation, k8s.ProxyInjectDisabled),
	}
)

// Report contains the Kind and Name for a given workload along with booleans
// describing the result of the injection transformation
type Report struct {
	Kind                 string
	Name                 string
	HostNetwork          bool
	Sidecar              bool
	UDP                  bool // true if any port in any container has `protocol: UDP`
	UnsupportedResource  bool
	InjectDisabled       bool
	InjectDisabledReason string
	InjectAnnotationAt   string
	TracingEnabled       bool

	// Uninjected consists of two boolean flags to indicate if a proxy and
	// proxy-init containers have been uninjected in this report
	Uninjected struct {
		// Proxy is true if a proxy container has been uninjected
		Proxy bool

		// ProxyInit is true if a proxy-init container has been uninjected
		ProxyInit bool
	}
}

// newReport returns a new Report struct, initialized with the Kind and Name
// from conf
func newReport(conf *ResourceConfig) *Report {
	var name string
	if m := conf.workload.Meta; m != nil {
		name = m.Name
	} else if m := conf.pod.meta; m != nil {
		name = m.Name
		if name == "" {
			name = m.GenerateName
		}
	}

	report := &Report{
		Kind: strings.ToLower(conf.workload.metaType.Kind),
		Name: name,
	}

	if conf.pod.meta != nil && conf.pod.spec != nil {
		report.InjectDisabled, report.InjectDisabledReason, report.InjectAnnotationAt = report.disableByAnnotation(conf)
		report.HostNetwork = conf.pod.spec.HostNetwork
		report.Sidecar = healthcheck.HasExistingSidecars(conf.pod.spec)
		report.UDP = checkUDPPorts(conf.pod.spec)
		report.TracingEnabled = conf.pod.meta.Annotations[k8s.ProxyTraceCollectorSvcAddrAnnotation] != "" || conf.nsAnnotations[k8s.ProxyTraceCollectorSvcAddrAnnotation] != ""
	} else if report.Kind != k8s.Namespace {
		report.UnsupportedResource = true
	}

	return report
}

// ResName returns a string "Kind/Name" for the workload referred in the report r
func (r *Report) ResName() string {
	return fmt.Sprintf("%s/%s", r.Kind, r.Name)
}

// Injectable returns false if the report flags indicate that the workload is on a host network
// or there is already a sidecar or the resource is not supported or inject is explicitly disabled.
// If false, the second returned value describes the reason.
func (r *Report) Injectable() (bool, []string) {
	var reasons []string
	if r.HostNetwork {
		reasons = append(reasons, hostNetworkEnabled)
	}
	if r.Sidecar {
		reasons = append(reasons, sidecarExists)
	}
	if r.UnsupportedResource {
		reasons = append(reasons, unsupportedResource)
	}
	if r.InjectDisabled {
		reasons = append(reasons, r.InjectDisabledReason)
	}

	if len(reasons) > 0 {
		return false, reasons
	}
	return true, nil
}

func checkUDPPorts(t *v1.PodSpec) bool {
	// Check for ports with `protocol: UDP`, which will not be routed by Linkerd
	for _, container := range t.Containers {
		for _, port := range container.Ports {
			if port.Protocol == v1.ProtocolUDP {
				return true
			}
		}
	}
	return false
}

// disabledByAnnotation checks annotations for both workload, namespace and returns
// if disabled, Inject Disabled reason and the resource where that annotation was present
func (r *Report) disableByAnnotation(conf *ResourceConfig) (bool, string, string) {
	// truth table of the effects of the inject annotation:
	//
	// origin  | namespace | pod      | inject?  | return
	// ------- | --------- | -------- | -------- | ------
	// webhook | enabled   | enabled  | yes      | false
	// webhook | enabled   | ""       | yes      | false
	// webhook | enabled   | disabled | no       | true
	// webhook | disabled  | enabled  | yes      | false
	// webhook | ""        | enabled  | yes      | false
	// webhook | disabled  | disabled | no       | true
	// webhook | ""        | disabled | no       | true
	// webhook | disabled  | ""       | no       | true
	// webhook | ""        | ""       | no       | true
	// cli     | n/a       | enabled  | yes      | false
	// cli     | n/a       | ""       | yes      | false
	// cli     | n/a       | disabled | no       | true

	podAnnotation := conf.pod.meta.Annotations[k8s.ProxyInjectAnnotation]
	nsAnnotation := conf.nsAnnotations[k8s.ProxyInjectAnnotation]

	if conf.origin == OriginCLI {
		return podAnnotation == k8s.ProxyInjectDisabled, "", ""
	}

	if nsAnnotation == k8s.ProxyInjectEnabled {
		if podAnnotation == k8s.ProxyInjectDisabled {
			return true, injectDisableAnnotationPresent, annotationAtWorkload
		}
		return false, "", annotationAtNamespace
	}

	if podAnnotation != k8s.ProxyInjectEnabled {
		return true, injectEnableAnnotationAbsent, ""
	}

	return false, "", annotationAtWorkload
}
