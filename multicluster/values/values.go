package values

import (
	"fmt"

	"github.com/linkerd/linkerd2/multicluster/static"
	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	corev1 "k8s.io/api/core/v1"
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
	LogFormat                      string   `json:"logFormat"`
	ServiceMirrorRetryLimit        uint32   `json:"serviceMirrorRetryLimit"`
	ServiceMirrorUID               int64    `json:"serviceMirrorUID"`
	ServiceMirrorGID               int64    `json:"serviceMirrorGID"`
	Replicas                       uint32   `json:"replicas"`
	RemoteMirrorServiceAccount     bool     `json:"remoteMirrorServiceAccount"`
	RemoteMirrorServiceAccountName string   `json:"remoteMirrorServiceAccountName"`
	TargetClusterName              string   `json:"targetClusterName"`
	EnablePodAntiAffinity          bool     `json:"enablePodAntiAffinity"`
	RevisionHistoryLimit           uint32   `json:"revisionHistoryLimit"`

	ServiceMirrorAdditionalEnv   []corev1.EnvVar `json:"serviceMirrorAdditionalEnv"`
	ServiceMirrorExperimentalEnv []corev1.EnvVar `json:"serviceMirrorExperimentalEnv"`

	LocalServiceMirror *LocalServiceMirror `json:"localServiceMirror"`
	ControllerDefaults *ControllerDefaults `json:"controllerDefaults"`
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
	GID                int64             `json:"GID"`
}

// Probe contains all options for the Probe Service
type Probe struct {
	FailureThreshold uint32 `json:"failureThreshold"`
	Path             string `json:"path"`
	Port             uint32 `json:"port"`
	NodePort         uint32 `json:"nodePort"`
	Seconds          uint32 `json:"seconds"`
	Timeout          string `json:"timeout"`
}

type LocalServiceMirror struct {
	ServiceMirrorRetryLimit  uint32          `json:"serviceMirrorRetryLimit"`
	FederatedServiceSelector string          `json:"federatedServiceSelector"`
	Replias                  uint32          `json:"replicas"`
	Image                    *linkerd2.Image `json:"image"`
	LogLevel                 string          `json:"logLevel"`
	LogFormat                string          `json:"logFormat"`
	EnablePprof              bool            `json:"enablePprof"`
	UID                      int64           `json:"UID"`
	GID                      int64           `json:"GID"`
}

// ControllerDefaults is used to unmarshal the default values.yaml file, so
// only entries that are objects that are not empty by default are required.
type ControllerDefaults struct {
	Gateway *ControllerDefaultsGateway `json:"gateway"`
	Image   *linkerd2.Image            `json:"image"`
}

type ControllerDefaultsGateway struct {
	Probe *ControllerDefaultsProbe `json:"probe"`
}

type ControllerDefaultsProbe struct {
	Port uint32 `json:"port"`
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
