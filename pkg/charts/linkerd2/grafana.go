package linkerd2

import (
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

var (
	grafanaAddOn = "grafana"
)

// Grafana is an add-on that consists of the grafana components
type Grafana map[string]interface{}

// Name returns the name of the Grafana add-on
func (g Grafana) Name() string {
	return grafanaAddOn
}

// Values returns the configuration values that were assigned for this add-on
func (g Grafana) Values() []byte {
	values, err := yaml.Marshal(g)
	if err != nil {
		return nil
	}
	return values
}

// Templates returns the template files specific to this add-on
func (g Grafana) Templates() []*chartutil.BufferedFile {
	return []*chartutil.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: "templates/grafana-rbac.yaml"},
		{Name: "templates/grafana.yaml"},
	}
}
