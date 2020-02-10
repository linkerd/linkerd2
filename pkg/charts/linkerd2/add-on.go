package linkerd2

import "k8s.io/helm/pkg/chartutil"

var (
	// AddOnChartsPath is where the linkerd2 add-ons will be present
	AddOnChartsPath = "add-ons/"
)

// AddOn interface consists of the common functions required by add-ons to be implemented
type AddOn interface {
	GetChartName() string
	GetValues() interface{}
	GetConfigFiles() []*chartutil.BufferedFile
	GetControlPlaneFiles() []*chartutil.BufferedFile
}

// defaultGetFiles returns the templates files that are part of the add-on sub-chart
func defaultGetFiles(stageFiles []string) []*chartutil.BufferedFile {
	var files []*chartutil.BufferedFile

	for _, template := range stageFiles {
		files = append(files, &chartutil.BufferedFile{
			Name: template,
		})
	}

	return files
}
