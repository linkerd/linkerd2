package linkerd2

import (
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

var (
	// publicApiAddOn is the name of the public-api add-on
	publicAPIAddOn = "publicApi"
)

type (

	// PublicAPI is an add-on that installs the public-api component
	PublicAPI struct {
		Enabled   bool            `json:"enabled"`
		Replicas  uint            `json:"replicas,omitempty"`
		Image     string          `json:"image,omitempty"`
		UID       int64           `json:"UID,omitempty"`
		Resources *Resources      `json:"resources,omitempty"`
		Proxy     *publicAPIProxy `json:"proxy,omitempty"`
	}

	// Proxy is a sub-type consisting of the proxy component of public-api
	publicAPIProxy struct {
		Resources *Resources `json:"resources,omitempty"`
	}
)

// Name returns the name of the Prometheus add-on
func (p PublicAPI) Name() string {
	return publicAPIAddOn
}

// Values returns the configuration values that were assigned for this add-on
func (p PublicAPI) Values() []byte {
	values, err := yaml.Marshal(p)
	if err != nil {
		return nil
	}
	return values
}

// ConfigStageTemplates returns the template files that are part of the config stage
func (p PublicAPI) ConfigStageTemplates() []*chartutil.BufferedFile {
	return []*chartutil.BufferedFile{
		{Name: "templates/controller-rbac.yaml"},
	}
}

// ControlPlaneStageTemplates returns the template files that are part of the Control Plane Stage.
func (p PublicAPI) ControlPlaneStageTemplates() []*chartutil.BufferedFile {
	return []*chartutil.BufferedFile{
		{Name: "templates/controller.yaml"},
	}
}
