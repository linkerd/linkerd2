package inject

import (
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	v1 "k8s.io/api/core/v1"
)

// Report contains the Kind and Name for a given workload along with booleans
// describing the result of the injection transformation
type Report struct {
	Kind                string
	Name                string
	HostNetwork         bool
	Sidecar             bool
	UDP                 bool // true if any port in any container has `protocol: UDP`
	UnsupportedResource bool
	InjectDisabled      bool

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
		report.InjectDisabled = report.disableByAnnotation(conf)
		report.HostNetwork = conf.pod.spec.HostNetwork
		report.Sidecar = healthcheck.HasExistingSidecars(conf.pod.spec)
		report.UDP = checkUDPPorts(conf.pod.spec)
	} else {
		report.UnsupportedResource = true
	}

	return report
}

// ResName returns a string "Kind/Name" for the workload referred in the report r
func (r *Report) ResName() string {
	return fmt.Sprintf("%s/%s", r.Kind, r.Name)
}

// Injectable returns false if the report flags indicate that the workload is on a host network
// or there is already a sidecar or the resource is not supported or inject is explicitly disabled
func (r *Report) Injectable() bool {
	return !r.HostNetwork && !r.Sidecar && !r.UnsupportedResource && !r.InjectDisabled
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

func (r *Report) disableByAnnotation(conf *ResourceConfig) bool {
	// truth table of the effects of the inject annotation:
	//
	// namespace | pod      | inject?  | return
	// --------- | -------- | -------- | ------
	// enabled   | enabled  | yes      | false
	// enabled   | ""       | yes      | false
	// enabled   | disabled | no       | true
	// disabled  | enabled  | yes      | false
	// ""        | enabled  | yes      | false
	// disabled  | disabled | no       | true
	// ""        | disabled | no       | true
	// disabled  | ""       | no       | true
	// ""        | ""       | no       | true
	//
	// for CLI, only the 'disabled' annotation is taken into consideration
	// for its opt-out effect

	podAnnotation := conf.pod.meta.Annotations[k8s.ProxyInjectAnnotation]
	nsAnnotation := conf.nsAnnotations[k8s.ProxyInjectAnnotation]

	if conf.origin == OriginCLI {
		return podAnnotation == k8s.ProxyInjectDisabled
	}

	if nsAnnotation == k8s.ProxyInjectEnabled {
		return podAnnotation == k8s.ProxyInjectDisabled
	}

	return podAnnotation != k8s.ProxyInjectEnabled
}
