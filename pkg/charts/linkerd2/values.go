package linkerd2

import (
	"fmt"
	"time"

	"github.com/imdario/mergo"
	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

const (
	helmDefaultChartDir     = "linkerd2"
	helmDefaultHAValuesFile = "values-ha.yaml"
)

type (
	// Values contains the top-level elements in the Helm charts
	Values struct {
		Stage                       string            `json:"stage"`
		ControllerImage             string            `json:"controllerImage"`
		ControllerImageVersion      string            `json:"controllerImageVersion"`
		WebImage                    string            `json:"webImage"`
		PrometheusImage             string            `json:"prometheusImage"`
		GrafanaImage                string            `json:"grafanaImage"`
		ControllerReplicas          uint              `json:"controllerReplicas"`
		ControllerLogLevel          string            `json:"controllerLogLevel"`
		PrometheusLogLevel          string            `json:"prometheusLogLevel"`
		ControllerUID               int64             `json:"controllerUID"`
		EnableH2Upgrade             bool              `json:"enableH2Upgrade"`
		EnablePodAntiAffinity       bool              `json:"enablePodAntiAffinity"`
		WebhookFailurePolicy        string            `json:"webhookFailurePolicy"`
		OmitWebhookSideEffects      bool              `json:"omitWebhookSideEffects"`
		RestrictDashboardPrivileges bool              `json:"restrictDashboardPrivileges"`
		DisableHeartBeat            bool              `json:"disableHeartBeat"`
		HeartbeatSchedule           string            `json:"heartbeatSchedule"`
		InstallNamespace            bool              `json:"installNamespace"`
		Configs                     ConfigJSONs       `json:"configs"`
		Global                      *Global           `json:"global"`
		Identity                    *Identity         `json:"identity"`
		Dashboard                   *Dashboard        `json:"dashboard"`
		DebugContainer              *DebugContainer   `json:"debugContainer"`
		ProxyInjector               *ProxyInjector    `json:"proxyInjector"`
		ProfileValidator            *ProfileValidator `json:"profileValidator"`
		Tap                         *Tap              `json:"tap"`
		NodeSelector                map[string]string `json:"nodeSelector"`
		SMIMetrics                  *SMIMetrics       `json:"smiMetrics"`

		DestinationResources   *Resources `json:"destinationResources"`
		GrafanaResources       *Resources `json:"grafanaResources"`
		HeartbeatResources     *Resources `json:"heartbeatResources"`
		IdentityResources      *Resources `json:"identityResources"`
		PrometheusResources    *Resources `json:"prometheusResources"`
		ProxyInjectorResources *Resources `json:"proxyInjectorResources"`
		PublicAPIResources     *Resources `json:"publicAPIResources"`
		SPValidatorResources   *Resources `json:"spValidatorResources"`
		TapResources           *Resources `json:"tapResources"`
		WebResources           *Resources `json:"webResources"`

		// Addon Structures
		Tracing Tracing `json:"tracing"`
	}

	// Global values common across all charts
	Global struct {
		Namespace                string `json:"namespace"`
		ClusterDomain            string `json:"clusterDomain"`
		ImagePullPolicy          string `json:"imagePullPolicy"`
		CliVersion               string `json:"cliVersion"`
		ControllerComponentLabel string `json:"controllerComponentLabel"`
		ControllerNamespaceLabel string `json:"controllerNamespaceLabel"`
		CreatedByAnnotation      string `json:"createdByAnnotation"`
		ProxyInjectAnnotation    string `json:"proxyInjectAnnotation"`
		ProxyInjectDisabled      string `json:"proxyInjectDisabled"`
		LinkerdNamespaceLabel    string `json:"linkerdNamespaceLabel"`
		ProxyContainerName       string `json:"proxyContainerName"`
		HighAvailability         bool   `json:"highAvailability"`
		CNIEnabled               bool   `json:"cniEnabled"`
		ControlPlaneTracing      bool   `json:"controlPlaneTracing"`
		IdentityTrustAnchorsPEM  string `json:"identityTrustAnchorsPEM"`
		IdentityTrustDomain      string `json:"identityTrustDomain"`

		Proxy     *Proxy     `json:"proxy"`
		ProxyInit *ProxyInit `json:"proxyInit"`
	}

	// ConfigJSONs is the JSON encoding of the Linkerd configuration
	ConfigJSONs struct {
		Global  string `json:"global"`
		Proxy   string `json:"proxy"`
		Install string `json:"install"`
	}

	// Proxy contains the fields to set the proxy sidecar container
	Proxy struct {
		Capabilities           *Capabilities `json:"capabilities"`
		Component              string        `json:"component"`
		DisableIdentity        bool          `json:"disableIdentity"`
		DisableTap             bool          `json:"disableTap"`
		EnableExternalProfiles bool          `json:"enableExternalProfiles"`
		Image                  *Image        `json:"image"`
		LogLevel               string        `json:"logLevel"`
		SAMountPath            *SAMountPath  `json:"saMountPath"`
		Ports                  *Ports        `json:"ports"`
		Resources              *Resources    `json:"resources"`
		Trace                  *Trace        `json:"trace"`
		UID                    int64         `json:"uid"`
		WaitBeforeExitSeconds  uint64        `json:"waitBeforeExitSeconds"`
	}

	// ProxyInit contains the fields to set the proxy-init container
	ProxyInit struct {
		Capabilities        *Capabilities `json:"capabilities"`
		IgnoreInboundPorts  string        `json:"ignoreInboundPorts"`
		IgnoreOutboundPorts string        `json:"ignoreOutboundPorts"`
		Image               *Image        `json:"image"`
		SAMountPath         *SAMountPath  `json:"saMountPath"`
		Resources           *Resources    `json:"resources"`
	}

	// DebugContainer contains the fields to set the debugging sidecar
	DebugContainer struct {
		Image *Image `json:"image"`
	}

	// Image contains the details to define a container image
	Image struct {
		Name       string `json:"name"`
		PullPolicy string `json:"pullPolicy"`
		Version    string `json:"version"`
	}

	// Ports contains all the port-related setups
	Ports struct {
		Admin    int32 `json:"admin"`
		Control  int32 `json:"control"`
		Inbound  int32 `json:"inbound"`
		Outbound int32 `json:"outbound"`
	}

	// Constraints wraps the Limit and Request settings for computational resources
	Constraints struct {
		Limit   string `json:"limit"`
		Request string `json:"request"`
	}

	// Capabilities contains the SecurityContext capabilities to add/drop into the injected
	// containers
	Capabilities struct {
		Add  []string `json:"add"`
		Drop []string `json:"drop"`
	}

	// SAMountPath contains the details for ServiceAccount volume mount
	SAMountPath struct {
		Name      string `json:"name"`
		MountPath string `json:"mountPath"`
		ReadOnly  bool   `json:"readOnly"`
	}

	// Resources represents the computational resources setup for a given container
	Resources struct {
		CPU    Constraints `json:"cpu"`
		Memory Constraints `json:"memory"`
	}

	// Dashboard has the Helm variables for the web dashboard
	Dashboard struct {
		Replicas int32 `json:"replicas"`
	}

	// Identity contains the fields to set the identity variables in the proxy
	// sidecar container
	Identity struct {
		Issuer *Issuer `json:"issuer"`
	}

	// Issuer has the Helm variables of the identity issuer
	Issuer struct {
		Scheme              string    `json:"scheme"`
		ClockSkewAllowance  string    `json:"clockSkewAllowance"`
		IssuanceLifetime    string    `json:"issuanceLifetime"`
		CrtExpiryAnnotation string    `json:"crtExpiryAnnotation"`
		CrtExpiry           time.Time `json:"crtExpiry"`
		TLS                 *TLS      `json:"tls"`
	}

	// ProxyInjector has all the proxy injector's Helm variables
	ProxyInjector struct {
		*TLS
	}

	// ProfileValidator has all the profile validator's Helm variables
	ProfileValidator struct {
		*TLS
	}

	// Tap has all the Tap's Helm variables
	Tap struct {
		*TLS
	}

	// SMIMetrics has all the SMIMetrics's Helm variables
	SMIMetrics struct {
		*TLS
		Enabled bool   `json:"enabled"`
		Image   string `json:"image"`
	}

	// TLS has a pair of PEM-encoded key and certificate variables used in the
	// Helm templates
	TLS struct {
		KeyPEM string `json:"keyPEM"`
		CrtPEM string `json:"crtPEM"`
	}

	// Trace has all the Tracing-related Helm variables
	Trace struct {
		CollectorSvcAddr    string `json:"collectorSvcAddr"`
		CollectorSvcAccount string `json:"collectorSvcAccount"`
	}
)

// NewValues returns a new instance of the Values type.
func NewValues(ha bool) (*Values, error) {
	chartDir := fmt.Sprintf("%s/", helmDefaultChartDir)
	v, err := readDefaults(chartDir, ha)
	if err != nil {
		return nil, err
	}

	v.Global.CliVersion = k8s.CreatedByAnnotationValue()
	v.ProfileValidator = &ProfileValidator{TLS: &TLS{}}
	v.ProxyInjector = &ProxyInjector{TLS: &TLS{}}
	v.Global.ProxyContainerName = k8s.ProxyContainerName
	v.Tap = &Tap{TLS: &TLS{}}

	return v, nil
}

// readDefaults read all the default variables from the values.yaml file.
// chartDir is the root directory of the Helm chart where values.yaml is.
func readDefaults(chartDir string, ha bool) (*Values, error) {
	valuesFiles := []*chartutil.BufferedFile{
		{Name: chartutil.ValuesfileName},
	}

	if ha {
		valuesFiles = append(valuesFiles, &chartutil.BufferedFile{
			Name: helmDefaultHAValuesFile,
		})
	}

	if err := charts.FilesReader(chartDir, valuesFiles); err != nil {
		return nil, err
	}

	values := Values{}
	for _, valuesFile := range valuesFiles {
		var v Values
		if err := yaml.Unmarshal(charts.InsertVersion(valuesFile.Data), &v); err != nil {
			return nil, err
		}

		var err error
		values, err = values.merge(v)
		if err != nil {
			return nil, err
		}
	}

	return &values, nil
}

// merge merges the non-empty properties of src into v.
// A new Values instance is returned. Neither src nor v are mutated after
// calling merge.
func (v Values) merge(src Values) (Values, error) {
	// By default, mergo.Merge doesn't overwrite any existing non-empty values
	// in its first argument. So in HA mode, we are merging values.yaml into
	// values-ha.yaml, instead of the other way round (like Helm). This ensures
	// that all the HA values take precedence.
	if err := mergo.Merge(&src, v); err != nil {
		return Values{}, err
	}

	return src, nil
}
