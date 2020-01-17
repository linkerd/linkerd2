package linkerd2

import (
	"k8s.io/helm/pkg/chartutil"
)

type (
	Prometheus struct {
		Enabled   bool       `json:"enabled"`
		LogLevel  string     `json:"logLevel"`
		Image     string     `json:"image"`
		Resources *Resources `json:"resources"`
	}
)

var (
	prometheusChartName   = "prometheus"
	prometheusConfigStage = []string{
		"templates/prometheus-rbac.yaml",
	}

	prometheusControlPlaneStage = []string{
		"templates/prometheus.yaml",
	}
)

func (*Prometheus) GetChartName() string {
	return prometheusChartName
}

func (*Prometheus) GetFiles() []*chartutil.BufferedFile {
	files := []*chartutil.BufferedFile{
		{Name: chartutil.ChartfileName},
	}

	for _, template := range prometheusConfigStage {
		files = append(files, &chartutil.BufferedFile{
			Name: template,
		})
	}

	for _, template := range prometheusControlPlaneStage {
		files = append(files, &chartutil.BufferedFile{
			Name: template,
		})
	}

	return files
}

func (g *Prometheus) GetValues() interface{} {
	return g
}

func (g *Prometheus) IsEnabled() bool {
	return g.Enabled
}
