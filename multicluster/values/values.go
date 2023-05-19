package values

import (
	"fmt"

	"github.com/linkerd/linkerd2/multicluster/static"
	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

const (
	helmDefaultChartDir     = "linkerd-multicluster"
	helmDefaultLinkChartDir = "linkerd-multicluster-link"
)

// Values contains the top-level elements in the Helm charts
type Values struct {
	CliVersion                     string   `json:"cliVersion"`
	ControllerImage                string   `json:"controllerImage"`
	ControllerImageVersion         string   `json:"controllerImageVersion"`
	Gateway                        *Gateway `json:"gateway"`
	IdentityTrustDomain            string   `json:"identityTrustDomain"`
	LinkerdNamespace               string   `json:"linkerdNamespace"`
	LinkerdVersion                 string   `json:"linkerdVersion"`
	ProxyOutboundPort              uint32   `json:"proxyOutboundPort"`
	ServiceMirror                  bool     `json:"serviceMirror"`
	LogLevel                       string   `json:"logLevel"`
	ServiceMirrorRetryLimit        uint32   `json:"serviceMirrorRetryLimit"`
	ServiceMirrorUID               int64    `json:"serviceMirrorUID"`
	RemoteMirrorServiceAccount     bool     `json:"remoteMirrorServiceAccount"`
	RemoteMirrorServiceAccountName string   `json:"remoteMirrorServiceAccountName"`
	TargetClusterName              string   `json:"targetClusterName"`
	EnablePodAntiAffinity          bool     `json:"enablePodAntiAffinity"`
}

// Gateway contains all options related to the Gateway Service
type Gateway struct {
	Enabled            bool              `json:"enabled"`
	Replicas           uint32            `json:"replicas"`
	Name               string            `json:"name"`
	Port               uint32            `json:"port"`
	NodePort           uint32            `json:"nodePort"`
	ServiceType        string            `json:"serviceType"`
	Probe              *Probe            `json:"probe"`
	ServiceAnnotations map[string]string `json:"serviceAnnotations"`
	LoadBalancerIP     string            `json:"loadBalancerIP"`
	PauseImage         string            `json:"pauseImage"`
	UID                int64             `json:"UID"`
}

// Probe contains all options for the Probe Service
type Probe struct {
	Path     string `json:"path"`
	Port     uint32 `json:"port"`
	NodePort uint32 `json:"nodePort"`
	Seconds  uint32 `json:"seconds"`
}

// NewInstallValues returns a new instance of the Values type.
func NewInstallValues() (*Values, error) {
	chartDir := fmt.Sprintf("%s/", helmDefaultChartDir)
	v, err := readDefaults(chartDir)
	if err != nil {
		return nil, err
	}

	v.CliVersion = k8s.CreatedByAnnotationValue()
	return v, nil
}

// NewLinkValues returns a new instance of the Values type.
func NewLinkValues() (*Values, error) {
	chartDir := fmt.Sprintf("%s/", helmDefaultLinkChartDir)
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
