package linkerd2

import "k8s.io/helm/pkg/chartutil"

var (
	AddonBufferedFiles = []*chartutil.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: "templates/addon-cm.yaml"},
	}
	AddonChartsPath = "linkerd2/add-ons/"
)

type AddOn interface {
	GetChartName() string
	IsEnabled() bool
	GetValues() interface{}
	GetFiles() []*chartutil.BufferedFile
}
