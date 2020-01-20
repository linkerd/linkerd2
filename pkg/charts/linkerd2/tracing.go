package linkerd2

import (
	"k8s.io/helm/pkg/chartutil"
)

type (
	Tracing struct {
		Enabled   bool       `json:"enabled"`
		Collector *Collector `json:"collector"`
		Jaeger    *Jaeger    `json:"jaeger"`
	}

	Collector struct {
		Name      string     `json:"name"`
		Image     string     `json:"image"`
		Resources *Resources `json:"resources"`
	}

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

func (*Tracing) GetChartName() string {
	return tracingChartName
}

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

func (t *Tracing) GetValues() interface{} {
	return t
}

func (t *Tracing) IsEnabled() bool {
	return t.Enabled
}
