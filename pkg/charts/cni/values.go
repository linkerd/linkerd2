package cni

import (
	"fmt"

	"github.com/linkerd/linkerd2/charts"
	chartspkg "github.com/linkerd/linkerd2/pkg/charts"
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

// RepairController contains the config for the repair-controller container
type RepairController struct {
	Image                 Image     `json:"image"`
	LogLevel              string    `json:"logLevel"`
	LogFormat             string    `json:"logFormat"`
	EnableSecurityContext bool      `json:"enableSecurityContext"`
	Resources             Resources `json:"resources"`
}

// Values contains the top-level elements in the cni Helm chart
type Values struct {
	InboundProxyPort     uint                   `json:"inboundProxyPort"`
	OutboundProxyPort    uint                   `json:"outboundProxyPort"`
	IgnoreInboundPorts   string                 `json:"ignoreInboundPorts"`
	IgnoreOutboundPorts  string                 `json:"ignoreOutboundPorts"`
	CliVersion           string                 `json:"cliVersion"`
	Image                Image                  `json:"image"`
	LogLevel             string                 `json:"logLevel"`
	PortsToRedirect      string                 `json:"portsToRedirect"`
	ProxyUID             int64                  `json:"proxyUID"`
	ProxyGID             int64                  `json:"proxyGID"`
	DestCNINetDir        string                 `json:"destCNINetDir"`
	DestCNIBinDir        string                 `json:"destCNIBinDir"`
	UseWaitFlag          bool                   `json:"useWaitFlag"`
	PriorityClassName    string                 `json:"priorityClassName"`
	ProxyAdminPort       string                 `json:"proxyAdminPort"`
	ProxyControlPort     string                 `json:"proxyControlPort"`
	Tolerations          []interface{}          `json:"tolerations"`
	PodLabels            map[string]string      `json:"podLabels"`
	CommonLabels         map[string]string      `json:"commonLabels"`
	ImagePullSecrets     []map[string]string    `json:"imagePullSecrets"`
	ExtraInitContainers  []interface{}          `json:"extraInitContainers"`
	IptablesMode         string                 `json:"iptablesMode"`
	DisableIPv6          bool                   `json:"disableIPv6"`
	EnablePSP            bool                   `json:"enablePSP"`
	Privileged           bool                   `json:"privileged"`
	Resources            Resources              `json:"resources"`
	RepairController     RepairController       `json:"repairController"`
	RevisionHistoryLimit uint                   `json:"revisionHistoryLimit"`
	UpdateStrategy       map[string]interface{} `json:"updateStrategy"`
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

// ToMap converts Values into a map[string]interface{}
func (v *Values) ToMap() (map[string]interface{}, error) {
	var valuesMap map[string]interface{}
	rawValues, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the values struct: %w", err)
	}

	err = yaml.Unmarshal(rawValues, &valuesMap)
	if err != nil {
		return nil, fmt.Errorf("failed to Unmarshal Values into a map: %w", err)
	}

	return valuesMap, nil
}

// readDefaults reads all the default variables from the values.yaml file.
// chartDir is the root directory of the Helm chart where values.yaml is.
func readDefaults(chartDir string) (*Values, error) {
	file := &loader.BufferedFile{
		Name: chartutil.ValuesfileName,
	}
	if err := chartspkg.ReadFile(charts.Templates, chartDir, file); err != nil {
		return nil, err
	}
	values := Values{}
	if err := yaml.Unmarshal(chartspkg.InsertVersion(file.Data), &values); err != nil {
		return nil, err
	}
	return &values, nil
}
