package linkerd2

import (
	"github.com/linkerd/linkerd2/pkg/merge"
	"k8s.io/helm/pkg/chartutil"
)

type (
	Grafana struct {
		Enabled   merge.BoolInSetting `json:"enabled"`
		Image     string              `json:"image"`
		Resources *Resources          `json:"resources"`
	}
)

var (
	grafanaChartName   = "grafana"
	grafanaConfigStage = []string{
		"templates/grafana-rbac.yaml",
	}

	grafanaControlPlaneStage = []string{
		"templates/grafana.yaml",
	}
)

func (*Grafana) GetChartName() string {
	return grafanaChartName
}

func (*Grafana) GetFiles() []*chartutil.BufferedFile {
	files := []*chartutil.BufferedFile{
		{Name: chartutil.ChartfileName},
	}

	for _, template := range grafanaConfigStage {
		files = append(files, &chartutil.BufferedFile{
			Name: template,
		})
	}

	for _, template := range grafanaControlPlaneStage {
		files = append(files, &chartutil.BufferedFile{
			Name: template,
		})
	}

	return files
}

func (g *Grafana) GetValues() interface{} {
	return g
}

func (g *Grafana) IsEnabled() bool {
	return g.Enabled.Value
}
