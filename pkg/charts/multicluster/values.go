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
	CliVersion               string `json:"cliVersion"`
	ControllerComponentLabel string `json:"controllerComponentLabel"`
	ControllerImage          string `json:"controllerImage"`
	ControllerImageVersion   string `json:"controllerImageVersion"`
	CreatedByAnnotation      string `json:"createdByAnnotation"`
	Gateway                  bool   `json:"gateway"`
	GatewayLocalProbePath    string `json:"gatewayLocalProbePath"`
	GatewayLocalProbePort    uint32 `json:"gatewayLocalProbePort"`
	GatewayName              string `json:"gatewayName"`
	GatewayNginxImage        string `json:"gatewayNginxImage"`
	GatewayNginxImageVersion string `json:"gatewayNginxImageVersion"`
	GatewayPort              uint32 `json:"gatewayPort"`
	GatewayProbePath         string `json:"gatewayProbePath"`
	GatewayProbePort         uint32 `json:"gatewayProbePort"`
	GatewayProbeSeconds      uint32 `json:"gatewayProbeSeconds"`
	IdentityTrustDomain      string `json:"identityTrustDomain"`
	LinkerdNamespace         string `json:"linkerdNamespace"`
	LinkerdVersion           string `json:"linkerdVersion"`
	Namespace                string `json:"namespace"`
	ProxyOutboundPort        uint32 `json:"proxyOutboundPort"`
	ServiceMirror            bool   `json:"serviceMirror"`
	ServiceMirrorLogLevel    string `json:"serviceMirrorLogLevel"`
	ServiceMirrorRetryLimit  uint32 `json:"serviceMirrorRetryLimit"`
	ServiceMirrorUID         int64  `json:"serviceMirrorUID"`
	RemoteMirrorServiceAccount bool `json:"remoteMirrorServiceAccount"`
	RemoteMirrorServiceAccountName string `json:"remoteMirrorServiceAccountName"`

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
