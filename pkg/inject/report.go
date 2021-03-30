package inject

import (
	"errors"
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	v1 "k8s.io/api/core/v1"
)

const (
	hostNetworkEnabled                   = "host_network_enabled"
	sidecarExists                        = "sidecar_already_exists"
	unsupportedResource                  = "unsupported_resource"
	injectEnableAnnotationAbsent         = "injection_enable_annotation_absent"
	injectDisableAnnotationPresent       = "injection_disable_annotation_present"
	annotationAtNamespace                = "namespace"
	annotationAtWorkload                 = "workload"
	invalidInjectAnnotationWorkload      = "invalid_inject_annotation_at_workload"
	invalidInjectAnnotationNamespace     = "invalid_inject_annotation_at_ns"
	disabledAutomountServiceAccountToken = "disabled_automount_service_account_token_account"
	udpPortsEnabled                      = "udp_ports_enabled"
)

var (
	// Reasons is a map of inject skip reasons with human readable sentences
	Reasons = map[string]string{
		hostNetworkEnabled:                   "hostNetwork is enabled",
		sidecarExists:                        "pod has a sidecar injected already",
		unsupportedResource:                  "this resource kind is unsupported",
		injectEnableAnnotationAbsent:         fmt.Sprintf("neither the namespace nor the pod have the annotation \"%s:%s\"", k8s.ProxyInjectAnnotation, k8s.ProxyInjectEnabled),
		injectDisableAnnotationPresent:       fmt.Sprintf("pod has the annotation \"%s:%s\"", k8s.ProxyInjectAnnotation, k8s.ProxyInjectDisabled),
		invalidInjectAnnotationWorkload:      fmt.Sprintf("invalid value for annotation \"%s\" at workload", k8s.ProxyInjectAnnotation),
		invalidInjectAnnotationNamespace:     fmt.Sprintf("invalid value for annotation \"%s\" at namespace", k8s.ProxyInjectAnnotation),
		disabledAutomountServiceAccountToken: "automountServiceAccountToken set to \"false\"",
		udpPortsEnabled:                      "UDP port(s) configured on pod spec",
	}
)

// Report contains the Kind and Name for a given workload along with booleans
// describing the result of the injection transformation
type Report struct {
	Kind                         string
	Name                         string
	HostNetwork                  bool
	Sidecar                      bool
	UDP                          bool // true if any port in any container has `protocol: UDP`
	UnsupportedResource          bool
	InjectDisabled               bool
	InjectDisabledReason         string
	InjectAnnotationAt           string
	Annotatable                  bool
	Annotated                    bool
	AutomountServiceAccountToken bool

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
	if conf.IsPod() {
		name = conf.pod.meta.Name
		if name == "" {
			name = conf.pod.meta.GenerateName
		}
	} else if m := conf.workload.Meta; m != nil {
		name = m.Name
	}

	report := &Report{
		Kind:                         strings.ToLower(conf.workload.metaType.Kind),
		Name:                         name,
		AutomountServiceAccountToken: true,
	}

	if conf.HasPodTemplate() {
		report.InjectDisabled, report.InjectDisabledReason, report.InjectAnnotationAt = report.disabledByAnnotation(conf)
		report.HostNetwork = conf.pod.spec.HostNetwork
		report.Sidecar = healthcheck.HasExistingSidecars(conf.pod.spec)
		report.UDP = checkUDPPorts(conf.pod.spec)
		if conf.pod.spec.AutomountServiceAccountToken != nil {
			report.AutomountServiceAccountToken = *conf.pod.spec.AutomountServiceAccountToken
		}
		if conf.origin == OriginWebhook {
			if vm := conf.serviceAccountVolumeMount(); vm == nil {
				report.AutomountServiceAccountToken = false
			}
		}
	} else {
		report.UnsupportedResource = true
	}

	if conf.HasPodTemplate() || conf.IsService() || conf.IsNamespace() {
		report.Annotatable = true
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

	if !r.AutomountServiceAccountToken {
		reasons = append(reasons, disabledAutomountServiceAccountToken)
	}

	if len(reasons) > 0 {
		return false, reasons
	}
	return true, nil
}

// IsAnnotatable returns true if the resource for a report can be annotated.
func (r *Report) IsAnnotatable() bool {
	return r.Annotatable
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

// disabledByAnnotation checks the workload and namespace for the annotation
// that disables injection. It returns if it is disabled, why it is disabled,
// and the location where the annotation was present.
func (r *Report) disabledByAnnotation(conf *ResourceConfig) (bool, string, string) {
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

	if !isInjectAnnotationValid(nsAnnotation) {
		return true, invalidInjectAnnotationNamespace, ""
	}

	if !isInjectAnnotationValid(podAnnotation) {
		return true, invalidInjectAnnotationWorkload, ""
	}

	if nsAnnotation == k8s.ProxyInjectEnabled || nsAnnotation == k8s.ProxyInjectIngress {
		if podAnnotation == k8s.ProxyInjectDisabled {
			return true, injectDisableAnnotationPresent, annotationAtWorkload
		}
		return false, "", annotationAtNamespace
	}

	if podAnnotation != k8s.ProxyInjectEnabled && podAnnotation != k8s.ProxyInjectIngress {
		return true, injectEnableAnnotationAbsent, ""
	}

	return false, "", annotationAtWorkload
}

func isInjectAnnotationValid(annotation string) bool {
	if annotation != "" && !(annotation == k8s.ProxyInjectEnabled || annotation == k8s.ProxyInjectDisabled || annotation == k8s.ProxyInjectIngress) {
		return false
	}
	return true
}

// ThrowInjectError errors out `inject` when the report contains errors
// related to automountServiceAccountToken, hostNetwork, existing sidecar,
// or udp ports
// See - https://github.com/linkerd/linkerd2/issues/4214
func (r *Report) ThrowInjectError() []error {

	errs := []error{}

	if !r.AutomountServiceAccountToken {
		errs = append(errs, errors.New(Reasons[disabledAutomountServiceAccountToken]))
	}

	if r.HostNetwork {
		errs = append(errs, errors.New(Reasons[hostNetworkEnabled]))
	}

	if r.Sidecar {
		errs = append(errs, errors.New(Reasons[sidecarExists]))
	}

	if r.UDP {
		errs = append(errs, errors.New(Reasons[udpPortsEnabled]))
	}

	return errs
}
