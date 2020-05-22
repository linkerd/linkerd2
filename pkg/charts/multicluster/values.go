package multicluster

import (
	"fmt"

	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

const helmDefaultChartDir = "linkerd2-multicluster"

// Values contains the top-level elements in the Helm charts
type Values struct {
	ControllerImage          string `json:"controllerImage"`
	ControllerImageVersion   string `json:"controllerImageVersion"`
	ControllerComponentLabel string `json:"controllerComponentLabel"`
	Namespace                string `json:"namespace"`
	LinkerdNamespace         string `json:"linkerdNamespace"`
	ProxyOutboundPort        uint32 `json:"proxyOutboundPort"`
	IdentityTrustDomain      string `json:"identityTrustDomain"`
	LinkerdVersion           string `json:"linkerdVersion"`
	CreatedByAnnotation      string `json:"createdByAnnotation"`
	ServiceMirror            bool   `json:"serviceMirror"`
	ServiceMirrorUID         int64  `json:"serviceMirrorUID"`
	ServiceMirrorLogLevel    string `json:"serviceMirrorLogLevel"`
	ServiceMirrorRetryLimit  uint32 `json:"serviceMirrorRetryLimit"`
	CliVersion               string `json:"cliVersion"`
	Gateway                  bool   `json:"gateway"`
	GatewayName              string `json:"gatewayName"`
	GatewayPort              uint32 `json:"gatewayPort"`
	GatewayProbeSeconds      uint32 `json:"gatewayProbeSeconds"`
	GatewayProbePort         uint32 `json:"gatewayProbePort"`
	GatewayProbePath         string `json:"gatewayProbePath"`
	GatewayLocalProbePath    string `json:"gatewayLocalProbePath"`
	GatewayLocalProbePort    uint32 `json:"gatewayLocalProbePort"`
	GatewayNginxImageVersion string `json:"gatewaynginxImageVersion"`
	GatewayNginxImage        string `json:"gatewaynginxImage"`
}

// NewValues returns a new instance of the Values type.
func NewValues() (*Values, error) {
	chartDir := fmt.Sprintf("%s/", helmDefaultChartDir)
	v, err := readDefaults(chartDir)
	if err != nil {
		return nil, err
	}

	v.CliVersion = k8s.CreatedByAnnotationValue()
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
