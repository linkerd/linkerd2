package linkerd2

import (
	"fmt"
	"time"

	"github.com/imdario/mergo"
	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	helmDefaultChartDir     = "linkerd2"
	helmDefaultHAValuesFile = "values-ha.yaml"
)

type (
	// Values contains the top-level elements in the Helm charts
	Values struct {
		ControllerImage             string            `json:"controllerImage"`
		WebImage                    string            `json:"webImage"`
		ControllerReplicas          uint              `json:"controllerReplicas"`
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
		Tolerations                 []interface{}     `json:"tolerations"`
		Stage                       string            `json:"stage"`

		DestinationResources   *Resources `json:"destinationResources"`
		HeartbeatResources     *Resources `json:"heartbeatResources"`
		IdentityResources      *Resources `json:"identityResources"`
		ProxyInjectorResources *Resources `json:"proxyInjectorResources"`
		PublicAPIResources     *Resources `json:"publicAPIResources"`
		SPValidatorResources   *Resources `json:"spValidatorResources"`
		TapResources           *Resources `json:"tapResources"`
		WebResources           *Resources `json:"webResources"`

		DestinationProxyResources   *Resources `json:"destinationProxyResources"`
		IdentityProxyResources      *Resources `json:"identityProxyResources"`
		ProxyInjectorProxyResources *Resources `json:"proxyInjectorProxyResources"`
		PublicAPIProxyResources     *Resources `json:"publicAPIProxyResources"`
		SPValidatorProxyResources   *Resources `json:"spValidatorProxyResources"`
		TapProxyResources           *Resources `json:"tapProxyResources"`
		WebProxyResources           *Resources `json:"webProxyResources"`

		// Addon Structures
		Grafana    Grafana    `json:"grafana"`
		Prometheus Prometheus `json:"prometheus"`
	}

	// Global values common across all charts
	Global struct {
		Namespace                    string              `json:"namespace"`
		ClusterDomain                string              `json:"clusterDomain"`
		ClusterNetworks              string              `json:"clusterNetworks"`
		ImagePullPolicy              string              `json:"imagePullPolicy"`
		CliVersion                   string              `json:"cliVersion"`
		ControllerComponentLabel     string              `json:"controllerComponentLabel"`
		ControllerImageVersion       string              `json:"controllerImageVersion"`
		ControllerLogLevel           string              `json:"controllerLogLevel"`
		ControllerNamespaceLabel     string              `json:"controllerNamespaceLabel"`
		WorkloadNamespaceLabel       string              `json:"workloadNamespaceLabel"`
		CreatedByAnnotation          string              `json:"createdByAnnotation"`
		ProxyInjectAnnotation        string              `json:"proxyInjectAnnotation"`
		ProxyInjectDisabled          string              `json:"proxyInjectDisabled"`
		LinkerdNamespaceLabel        string              `json:"linkerdNamespaceLabel"`
		ProxyContainerName           string              `json:"proxyContainerName"`
		HighAvailability             bool                `json:"highAvailability"`
		CNIEnabled                   bool                `json:"cniEnabled"`
		EnableEndpointSlices         bool                `json:"enableEndpointSlices"`
		ControlPlaneTracing          bool                `json:"controlPlaneTracing"`
		ControlPlaneTracingNamespace string              `json:"controlPlaneTracingNamespace"`
		IdentityTrustAnchorsPEM      string              `json:"identityTrustAnchorsPEM"`
		IdentityTrustDomain          string              `json:"identityTrustDomain"`
		PrometheusURL                string              `json:"prometheusUrl"`
		GrafanaURL                   string              `json:"grafanaUrl"`
		ImagePullSecrets             []map[string]string `json:"imagePullSecrets"`
		LinkerdVersion               string              `json:"linkerdVersion"`

		PodAnnotations map[string]string `json:"podAnnotations"`
		PodLabels      map[string]string `json:"podLabels"`

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
		Capabilities *Capabilities `json:"capabilities"`
		// This should match .Resources.CPU.Limit, but must be a whole number
		Cores                         int64            `json:"cores,omitempty"`
		DisableIdentity               bool             `json:"disableIdentity"`
		DisableTap                    bool             `json:"disableTap"`
		EnableExternalProfiles        bool             `json:"enableExternalProfiles"`
		Image                         *Image           `json:"image"`
		LogLevel                      string           `json:"logLevel"`
		LogFormat                     string           `json:"logFormat"`
		SAMountPath                   *VolumeMountPath `json:"saMountPath"`
		Ports                         *Ports           `json:"ports"`
		Resources                     *Resources       `json:"resources"`
		UID                           int64            `json:"uid"`
		WaitBeforeExitSeconds         uint64           `json:"waitBeforeExitSeconds"`
		IsGateway                     bool             `json:"isGateway"`
		IsIngress                     bool             `json:"isIngress"`
		RequireIdentityOnInboundPorts string           `json:"requireIdentityOnInboundPorts"`
		OutboundConnectTimeout        string           `json:"outboundConnectTimeout"`
		InboundConnectTimeout         string           `json:"inboundConnectTimeout"`
		OpaquePorts                   string           `json:"opaquePorts"`
	}

	// ProxyInit contains the fields to set the proxy-init container
	ProxyInit struct {
		Capabilities         *Capabilities    `json:"capabilities"`
		IgnoreInboundPorts   string           `json:"ignoreInboundPorts"`
		IgnoreOutboundPorts  string           `json:"ignoreOutboundPorts"`
		Image                *Image           `json:"image"`
		SAMountPath          *VolumeMountPath `json:"saMountPath"`
		XTMountPath          *VolumeMountPath `json:"xtMountPath"`
		Resources            *Resources       `json:"resources"`
		CloseWaitTimeoutSecs int64            `json:"closeWaitTimeoutSecs"`
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

	// VolumeMountPath contains the details for volume mounts
	VolumeMountPath struct {
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
		Scheme              string     `json:"scheme"`
		ClockSkewAllowance  string     `json:"clockSkewAllowance"`
		IssuanceLifetime    string     `json:"issuanceLifetime"`
		CrtExpiryAnnotation string     `json:"crtExpiryAnnotation"`
		CrtExpiry           time.Time  `json:"crtExpiry"`
		TLS                 *IssuerTLS `json:"tls"`
	}

	// ProxyInjector has all the proxy injector's Helm variables
	ProxyInjector struct {
		*TLS
		NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector"`
	}

	// ProfileValidator has all the profile validator's Helm variables
	ProfileValidator struct {
		*TLS
		NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector"`
	}

	// Tap has all the Tap's Helm variables
	Tap struct {
		*TLS
	}

	// TLS has a pair of PEM-encoded key and certificate variables used in the
	// Helm templates
	TLS struct {
		ExternalSecret bool   `json:"externalSecret"`
		KeyPEM         string `json:"keyPEM"`
		CrtPEM         string `json:"crtPEM"`
		CaBundle       string `json:"caBundle"`
	}

	// IssuerTLS is a stripped down version of TLS that lacks the integral caBundle.
	// It is tracked separately in the field 'global.IdentityTrustAnchorsPEM'
	IssuerTLS struct {
		KeyPEM string `json:"keyPEM"`
		CrtPEM string `json:"crtPEM"`
	}
)

// NewValues returns a new instance of the Values type.
func NewValues() (*Values, error) {
	v, err := readDefaults(false)
	if err != nil {
		return nil, err
	}

	v.Global.ControllerImageVersion = version.Version
	v.Global.Proxy.Image.Version = version.Version
	v.DebugContainer.Image.Version = version.Version
	v.Global.CliVersion = k8s.CreatedByAnnotationValue()
	v.ProfileValidator.TLS = &TLS{}
	v.ProxyInjector.TLS = &TLS{}
	v.Global.ProxyContainerName = k8s.ProxyContainerName
	v.Tap = &Tap{TLS: &TLS{}}

	return v, nil
}

// MergeHAValues retrieves the default HA values and merges them into the received values
func MergeHAValues(values *Values) error {
	haValues, err := readDefaults(true)
	if err != nil {
		return err
	}
	*values, err = values.Merge(*haValues)
	return err
}

// readDefaults read all the default variables from the values.yaml file.
func readDefaults(ha bool) (*Values, error) {
	var valuesFile *loader.BufferedFile
	if ha {
		valuesFile = &loader.BufferedFile{Name: helmDefaultHAValuesFile}
	} else {
		valuesFile = &loader.BufferedFile{Name: chartutil.ValuesfileName}
	}

	chartDir := fmt.Sprintf("%s/", helmDefaultChartDir)
	if err := charts.ReadFile(static.Templates, chartDir, valuesFile); err != nil {
		return nil, err
	}

	var values Values
	err := yaml.Unmarshal(charts.InsertVersion(valuesFile.Data), &values)

	return &values, err
}

// Merge merges the non-empty properties of src into v.
// A new Values instance is returned. Neither src nor v are mutated after
// calling merge.
func (v Values) Merge(src Values) (Values, error) {
	// By default, mergo.Merge doesn't overwrite any existing non-empty values
	// in its first argument. So in HA mode, we are merging values.yaml into
	// values-ha.yaml, instead of the other way round (like Helm). This ensures
	// that all the HA values take precedence.
	if err := mergo.Merge(&src, v); err != nil {
		return Values{}, err
	}

	return src, nil
}

// DeepCopy creates a deep copy of the Values struct by marshalling to yaml and
// then unmarshalling a new struct.
func (v *Values) DeepCopy() (*Values, error) {
	dst := Values{}
	bytes, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(bytes, &dst)
	if err != nil {
		return nil, err
	}
	return &dst, nil
}

func (v *Values) String() string {
	bytes, _ := yaml.Marshal(v)
	return string(bytes)
}

// GetGlobal is a safe accessor for Global. It initializes Global on the first
// access.
func (v *Values) GetGlobal() *Global {
	if v.Global == nil {
		v.Global = &Global{}
	}
	return v.Global
}
