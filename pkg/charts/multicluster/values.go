package multicluster

import (
	"fmt"

	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

const helmDefaultChartDir = "linkerd2-multicluster-remote-setup"

// Values contains the top-level elements in the Helm charts
type Values struct {
	CliVersion              string `json:"cliVersion"`
	GatewayName             string `json:"gatewayName"`
	GatewayNamespace        string `json:"gatewayNamespace"`
	IdentityTrustDomain     string `json:"identityTrustDomain"`
	IncomingPort            uint32 `json:"incomingPort"`
	LinkerdNamespace        string `json:"linkerdNamespace"`
	ProbePath               string `json:"probePath"`
	ProbePeriodSeconds      uint32 `json:"probePeriodSeconds"`
	ProbePort               uint32 `json:"probePort"`
	ProxyOutboundPort       uint32 `json:"proxyOutboundPort"`
	ServiceAccountName      string `json:"serviceAccountName"`
	ServiceAccountNamespace string `json:"serviceAccountNamespace"`
	NginxImageVersion       string `json:"nginxImageVersion"`
	NginxImage              string `json:"nginxImage"`
	LinkerdVersion          string `json:"linkerdVersion"`
	CreatedByAnnotation     string `json:"createdByAnnotation"`
	LocalProbePath          string `json:"localProbePath"`
	LocalProbePort          uint32 `json:"localProbePort"`
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
