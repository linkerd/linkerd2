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
	helmDefaultCNIChartDir = "linkerd2-cni"
)

// Image contains details about the location of the container image
type Image struct {
	Name       string      `json:"name"`
	Version    string      `json:"version"`
	PullPolicy interface{} `json:"pullPolicy"`
}

// Constraints wraps the Limit and Request settings for computational resources
type Constraints struct {
	Limit   string `json:"limit"`
	Request string `json:"request"`
}

// Resources represents the computational resources setup for a given container
type Resources struct {
	CPU              Constraints `json:"cpu"`
	Memory           Constraints `json:"memory"`
	EphemeralStorage Constraints `json:"ephemeral-storage"`
}

// Values contains the top-level elements in the cni Helm chart
type Values struct {
	InboundProxyPort    uint                `json:"inboundProxyPort"`
	OutboundProxyPort   uint                `json:"outboundProxyPort"`
	IgnoreInboundPorts  string              `json:"ignoreInboundPorts"`
	IgnoreOutboundPorts string              `json:"ignoreOutboundPorts"`
	CliVersion          string              `json:"cliVersion"`
	Image               Image               `json:"image"`
	LogLevel            string              `json:"logLevel"`
	PortsToRedirect     string              `json:"portsToRedirect"`
	ProxyUID            int64               `json:"proxyUID"`
	DestCNINetDir       string              `json:"destCNINetDir"`
	DestCNIBinDir       string              `json:"destCNIBinDir"`
	UseWaitFlag         bool                `json:"useWaitFlag"`
	PriorityClassName   string              `json:"priorityClassName"`
	ProxyAdminPort      string              `json:"proxyAdminPort"`
	ProxyControlPort    string              `json:"proxyControlPort"`
	Tolerations         []interface{}       `json:"tolerations"`
	PodLabels           map[string]string   `json:"podLabels"`
	CommonLabels        map[string]string   `json:"commonLabels"`
	ImagePullSecrets    []map[string]string `json:"imagePullSecrets"`
	ExtraInitContainers []interface{}       `json:"extraInitContainers"`
	EnablePSP           bool                `json:"enablePSP"`
	Privileged    		bool				`json:"privileged"`
	Resources           Resources           `json:"resources"`
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
