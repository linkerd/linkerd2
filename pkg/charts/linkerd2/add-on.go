package linkerd2

import "k8s.io/helm/pkg/chartutil"

var (
	// AddOnChartsPath is where the linkerd2 add-ons will be present
	AddOnChartsPath = "linkerd2/add-ons/"
)

// AddOn interface consists of the common functions required by add-ons to be implemented
type AddOn interface {
	GetChartName() string
	GetValues() interface{}
	GetFiles() []*chartutil.BufferedFile
}

// defaultGetFiles returns the templates files that are part of the add-on sub-chart
func defaultGetFiles(configStage, controlPlaneStage []string) []*chartutil.BufferedFile {
	files := []*chartutil.BufferedFile{
		{Name: chartutil.ChartfileName},
	}

	for _, template := range configStage {
		files = append(files, &chartutil.BufferedFile{
			Name: template,
		})
	}

	for _, template := range controlPlaneStage {
		files = append(files, &chartutil.BufferedFile{
			Name: template,
		})
	}

	return files
}
