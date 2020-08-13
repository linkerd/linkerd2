package linkerd2

import (
	loader "helm.sh/helm/v3/pkg/chart/loader"
	"sigs.k8s.io/yaml"
)

const (
	// GrafanaAddOn is the name of the grafana add-on
	GrafanaAddOn = "grafana"
)

// Grafana is an add-on that consists of the grafana components
type Grafana map[string]interface{}

// Name returns the name of the Grafana add-on
func (g Grafana) Name() string {
	return GrafanaAddOn
}

// Values returns the configuration values that were assigned for this add-on
func (g Grafana) Values() []byte {
	values, err := yaml.Marshal(g)
	if err != nil {
		return nil
	}
	return values
}

// ConfigStageTemplates returns the template files that are part of the config stage
func (g Grafana) ConfigStageTemplates() []*loader.BufferedFile {
	return []*loader.BufferedFile{
		{Name: "templates/grafana-rbac.yaml"},
	}
}

// ControlPlaneStageTemplates returns the template files that are part of the Control Plane Stage.
func (g Grafana) ControlPlaneStageTemplates() []*loader.BufferedFile {
	return []*loader.BufferedFile{
		{Name: "templates/grafana.yaml"},
	}
}
