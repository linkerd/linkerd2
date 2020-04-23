package linkerd2

import (
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

var (
	tracingAddOn = "tracing"
)

// Tracing is an add-on that installs the distributed tracing
// related components like OpenCensus Collector and Jaeger
type Tracing map[string]interface{}

// Name returns the name of the Tracing add-on
func (t Tracing) Name() string {
	return tracingAddOn
}

// Values returns the configuration values that were assigned for this add-on
func (t Tracing) Values() []byte {
	values, err := yaml.Marshal(t)
	if err != nil {
		return nil
	}
	return values
}

// ConfigStageTemplates returns the template files that are part of the config stage
func (t Tracing) ConfigStageTemplates() []*chartutil.BufferedFile {
	return []*chartutil.BufferedFile{
		{Name: "templates/tracing-rbac.yaml"},
	}
}

// ControlPlaneStageTemplates returns the template files that are part of the Control Plane Stage.
func (t Tracing) ControlPlaneStageTemplates() []*chartutil.BufferedFile {
	return []*chartutil.BufferedFile{
		{Name: "templates/tracing.yaml"},
	}
}
