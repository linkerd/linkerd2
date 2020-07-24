package linkerd2

import (
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

const (
	// dashboardAddOn is the name of the grafana add-on
	dashboardAddOn = "dashboard"
)

// Dashboard is an add-on that consists of the linkerd-web components
type Dashboard map[string]interface{}

// Name returns the name of the Grafana add-on
func (d Dashboard) Name() string {
	return dashboardAddOn
}

// Values returns the configuration values that were assigned for this add-on
func (d Dashboard) Values() []byte {
	values, err := yaml.Marshal(d)
	if err != nil {
		return nil
	}
	return values
}

// ConfigStageTemplates returns the template files that are part of the config stage
func (d Dashboard) ConfigStageTemplates() []*chartutil.BufferedFile {
	return []*chartutil.BufferedFile{
		{Name: "templates/web-rbac.yaml"},
	}
}

// ControlPlaneStageTemplates returns the template files that are part of the Control Plane Stage.
func (d Dashboard) ControlPlaneStageTemplates() []*chartutil.BufferedFile {
	return []*chartutil.BufferedFile{
		{Name: "templates/web.yaml"},
	}
}
