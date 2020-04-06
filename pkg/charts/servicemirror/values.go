package servicemirror

import (
	"fmt"

	"github.com/linkerd/linkerd2/pkg/charts"
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

const (
	helmDefaultServiceMirrorChartDir = "linkerd2-service-mirror"
)

// Values contains the top-level elements in the Helm charts
type Values struct {
	Namespace                string `json:"namespace"`
	ControllerImage          string `json:"controllerImage"`
	ControllerImageVersion   string `json:"controllerImageVersion"`
	ControllerComponentLabel string `json:"controllerComponentLabel"`
	ServiceMirrorUID         int64  `json:"serviceMirrorUID"`
	LogLevel                 string `json:"logLevel"`
	EventRequeueLimit        int32  `json:"eventRequeueLimit"`
}

// NewValues returns a new instance of the Values type.
func NewValues() (*Values, error) {
	chartDir := fmt.Sprintf("%s/", helmDefaultServiceMirrorChartDir)
	v, err := readDefaults(chartDir)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// readDefaults read all the default variables from the values.yaml file.
// chartDir is the root directory of the Helm chart where values.yaml is.
func readDefaults(chartDir string) (*Values, error) {
	file := &chartutil.BufferedFile{
		Name: chartutil.ValuesfileName,
	}
	if err := charts.ReadFile(chartDir, file); err != nil {
		return nil, err
	}
	values := Values{}
	if err := yaml.Unmarshal(charts.InsertVersion(file.Data), &values); err != nil {
		return nil, err
	}
	return &values, nil
}
