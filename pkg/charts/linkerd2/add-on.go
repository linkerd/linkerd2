package linkerd2

import "k8s.io/helm/pkg/chartutil"

var (
	AddonChartsPath = "linkerd2/add-ons/"
)

type AddOn interface {
	GetChartName() string
	IsEnabled() bool
	GetValues() interface{}
	GetFiles() []*chartutil.BufferedFile
}
