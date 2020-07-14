package linkerd2

import (
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

var (
	// PrometheusAddOn is the name of the prometheus add-on
	PrometheusAddOn = "prometheus"
)

// Prometheus is an add-on that installs the prometheus component
type Prometheus map[string]interface{}

// Name returns the name of the Prometheus add-on
func (p Prometheus) Name() string {
	return PrometheusAddOn
}

// Values returns the configuration values that were assigned for this add-on
func (p Prometheus) Values() []byte {
	values, err := yaml.Marshal(p)
	if err != nil {
		return nil
	}
	return values
}

// ConfigStageTemplates returns the template files that are part of the config stage
func (p Prometheus) ConfigStageTemplates() []*chartutil.BufferedFile {
	return []*chartutil.BufferedFile{
		{Name: "templates/prometheus-rbac.yaml"},
	}
}

// ControlPlaneStageTemplates returns the template files that are part of the Control Plane Stage.
func (p Prometheus) ControlPlaneStageTemplates() []*chartutil.BufferedFile {
	return []*chartutil.BufferedFile{
		{Name: "templates/prometheus.yaml"},
	}
}
