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
		Stage                       string            `json:"stage,omitempty"`
		ControllerImage             string            `json:"controllerImage,omitempty"`
		ControllerImageVersion      string            `json:"controllerImageVersion,omitempty"`
		WebImage                    string            `json:"webImage,omitempty"`
		ControllerReplicas          uint              `json:"controllerReplicas,omitempty"`
		ControllerUID               int64             `json:"controllerUID,omitempty"`
		EnableH2Upgrade             bool              `json:"enableH2Upgrade,omitempty"`
		EnablePodAntiAffinity       bool              `json:"enablePodAntiAffinity,omitempty"`
		WebhookFailurePolicy        string            `json:"webhookFailurePolicy,omitempty"`
		OmitWebhookSideEffects      bool              `json:"omitWebhookSideEffects,omitempty"`
		RestrictDashboardPrivileges bool              `json:"restrictDashboardPrivileges,omitempty"`
		DisableHeartBeat            bool              `json:"disableHeartBeat,omitempty"`
		HeartbeatSchedule           string            `json:"heartbeatSchedule,omitempty"`
		InstallNamespace            bool              `json:"installNamespace,omitempty"`
		Configs                     ConfigJSONs       `json:"configs,omitempty"`
		Global                      *Global           `json:"global,omitempty"`
		Identity                    *Identity         `json:"identity,omitempty"`
		Dashboard                   *Dashboard        `json:"dashboard,omitempty"`
		DebugContainer              *DebugContainer   `json:"debugContainer,omitempty"`
		ProxyInjector               *ProxyInjector    `json:"proxyInjector,omitempty"`
		ProfileValidator            *ProfileValidator `json:"profileValidator,omitempty"`
		Tap                         *Tap              `json:"tap,omitempty"`
		NodeSelector                map[string]string `json:"nodeSelector,omitempty"`
		Tolerations                 []interface{}     `json:"tolerations,omitempty"`
		SMIMetrics                  *SMIMetrics       `json:"smiMetrics,omitempty"`

		DestinationResources   *Resources `json:"destinationResources,omitempty"`
		HeartbeatResources     *Resources `json:"heartbeatResources,omitempty"`
		IdentityResources      *Resources `json:"identityResources,omitempty"`
		ProxyInjectorResources *Resources `json:"proxyInjectorResources,omitempty"`
		PublicAPIResources     *Resources `json:"publicAPIResources,omitempty"`
		SMIMetricsResources    *Resources `json:"smiMetricsResources,omitempty"`
		SPValidatorResources   *Resources `json:"spValidatorResources,omitempty"`
		TapResources           *Resources `json:"tapResources,omitempty"`
		WebResources           *Resources `json:"webResources,omitempty"`

		DestinationProxyResources   *Resources `json:"destinationProxyResources,omitempty"`
		IdentityProxyResources      *Resources `json:"identityProxyResources,omitempty"`
		ProxyInjectorProxyResources *Resources `json:"proxyInjectorProxyResources,omitempty"`
		PublicAPIProxyResources     *Resources `json:"publicAPIProxyResources,omitempty"`
		SMIMetricsProxyResources    *Resources `json:"smiMetricsProxyResources,omitempty"`
		SPValidatorProxyResources   *Resources `json:"spValidatorProxyResources,omitempty"`
		TapProxyResources           *Resources `json:"tapProxyResources,omitempty"`
		WebProxyResources           *Resources `json:"webProxyResources,omitempty"`

		// Addon Structures
		Grafana    *Grafana    `json:"grafana,omitempty"`
		Prometheus *Prometheus `json:"prometheus,omitempty"`
		Tracing    *Tracing    `json:"tracing,omitempty"`
	}

	// Global values common across all charts
	Global struct {
		Namespace                string `json:"namespace,omitempty"`
		ClusterDomain            string `json:"clusterDomain,omitempty"`
		ImagePullPolicy          string `json:"imagePullPolicy,omitempty"`
		CliVersion               string `json:"cliVersion,omitempty"`
		ControllerComponentLabel string `json:"controllerComponentLabel,omitempty"`
		ControllerImageVersion   string `json:"controllerImageVersion,omitempty"`
		ControllerLogLevel       string `json:"controllerLogLevel,omitempty"`
		ControllerNamespaceLabel string `json:"controllerNamespaceLabel,omitempty"`
		WorkloadNamespaceLabel   string `json:"workloadNamespaceLabel,omitempty"`
		CreatedByAnnotation      string `json:"createdByAnnotation,omitempty"`
		ProxyInjectAnnotation    string `json:"proxyInjectAnnotation,omitempty"`
		ProxyInjectDisabled      string `json:"proxyInjectDisabled,omitempty"`
		LinkerdNamespaceLabel    string `json:"linkerdNamespaceLabel,omitempty"`
		ProxyContainerName       string `json:"proxyContainerName,omitempty"`
		HighAvailability         bool   `json:"highAvailability,omitempty"`
		CNIEnabled               bool   `json:"cniEnabled,omitempty"`
		EnableEndpointSlices     bool   `json:"enableEndpointSlices,omitempty"`
		ControlPlaneTracing      bool   `json:"controlPlaneTracing,omitempty"`
		IdentityTrustAnchorsPEM  string `json:"identityTrustAnchorsPEM,omitempty"`
		IdentityTrustDomain      string `json:"identityTrustDomain,omitempty"`
		PrometheusURL            string `json:"prometheusUrl,omitempty"`
		GrafanaURL               string `json:"grafanaUrl,omitempty"`

		Proxy     *Proxy     `json:"proxy,omitempty"`
		ProxyInit *ProxyInit `json:"proxyInit,omitempty"`
	}

	// ConfigJSONs is the JSON encoding of the Linkerd configuration
	ConfigJSONs struct {
		Global  string `json:"global,omitempty"`
		Proxy   string `json:"proxy,omitempty"`
		Install string `json:"install,omitempty"`
	}

	// Proxy contains the fields to set the proxy sidecar container
	Proxy struct {
		Capabilities                  *Capabilities    `json:"capabilities,omitempty"`
		Component                     string           `json:"component,omitempty"`
		DisableIdentity               bool             `json:"disableIdentity,omitempty"`
		DisableTap                    bool             `json:"disableTap,omitempty"`
		EnableExternalProfiles        bool             `json:"enableExternalProfiles,omitempty"`
		DestinationGetNetworks        string           `json:"destinationGetNetworks,omitempty"`
		Image                         *Image           `json:"image,omitempty"`
		LogLevel                      string           `json:"logLevel,omitempty"`
		LogFormat                     string           `json:"logFormat,omitempty"`
		SAMountPath                   *VolumeMountPath `json:"saMountPath,omitempty"`
		Ports                         *Ports           `json:"ports,omitempty"`
		Resources                     *Resources       `json:"resources,omitempty"`
		Trace                         *Trace           `json:"trace,omitempty"`
		UID                           int64            `json:"uid,omitempty"`
		WaitBeforeExitSeconds         uint64           `json:"waitBeforeExitSeconds,omitempty"`
		IsGateway                     bool             `json:"isGateway,omitempty"`
		RequireIdentityOnInboundPorts string           `json:"requireIdentityOnInboundPorts,omitempty"`
		OutboundConnectTimeout        string           `json:"outboundConnectTimeout,omitempty"`
		InboundConnectTimeout         string           `json:"inboundConnectTimeout,omitempty"`
	}

	// ProxyInit contains the fields to set the proxy-init container
	ProxyInit struct {
		Capabilities         *Capabilities    `json:"capabilities,omitempty"`
		IgnoreInboundPorts   string           `json:"ignoreInboundPorts,omitempty"`
		IgnoreOutboundPorts  string           `json:"ignoreOutboundPorts,omitempty"`
		Image                *Image           `json:"image,omitempty"`
		SAMountPath          *VolumeMountPath `json:"saMountPath,omitempty"`
		XTMountPath          *VolumeMountPath `json:"xtMountPath,omitempty"`
		Resources            *Resources       `json:"resources,omitempty"`
		CloseWaitTimeoutSecs int64            `json:"closeWaitTimeoutSecs,omitempty"`
	}

	// DebugContainer contains the fields to set the debugging sidecar
	DebugContainer struct {
		Image *Image `json:"image,omitempty"`
	}

	// Image contains the details to define a container image
	Image struct {
		Name       string `json:"name,omitempty"`
		PullPolicy string `json:"pullPolicy,omitempty"`
		Version    string `json:"version,omitempty"`
	}

	// Ports contains all the port-related setups
	Ports struct {
		Admin    int32 `json:"admin,omitempty"`
		Control  int32 `json:"control,omitempty"`
		Inbound  int32 `json:"inbound,omitempty"`
		Outbound int32 `json:"outbound,omitempty"`
	}

	// Constraints wraps the Limit and Request settings for computational resources
	Constraints struct {
		Limit   string `json:"limit,omitempty"`
		Request string `json:"request,omitempty"`
	}

	// Capabilities contains the SecurityContext capabilities to add/drop into the injected
	// containers
	Capabilities struct {
		Add  []string `json:"add,omitempty"`
		Drop []string `json:"drop,omitempty"`
	}

	// VolumeMountPath contains the details for volume mounts
	VolumeMountPath struct {
		Name      string `json:"name,omitempty"`
		MountPath string `json:"mountPath,omitempty"`
		ReadOnly  bool   `json:"readOnly,omitempty"`
	}

	// Resources represents the computational resources setup for a given container
	Resources struct {
		CPU    *Constraints `json:"cpu,omitempty"`
		Memory *Constraints `json:"memory,omitempty"`
	}

	// Dashboard has the Helm variables for the web dashboard
	Dashboard struct {
		Replicas int32 `json:"replicas,omitempty"`
	}

	// Identity contains the fields to set the identity variables in the proxy
	// sidecar container
	Identity struct {
		Issuer *Issuer `json:"issuer,omitempty"`
	}

	// Issuer has the Helm variables of the identity issuer
	Issuer struct {
		Scheme              string     `json:"scheme,omitempty"`
		ClockSkewAllowance  string     `json:"clockSkewAllowance,omitempty"`
		IssuanceLifetime    string     `json:"issuanceLifetime,omitempty"`
		CrtExpiryAnnotation string     `json:"crtExpiryAnnotation,omitempty"`
		CrtExpiry           time.Time  `json:"crtExpiry,omitempty"`
		TLS                 *IssuerTLS `json:"tls,omitempty"`
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
		Enabled bool   `json:"enabled,omitempty"`
		Image   string `json:"image,omitempty"`
	}

	// TLS has a pair of PEM-encoded key and certificate variables used in the
	// Helm templates
	TLS struct {
		ExternalSecret bool   `json:"externalSecret,omitempty"`
		KeyPEM         string `json:"keyPEM,omitempty"`
		CrtPEM         string `json:"crtPEM,omitempty"`
		CaBundle       string `json:"caBundle,omitempty"`
	}

	// IssuerTLS is a stripped down version of TLS that lacks the integral caBundle.
	// It is tracked separately in the field 'global.IdentityTrustAnchorsPEM'
	IssuerTLS struct {
		KeyPEM string `json:"keyPEM,omitempty"`
		CrtPEM string `json:"crtPEM,omitempty"`
	}

	// Trace has all the tracing-related Helm variables
	Trace struct {
		CollectorSvcAddr    string `json:"collectorSvcAddr,omitempty"`
		CollectorSvcAccount string `json:"collectorSvcAccount,omitempty"`
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
