package cni

import (
	"fmt"

	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

const (
	HelmDefaultCNIChartDir = "linkerd2-cni"
)

// Values contains the top-level elements in the cni Helm chart
type Values struct {
	InboundProxyPort    uint          `json:"inboundProxyPort"`
	OutboundProxyPort   uint          `json:"outboundProxyPort"`
	IgnoreInboundPorts  string        `json:"ignoreInboundPorts"`
	IgnoreOutboundPorts string        `json:"ignoreOutboundPorts"`
	CliVersion          string        `json:"cliVersion"`
	CNIPluginImage      string        `json:"cniPluginImage"`
	CNIPluginVersion    string        `json:"cniPluginVersion"`
	LogLevel            string        `json:"logLevel"`
	PortsToRedirect     string        `json:"portsToRedirect"`
	ProxyUID            int64         `json:"proxyUID"`
	DestCNINetDir       string        `json:"destCNINetDir"`
	DestCNIBinDir       string        `json:"destCNIBinDir"`
	UseWaitFlag         bool          `json:"useWaitFlag"`
	PriorityClassName   string        `json:"priorityClassName"`
	ProxyAdminPort      string        `json:"proxyAdminPort"`
	ProxyControlPort    string        `json:"proxyControlPort"`
	Tolerations         []interface{} `json:"tolerations"`
	Resources           []interface{} `json:"resources"`
}

// NewValues returns a new instance of the Values type.
func NewValues() (*Values, error) {
	chartDir := fmt.Sprintf("%s/", HelmDefaultCNIChartDir)
	v, err := readDefaults(chartDir)
	if err != nil {
		return nil, err
	}

	v.CliVersion = k8s.CreatedByAnnotationValue()
	return v, nil
}

// ToMap converts the Values intro a map[string]interface{}
func (v *Values) ToMap() (map[string]interface{}, error) {
	var valuesMap map[string]interface{}
	rawValues, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("Failed to marshal the values struct: %w", err)
	}

	err = yaml.Unmarshal(rawValues, &valuesMap)
	if err != nil {
		return nil, fmt.Errorf("Failed to Unmarshal Values into a map: %w", err)
	}

	return valuesMap, nil
}

// readDefaults reads all the default variables from the values.yaml file.
// chartDir is the root directory of the Helm chart where values.yaml is.
func readDefaults(chartDir string) (*Values, error) {
	file := &loader.BufferedFile{
		Name: chartutil.ValuesfileName,
	}
	if err := charts.ReadFile(static.Templates, chartDir, file); err != nil {
		return nil, err
	}
	values := Values{}
	if err := yaml.Unmarshal(charts.InsertVersion(file.Data), &values); err != nil {
		return nil, err
	}
	return &values, nil
}
