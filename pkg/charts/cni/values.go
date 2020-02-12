package cni

import (
	"fmt"

	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

const (
	helmDefaultCNIChartDir = "linkerd2-cni"
)

// Values contains the top-level elements in the cni Helm chart
type Values struct {
	Namespace             string `json:"namespace"`
	CNIResourceLabel      string `json:"cniResourceLabel"`
	InboundProxyPort      uint   `json:"inboundProxyPort"`
	OutboundProxyPort     uint   `json:"outboundProxyPort"`
	IgnoreInboundPorts    string `json:"ignoreInboundPorts"`
	IgnoreOutboundPorts   string `json:"ignoreOutboundPorts"`
	CreatedByAnnotation   string `json:"createdByAnnotation"`
	CliVersion            string `json:"cliVersion"`
	CNIPluginImage        string `json:"cniPluginImage"`
	CNIPluginVersion      string `json:"cniPluginVersion"`
	LogLevel              string `json:"logLevel"`
	PortsToRedirect       string `json:"portsToRedirect"`
	ProxyUID              int64  `json:"proxyUID"`
	DestCNINetDir         string `json:"destCNINetDir"`
	DestCNIBinDir         string `json:"destCNIBinDir"`
	UseWaitFlag           bool   `json:"useWaitFlag"`
	ProxyInjectAnnotation string `json:"proxyInjectAnnotation"`
	ProxyInjectDisabled   string `json:"proxyInjectDisabled"`
}

// NewValues returns a new instance of the Values type.
func NewValues() (*Values, error) {
	chartDir := fmt.Sprintf("%s/", helmDefaultCNIChartDir)
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
	if err := yaml.Unmarshal(file.Data, &values); err != nil {
		return nil, err
	}
	return &values, nil
}
