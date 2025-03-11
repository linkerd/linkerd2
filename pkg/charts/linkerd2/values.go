package linkerd2

import (
	"errors"
	"fmt"

	"github.com/imdario/mergo"
	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"helm.sh/helm/v3/pkg/chart/loader"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	// HelmChartDirCrds is the directory name for the linkerd-crds chart
	HelmChartDirCrds = "linkerd-crds"

	// HelmChartDirCP is the directory name for the linkerd-control-plane chart
	HelmChartDirCP = "linkerd-control-plane"
)

type (
	// Values contains the top-level elements in the Helm charts
	Values struct {
		ControllerImage              string                 `json:"controllerImage"`
		ControllerReplicas           uint                   `json:"controllerReplicas"`
		ControllerUID                int64                  `json:"controllerUID"`
		ControllerGID                int64                  `json:"controllerGID"`
		EnableH2Upgrade              bool                   `json:"enableH2Upgrade"`
		EnablePodAntiAffinity        bool                   `json:"enablePodAntiAffinity"`
		NodeAffinity                 map[string]interface{} `json:"nodeAffinity"`
		EnablePodDisruptionBudget    bool                   `json:"enablePodDisruptionBudget"`
		Controller                   *Controller            `json:"controller"`
		WebhookFailurePolicy         string                 `json:"webhookFailurePolicy"`
		DeploymentStrategy           map[string]interface{} `json:"deploymentStrategy,omitempty"`
		DisableHeartBeat             bool                   `json:"disableHeartBeat"`
		HeartbeatSchedule            string                 `json:"heartbeatSchedule"`
		Configs                      ConfigJSONs            `json:"configs"`
		ClusterDomain                string                 `json:"clusterDomain"`
		ClusterNetworks              string                 `json:"clusterNetworks"`
		ImagePullPolicy              string                 `json:"imagePullPolicy"`
		CliVersion                   string                 `json:"cliVersion"`
		ControllerLogLevel           string                 `json:"controllerLogLevel"`
		ControllerLogFormat          string                 `json:"controllerLogFormat"`
		ProxyContainerName           string                 `json:"proxyContainerName"`
		HighAvailability             bool                   `json:"highAvailability"`
		CNIEnabled                   bool                   `json:"cniEnabled"`
		EnableEndpointSlices         bool                   `json:"enableEndpointSlices"`
		DisableIPv6                  bool                   `json:"disableIPv6"`
		ControlPlaneTracing          bool                   `json:"controlPlaneTracing"`
		ControlPlaneTracingNamespace string                 `json:"controlPlaneTracingNamespace"`
		IdentityTrustAnchorsPEM      string                 `json:"identityTrustAnchorsPEM"`
		IdentityTrustDomain          string                 `json:"identityTrustDomain"`
		PrometheusURL                string                 `json:"prometheusUrl"`
		ImagePullSecrets             []map[string]string    `json:"imagePullSecrets"`
		LinkerdVersion               string                 `json:"linkerdVersion"`
		RevisionHistoryLimit         uint                   `json:"revisionHistoryLimit"`

		DestinationController *DestinationController `json:"destinationController"`
		Heartbeat             map[string]interface{} `json:"heartbeat"`
		SPValidator           map[string]interface{} `json:"spValidator"`

		PodAnnotations    map[string]string `json:"podAnnotations"`
		PodLabels         map[string]string `json:"podLabels"`
		PriorityClassName string            `json:"priorityClassName"`

		PodMonitor       *PodMonitor       `json:"podMonitor"`
		PolicyController *PolicyController `json:"policyController"`
		Proxy            *Proxy            `json:"proxy"`
		ProxyInit        *ProxyInit        `json:"proxyInit"`
		NetworkValidator *NetworkValidator `json:"networkValidator"`
		Identity         *Identity         `json:"identity"`
		DebugContainer   *DebugContainer   `json:"debugContainer"`
		ProxyInjector    *ProxyInjector    `json:"proxyInjector"`
		ProfileValidator *Webhook          `json:"profileValidator"`
		PolicyValidator  *Webhook          `json:"policyValidator"`
		NodeSelector     map[string]string `json:"nodeSelector"`
		Tolerations      []interface{}     `json:"tolerations"`

		DestinationResources   *Resources `json:"destinationResources"`
		HeartbeatResources     *Resources `json:"heartbeatResources"`
		IdentityResources      *Resources `json:"identityResources"`
		ProxyInjectorResources *Resources `json:"proxyInjectorResources"`

		DestinationProxyResources   *Resources `json:"destinationProxyResources"`
		IdentityProxyResources      *Resources `json:"identityProxyResources"`
		ProxyInjectorProxyResources *Resources `json:"proxyInjectorProxyResources"`
		Egress                      *Egress    `json:"egress"`
	}

	// Resources represents the computational resources setup for a given container
	Egress struct {
		GlobalEgressNetworkNamespace string `json:"globalEgressNetworkNamespace"`
	}

	// Controller contains the fields to set the controller container
	Controller struct {
		PodDisruptionBudget *PodDisruptionBudget `json:"podDisruptionBudget"`
	}

	DestinationController struct {
		MeshedHttp2ClientProtobuf map[string]interface{} `json:"meshedHttp2ClientProtobuf"`
		PodAnnotations            map[string]string      `json:"podAnnotations"`
	}

	// PodDisruptionBudget contains the fields to set the PDB
	PodDisruptionBudget struct {
		MaxUnavailable int `json:"maxUnavailable"`
	}

	// ConfigJSONs is the JSON encoding of the Linkerd configuration
	ConfigJSONs struct {
		Global  string `json:"global"`
		Proxy   string `json:"proxy"`
		Install string `json:"install"`
	}

	// Proxy contains the fields to set the proxy sidecar container
	Proxy struct {
		Capabilities                         *Capabilities    `json:"capabilities"`
		EnableExternalProfiles               bool             `json:"enableExternalProfiles"`
		Image                                *Image           `json:"image"`
		EnableShutdownEndpoint               bool             `json:"enableShutdownEndpoint"`
		LogLevel                             string           `json:"logLevel"`
		LogFormat                            string           `json:"logFormat"`
		LogHTTPHeaders                       string           `json:"logHTTPHeaders"`
		SAMountPath                          *VolumeMountPath `json:"saMountPath"`
		Ports                                *Ports           `json:"ports"`
		Resources                            *Resources       `json:"resources"`
		UID                                  int64            `json:"uid"`
		GID                                  int64            `json:"gid"`
		WaitBeforeExitSeconds                uint64           `json:"waitBeforeExitSeconds"`
		IsGateway                            bool             `json:"isGateway"`
		IsIngress                            bool             `json:"isIngress"`
		RequireIdentityOnInboundPorts        string           `json:"requireIdentityOnInboundPorts"`
		OutboundConnectTimeout               string           `json:"outboundConnectTimeout"`
		InboundConnectTimeout                string           `json:"inboundConnectTimeout"`
		OutboundDiscoveryCacheUnusedTimeout  string           `json:"outboundDiscoveryCacheUnusedTimeout"`
		InboundDiscoveryCacheUnusedTimeout   string           `json:"inboundDiscoveryCacheUnusedTimeout"`
		DisableOutboundProtocolDetectTimeout bool             `json:"disableOutboundProtocolDetectTimeout"`
		DisableInboundProtocolDetectTimeout  bool             `json:"disableInboundProtocolDetectTimeout"`
		PodInboundPorts                      string           `json:"podInboundPorts"`
		OpaquePorts                          string           `json:"opaquePorts"`
		Await                                bool             `json:"await"`
		DefaultInboundPolicy                 string           `json:"defaultInboundPolicy"`
		OutboundTransportMode                string           `json:"outboundTransportMode"`
		AccessLog                            string           `json:"accessLog"`
		ShutdownGracePeriod                  string           `json:"shutdownGracePeriod"`
		NativeSidecar                        bool             `json:"nativeSidecar"`
		StartupProbe                         *StartupProbe    `json:"startupProbe"`
		ReadinessProbe                       *Probe           `json:"readinessProbe"`
		LivenessProbe                        *Probe           `json:"livenessProbe"`
		Control                              *ProxyControl    `json:"control"`

		AdditionalEnv   []corev1.EnvVar `json:"additionalEnv"`
		ExperimentalEnv []corev1.EnvVar `json:"experimentalEnv"`

		Inbound  ProxyParams `json:"inbound,omitempty"`
		Outbound ProxyParams `json:"outbound,omitempty"`

		// Deprecated: Use Runtime.Workers.Minimum.
		Cores int64 `json:"cores,omitempty"`

		Runtime ProxyRuntime `json:"runtime,omitempty"`
	}

	ProxyParams      = map[string]ProxyScopeParams
	ProxyScopeParams = map[string]ProxyProtoParams
	ProxyProtoParams = map[string]interface{}

	ProxyControl struct {
		Streams *ProxyControlStreams `json:"streams"`
	}

	ProxyControlStreams struct {
		InitialTimeout string `json:"initialTimeout"`
		IdleTimeout    string `json:"idleTimeout"`
		Lifetime       string `json:"lifetime"`
	}

	ProxyRuntime struct {
		Workers ProxyRuntimeWorkers `json:"workers,omitempty"`
	}

	ProxyRuntimeWorkers struct {
		Maximum int64 `json:"maximum,omitempty"`
		Minimum int64 `json:"minimum,omitempty"`

		MaximumByRatioOfAvailableCPUs float64 `json:"maximumByRatioOfAvailableCPUs,omitempty"`
	}

	// ProxyInit contains the fields to set the proxy-init container
	ProxyInit struct {
		Capabilities        *Capabilities    `json:"capabilities"`
		IgnoreInboundPorts  string           `json:"ignoreInboundPorts"`
		IgnoreOutboundPorts string           `json:"ignoreOutboundPorts"`
		KubeAPIServerPorts  string           `json:"kubeAPIServerPorts"`
		SkipSubnets         string           `json:"skipSubnets"`
		LogLevel            string           `json:"logLevel"`
		LogFormat           string           `json:"logFormat"`
		Image               *Image           `json:"image"`
		SAMountPath         *VolumeMountPath `json:"saMountPath"`
		XTMountPath         *VolumeMountPath `json:"xtMountPath"`
		/* DEPRECATED: should be removed after stable-2.16.0, left in for bc */
		Resources            *Resources `json:"resources"`
		CloseWaitTimeoutSecs int64      `json:"closeWaitTimeoutSecs"`
		Privileged           bool       `json:"privileged"`
		RunAsRoot            bool       `json:"runAsRoot"`
		RunAsUser            int64      `json:"runAsUser"`
		RunAsGroup           int64      `json:"runAsGroup"`
		IptablesMode         string     `json:"iptablesMode"`
	}

	NetworkValidator struct {
		LogLevel              string `json:"logLevel"`
		LogFormat             string `json:"logFormat"`
		ConnectAddr           string `json:"connectAddr"`
		ListenAddr            string `json:"listenAddr"`
		Timeout               string `json:"timeout"`
		EnableSecurityContext bool   `json:"enableSecurityContext"`
	}

	// DebugContainer contains the fields to set the debugging sidecar
	DebugContainer struct {
		Image *Image `json:"image"`
	}

	// PodMonitor contains the fields to configure the Prometheus Operator `PodMonitor`
	PodMonitor struct {
		Enabled        bool                  `json:"enabled"`
		ScrapeInterval string                `json:"scrapeInterval"`
		ScrapeTimeout  string                `json:"scrapeTimeout"`
		Controller     *PodMonitorController `json:"controller"`
		ServiceMirror  *PodMonitorComponent  `json:"serviceMirror"`
		Proxy          *PodMonitorComponent  `json:"proxy"`
	}

	// PodMonitorController contains the fields to configure the Prometheus Operator `PodMonitor` for the control-plane
	PodMonitorController struct {
		Enabled           bool   `json:"enabled"`
		NamespaceSelector string `json:"namespaceSelector"`
	}

	// PodMonitorComponent contains the fields to configure the Prometheus Operator `PodMonitor` for other components
	PodMonitorComponent struct {
		Enabled bool `json:"enabled"`
	}

	// PolicyController contains the fields to configure the policy controller container
	PolicyController struct {
		Image         *Image     `json:"image"`
		Resources     *Resources `json:"resources"`
		LogLevel      string     `json:"logLevel"`
		ProbeNetworks []string   `json:"probeNetworks"`
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

	Probe struct {
		InitialDelaySeconds uint `json:"initialDelaySeconds"`
		TimeoutSeconds      uint `json:"timeoutSeconds"`
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
		CPU              Constraints `json:"cpu"`
		Memory           Constraints `json:"memory"`
		EphemeralStorage Constraints `json:"ephemeral-storage"`
	}

	// StartupProbe represents the initContainer startup probe parameters for the proxy
	StartupProbe struct {
		InitialDelaySeconds uint `json:"initialDelaySeconds"`
		PeriodSeconds       uint `json:"periodSeconds"`
		FailureThreshold    uint `json:"failureThreshold"`
	}

	// Identity contains the fields to set the identity variables in the proxy
	// sidecar container
	Identity struct {
		ExternalCA                    bool              `json:"externalCA"`
		ServiceAccountTokenProjection bool              `json:"serviceAccountTokenProjection"`
		Issuer                        *Issuer           `json:"issuer"`
		KubeAPI                       *KubeAPI          `json:"kubeAPI"`
		PodAnnotations                map[string]string `json:"podAnnotations"`

		AdditionalEnv   []corev1.EnvVar `json:"additionalEnv"`
		ExperimentalEnv []corev1.EnvVar `json:"experimentalEnv"`
	}

	// Issuer has the Helm variables of the identity issuer
	Issuer struct {
		Scheme             string     `json:"scheme"`
		ClockSkewAllowance string     `json:"clockSkewAllowance"`
		IssuanceLifetime   string     `json:"issuanceLifetime"`
		TLS                *IssuerTLS `json:"tls"`
	}

	// KubeAPI contains the kube-apiserver client config
	KubeAPI struct {
		ClientQPS   float32 `json:"clientQPS"`
		ClientBurst int     `json:"clientBurst"`
	}

	// ProxyInjector configures the proxy-injector webhook
	ProxyInjector struct {
		Webhook
		PodAnnotations  map[string]string `json:"podAnnotations"`
		AdditionalEnv   []corev1.EnvVar   `json:"additionalEnv"`
		ExperimentalEnv []corev1.EnvVar   `json:"experimentalEnv"`
	}

	// Webhook Helm variables for a webhook
	Webhook struct {
		*TLS
		NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector"`
	}

	// TLS has a pair of PEM-encoded key and certificate variables used in the
	// Helm templates
	TLS struct {
		ExternalSecret     bool   `json:"externalSecret"`
		KeyPEM             string `json:"keyPEM"`
		CrtPEM             string `json:"crtPEM"`
		CaBundle           string `json:"caBundle"`
		InjectCaFrom       string `json:"injectCaFrom"`
		InjectCaFromSecret string `json:"injectCaFromSecret"`
	}

	// IssuerTLS is a stripped down version of TLS that lacks the integral caBundle.
	// It is tracked separately in the field 'IdentityTrustAnchorsPEM'
	IssuerTLS struct {
		KeyPEM string `json:"keyPEM"`
		CrtPEM string `json:"crtPEM"`
	}
)

// NewValues returns a new instance of the Values type.
func NewValues() (*Values, error) {
	v, err := readDefaults(HelmChartDirCrds + "/values.yaml")
	if err != nil {
		return nil, err
	}
	vCP, err := readDefaults(HelmChartDirCP + "/values.yaml")
	if err != nil {
		return nil, err
	}
	*v, err = v.Merge(*vCP)
	if err != nil {
		return nil, err
	}

	v.DebugContainer.Image.Version = version.Version
	v.CliVersion = k8s.CreatedByAnnotationValue()
	v.ProfileValidator.TLS = &TLS{}
	v.ProxyInjector.TLS = &TLS{}
	v.ProxyContainerName = k8s.ProxyContainerName

	return v, nil
}

// ValuesFromConfigMap converts the data in linkerd-config into
// a Values struct
func ValuesFromConfigMap(cm *corev1.ConfigMap) (*Values, error) {
	raw, ok := cm.Data["values"]
	if !ok {
		return nil, errors.New("Linkerd values not found in ConfigMap")
	}
	v := &Values{}
	err := yaml.Unmarshal([]byte(raw), &v)
	return v, err
}

// MergeHAValues retrieves the default HA values and merges them into the received values
func MergeHAValues(values *Values) error {
	haValues, err := readDefaults(HelmChartDirCP + "/values-ha.yaml")
	if err != nil {
		return err
	}
	*values, err = values.Merge(*haValues)
	return err
}

// readDefaults read all the default variables from filename.
func readDefaults(filename string) (*Values, error) {
	valuesFile := &loader.BufferedFile{Name: filename}
	if err := charts.ReadFile(static.Templates, "/", valuesFile); err != nil {
		return nil, err
	}

	var values Values
	err := yaml.Unmarshal(charts.InsertVersion(valuesFile.Data), &values)

	return &values, err
}

// Merge merges the non-empty properties of src into v.
// A new Values instance is returned. Neither src nor v are mutated after
// calling Merge.
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
