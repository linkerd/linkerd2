package linkerd2

import (
	"helm.sh/helm/v3/pkg/chart/loader"
	"sigs.k8s.io/yaml"
)

var (
	// TracingAddOn represents the name of the tracing add-on
	TracingAddOn = "tracing"
)

// Tracing is an add-on that installs the distributed tracing
// related components like OpenCensus Collector and Jaeger
type Tracing map[string]interface{}

// Name returns the name of the Tracing add-on
func (t Tracing) Name() string {
	return TracingAddOn
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
func (t Tracing) ConfigStageTemplates() []*loader.BufferedFile {
	return []*loader.BufferedFile{
		{Name: "templates/tracing-rbac.yaml"},
	}
}

// ControlPlaneStageTemplates returns the template files that are part of the Control Plane Stage.
func (t Tracing) ControlPlaneStageTemplates() []*loader.BufferedFile {
	return []*loader.BufferedFile{
		{Name: "templates/tracing.yaml"},
	}
}
