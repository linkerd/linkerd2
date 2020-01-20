package linkerd2

import (
	"k8s.io/helm/pkg/chartutil"
)

type (

	// Tracing consists of the add-on configuration of the distributed tracing components sub-chart.
	Tracing struct {
		Enabled   bool       `json:"enabled"`
		Collector *Collector `json:"collector"`
		Jaeger    *Jaeger    `json:"jaeger"`
	}

	// Collector consists of the config values required for Trace collector
	Collector struct {
		Name      string     `json:"name"`
		Image     string     `json:"image"`
		Resources *Resources `json:"resources"`
	}

	// Jaeger consists of the config values required for Jaeger
	Jaeger struct {
		Name      string     `json:"name"`
		Image     string     `json:"image"`
		Resources *Resources `json:"resources"`
	}
)

var (
	tracingChartName   = "tracing"
	tracingConfigStage = []string{
		"templates/tracing-rbac.yaml",
	}

	tracingControlPlaneStage = []string{
		"templates/tracing.yaml",
	}
)

// GetChartName returns the name of the add-on sub-chart
func (*Tracing) GetChartName() string {
	return tracingChartName
}

// GetFiles returns the templates files that are part of the add-on sub-chart
func (*Tracing) GetFiles() []*chartutil.BufferedFile {
	files := []*chartutil.BufferedFile{
		{Name: chartutil.ChartfileName},
	}

	for _, template := range tracingConfigStage {
		files = append(files, &chartutil.BufferedFile{
			Name: template,
		})
	}

	for _, template := range tracingControlPlaneStage {
		files = append(files, &chartutil.BufferedFile{
			Name: template,
		})
	}

	return files
}

// GetValues returns the values struct which will be used to render the add-on sub-chart.
func (t *Tracing) GetValues() interface{} {
	return t
}
