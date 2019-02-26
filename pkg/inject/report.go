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
	Udp                 bool // true if any port in any container has `protocol: UDP`
	UnsupportedResource bool
	InjectDisabled      bool
}

// NewReport returns a new Report struct, initialized with the Kind and Name
// from conf
func NewReport(conf *ResourceConfig) Report {
	var name string
	if conf.objectMeta.ObjectMeta != nil {
		name = conf.objectMeta.Name
	}
	return Report{
		Kind: strings.ToLower(conf.meta.Kind),
		Name: name,
	}
}

// ResName returns a string "Kind/Name" for the workload referred in the report r
func (r Report) ResName() string {
	return fmt.Sprintf("%s/%s", r.Kind, r.Name)
}

// update updates the report for the provided resource conf.
func (r *Report) update(conf *ResourceConfig) {
	r.InjectDisabled = conf.objectMeta.ObjectMeta.GetAnnotations()[k8s.ProxyInjectAnnotation] == k8s.ProxyInjectDisabled
	r.HostNetwork = conf.podSpec.HostNetwork
	r.Sidecar = healthcheck.HasExistingSidecars(conf.podSpec)
	r.Udp = checkUDPPorts(conf.podSpec)
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
