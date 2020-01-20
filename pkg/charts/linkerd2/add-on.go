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
