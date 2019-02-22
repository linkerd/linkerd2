package inject

import (
	"fmt"
	"strings"
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
	if conf.objectMeta != nil {
		name = conf.objectMeta.Name
	}
	return Report{
		Kind: strings.ToLower(conf.meta.Kind),
		Name: name,
	}
}

// ResName returns a string "Kind/Name" for the workload referred in the report i
func (i Report) ResName() string {
	return fmt.Sprintf("%s/%s", i.Kind, i.Name)
}
