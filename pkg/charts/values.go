package charts

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/imdario/mergo"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

const helmDefaultChartDir = "linkerd2"

type (
	// Values contains the top-level elements in the Helm charts
	Values struct {
		Stage                       string
		Namespace                   string
		ClusterDomain               string
		ControllerImage             string
		ControllerImageVersion      string
		WebImage                    string
		PrometheusImage             string
		GrafanaImage                string
		ImagePullPolicy             string
		UUID                        string
		CliVersion                  string
		ControllerReplicas          uint
		ControllerLogLevel          string
		PrometheusLogLevel          string
		ControllerComponentLabel    string
		ControllerNamespaceLabel    string
		CreatedByAnnotation         string
		ProxyContainerName          string
		ProxyInjectAnnotation       string
		ProxyInjectDisabled         string
		LinkerdNamespaceLabel       string
		ControllerUID               int64
		EnableH2Upgrade             bool
		EnablePodAntiAffinity       bool
		HighAvailability            bool
		NoInitContainer             bool
		WebhookFailurePolicy        string
		OmitWebhookSideEffects      bool
		RestrictDashboardPrivileges bool
		HeartbeatSchedule           string

		Configs configJSONs

		DestinationResources,
		GrafanaResources,
		HeartbeatResources,
		IdentityResources,
		PrometheusResources,
		ProxyInjectorResources,
		PublicAPIResources,
		SPValidatorResources,
		TapResources,
		WebResources *Resources

		Identity         *Identity
		ProxyInjector    *ProxyInjector
		ProfileValidator *ProfileValidator
		Tap              *Tap
		Proxy            *Proxy
		ProxyInit        *ProxyInit
	}

	configJSONs struct{ Global, Proxy, Install string }

	// Proxy contains the fields to set the proxy sidecar container
	Proxy struct {
		Capabilities           *Capabilities
		Component              string
		DisableIdentity        bool
		DisableTap             bool
		EnableExternalProfiles bool
		Image                  *Image
		LogLevel               string
		SAMountPath            *SAMountPath
		Ports                  *Ports
		Resources              *Resources
		UID                    int64
	}

	// ProxyInit contains the fields to set the proxy-init container
	ProxyInit struct {
		Capabilities        *Capabilities
		IgnoreInboundPorts  string
		IgnoreOutboundPorts string
		Image               *Image
		SAMountPath         *SAMountPath
		Resources           *Resources
	}

	// DebugContainer contains the fields to set the debugging sidecar
	DebugContainer struct {
		Image *Image
	}

	// Image contains the details to define a container image
	Image struct {
		Name       string
		PullPolicy string
		Version    string
	}

	// Ports contains all the port-related setups
	Ports struct {
		Admin    int32
		Control  int32
		Inbound  int32
		Outbound int32
	}

	// Constraints wraps the Limit and Request settings for computational resources
	Constraints struct {
		Limit   string
		Request string
	}

	// Capabilities contains the SecurityContext capabilities to add/drop into the injected
	// containers
	Capabilities struct {
		Add  []string
		Drop []string
	}

	// SAMountPath contains the details for ServiceAccount volume mount
	SAMountPath struct {
		Name      string
		MountPath string
		ReadOnly  bool
	}

	// Resources represents the computational resources setup for a given container
	Resources struct {
		CPU    Constraints
		Memory Constraints
	}

	// Identity contains the fields to set the identity variables in the proxy
	// sidecar container
	Identity struct {
		TrustAnchorsPEM string
		TrustDomain     string
		Issuer          *Issuer
	}

	// Issuer has the Helm variables of the identity issuer
	Issuer struct {
		ClockSkewAllowance  string
		IssuanceLifetime    string
		CrtExpiryAnnotation string
		CrtExpiry           time.Time
		TLS                 *TLS
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

	// TLS has a pair of PEM-encoded key and certificate variables used in the
	// Helm templates
	TLS struct {
		KeyPEM, CrtPEM string
	}
)

// NewValues returns a new instance of the Values type.
func NewValues(ha bool) (*Values, error) {
	chartDir := fmt.Sprintf("%s%s", helmDefaultChartDir, string(filepath.Separator))
	v, err := readDefaults(chartDir, ha)
	if err != nil {
		return nil, err
	}

	v.CliVersion = k8s.CreatedByAnnotationValue()
	v.ProxyContainerName = k8s.ProxyContainerName
	v.DestinationResources = &Resources{}
	v.GrafanaResources = &Resources{}
	v.HeartbeatResources = &Resources{}
	v.IdentityResources = &Resources{}
	v.PrometheusResources = &Resources{}
	v.ProxyInjectorResources = &Resources{}
	v.PublicAPIResources = &Resources{}
	v.SPValidatorResources = &Resources{}
	v.TapResources = &Resources{}
	v.WebResources = &Resources{}
	v.Proxy.Component = k8s.Deployment // only Deployment workloads are injected

	return v, nil
}

// readDefaults read all the default variables from the values.yaml file.
// chartDir is the root directory of the Helm chart where values.yaml is.
func readDefaults(chartDir string, ha bool) (*Values, error) {
	valuesFiles := []*chartutil.BufferedFile{
		{Name: helmDefaultValuesFile},
	}

	if ha {
		valuesFiles = append(valuesFiles, &chartutil.BufferedFile{
			Name: helmDefaultHAValuesFile,
		})
	}

	if err := filesReader(chartDir, valuesFiles); err != nil {
		return nil, err
	}

	values := Values{}
	for _, valuesFile := range valuesFiles {
		var v Values
		if err := yaml.Unmarshal(valuesFile.Data, &v); err != nil {
			return nil, err
		}

		merged, err := values.merge(v)
		if err != nil {
			return nil, err
		}

		values = *merged
	}

	return &values, nil
}

// merge merges the non-empty properties of src into v.
// A new Values instance is returned. Neither src nor v are mutated after
// calling merge.
func (v Values) merge(src Values) (*Values, error) {
	// By default, mergo.Merge doesn't overwrite any existing non-empty values
	// in src. So in HA mode, we are merging values.yaml into values-ha.yaml
	// so that all the HA values take precedence.
	if err := mergo.Merge(&src, v); err != nil {
		return nil, err
	}

	newValue := src
	return &newValue, nil
}
