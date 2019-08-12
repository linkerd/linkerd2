package charts

import (
	"time"

	"k8s.io/helm/pkg/chartutil"
)

const (
	helmDefaultValuesFile   = "values.yaml"
	helmDefaultHAValuesFile = "values-ha.yaml"
)

// DefaultValues contain all the default variables defined in the values.yaml
// and values-ha.yaml.
type DefaultValues struct {
	ControllerReplicas               uint
	ControllerLogLevel               string
	ControllerCPULimit               string
	ControllerCPURequest             string
	ControllerMemoryLimit            string
	ControllerMemoryRequest          string
	ControllerUID                    int64
	EnableExternalProfiles           bool
	EnableH2Upgrade                  bool
	GrafanaCPULimit                  string
	GrafanaCPURequest                string
	GrafanaMemoryLimit               string
	GrafanaMemoryRequest             string
	ImagePullPolicy                  string
	IdentityCPULimit                 string
	IdentityCPURequest               string
	IdentityMemoryLimit              string
	IdentityMemoryRequest            string
	IdentityTrustDomain              string
	IdentityIssuerClockSkewAllowance time.Duration
	IdentityIssuerIssuanceLifetime   time.Duration
	OmitWebhookSideEffects           bool
	PrometheusCPULimit               string
	PrometheusCPURequest             string
	PrometheusImage                  string
	PrometheusMemoryLimit            string
	PrometheusMemoryRequest          string
	ProxyAdminPort                   uint
	ProxyControlPort                 uint
	ProxyCPULimit                    string
	ProxyCPURequest                  string
	ProxyImageName                   string
	ProxyInboundPort                 uint
	ProxyInitImageName               string
	ProxyInitCPULimit                string
	ProxyInitCPURequest              string
	ProxyInitMemoryLimit             string
	ProxyInitMemoryRequest           string
	ProxyLogLevel                    string
	ProxyMemoryLimit                 string
	ProxyMemoryRequest               string
	ProxyOutboundPort                uint
	ProxyUID                         int64
	WebhookFailurePolicy             string
}

// ReadDefaults read all the default variables from the values.yaml file.
// If ha is true, values-ha.yaml will be merged into values.yaml.
// chartDir is the root directory of the Helm chart where values.yaml is.
func ReadDefaults(chartDir string, ha bool) (*DefaultValues, error) {
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

	values := chartutil.Values{}
	for _, valuesFile := range valuesFiles {
		v, err := chartutil.ReadValues(valuesFile.Data)
		if err != nil {
			return nil, err
		}
		values.MergeInto(v)
	}
	return setDefaults(values, ha)
}

func setDefaults(defaults chartutil.Values, ha bool) (*DefaultValues, error) {
	identity, err := defaults.Table("Identity")
	if err != nil {
		return nil, err
	}

	identityIssuer, err := defaults.Table("Identity.Issuer")
	if err != nil {
		return nil, err
	}

	identityIssuanceLifetime, err := time.ParseDuration(identityIssuer["IssuanceLifeTime"].(string))
	if err != nil {
		return nil, err
	}

	identityClockSkewAllowance, err := time.ParseDuration(identityIssuer["ClockSkewAllowance"].(string))
	if err != nil {
		return nil, err
	}

	proxy, err := defaults.Table("Proxy")
	if err != nil {
		return nil, err
	}

	proxyImage, err := defaults.Table("Proxy.Image")
	if err != nil {
		return nil, err
	}

	proxyInitImage, err := defaults.Table("ProxyInit.Image")
	if err != nil {
		return nil, err
	}

	proxyInitResourcesCPU, err := defaults.Table("ProxyInit.Resources.CPU")
	if err != nil {
		return nil, err
	}

	proxyInitResourcesMemory, err := defaults.Table("ProxyInit.Resources.Memory")
	if err != nil {
		return nil, err
	}

	proxyPorts, err := defaults.Table("Proxy.Ports")
	if err != nil {
		return nil, err
	}

	proxyResourcesCPU, err := defaults.Table("Proxy.Resources.CPU")
	if err != nil {
		return nil, err
	}

	proxyResourcesMemory, err := defaults.Table("Proxy.Resources.Memory")
	if err != nil {
		return nil, err
	}

	defaultValues := &DefaultValues{
		ControllerReplicas:               uint(defaults["ControllerReplicas"].(float64)),
		ControllerLogLevel:               defaults["ControllerLogLevel"].(string),
		ControllerUID:                    int64(defaults["ControllerUID"].(float64)),
		EnableExternalProfiles:           proxy["EnableExternalProfiles"].(bool),
		EnableH2Upgrade:                  defaults["EnableH2Upgrade"].(bool),
		ImagePullPolicy:                  defaults["ImagePullPolicy"].(string),
		IdentityTrustDomain:              identity["TrustDomain"].(string),
		IdentityIssuerClockSkewAllowance: identityClockSkewAllowance,
		IdentityIssuerIssuanceLifetime:   identityIssuanceLifetime,
		OmitWebhookSideEffects:           defaults["OmitWebhookSideEffects"].(bool),
		PrometheusImage:                  defaults["PrometheusImage"].(string),
		ProxyAdminPort:                   uint(proxyPorts["Admin"].(float64)),
		ProxyControlPort:                 uint(proxyPorts["Control"].(float64)),
		ProxyCPULimit:                    proxyResourcesCPU["Limit"].(string),
		ProxyCPURequest:                  proxyResourcesCPU["Request"].(string),
		ProxyImageName:                   proxyImage["Name"].(string),
		ProxyInboundPort:                 uint(proxyPorts["Inbound"].(float64)),
		ProxyInitImageName:               proxyInitImage["Name"].(string),
		ProxyInitCPULimit:                proxyInitResourcesCPU["Limit"].(string),
		ProxyInitCPURequest:              proxyInitResourcesCPU["Request"].(string),
		ProxyInitMemoryLimit:             proxyInitResourcesMemory["Limit"].(string),
		ProxyInitMemoryRequest:           proxyInitResourcesMemory["Request"].(string),
		ProxyLogLevel:                    proxy["LogLevel"].(string),
		ProxyMemoryLimit:                 proxyResourcesMemory["Limit"].(string),
		ProxyMemoryRequest:               proxyResourcesMemory["Request"].(string),
		ProxyOutboundPort:                uint(proxyPorts["Outbound"].(float64)),
		ProxyUID:                         int64(proxy["UID"].(float64)),
		WebhookFailurePolicy:             defaults["WebhookFailurePolicy"].(string),
	}

	if ha {
		controllerResourcesCPU, err := defaults.Table("ControllerResources.CPU")
		if err != nil {
			return nil, err
		}

		controllerResourcesMemory, err := defaults.Table("ControllerResources.Memory")
		if err != nil {
			return nil, err
		}

		defaultValues.ControllerCPULimit = controllerResourcesCPU["Limit"].(string)
		defaultValues.ControllerCPURequest = controllerResourcesCPU["Request"].(string)
		defaultValues.ControllerMemoryLimit = controllerResourcesMemory["Limit"].(string)
		defaultValues.ControllerMemoryRequest = controllerResourcesMemory["Request"].(string)

		grafanaResourcesCPU, err := defaults.Table("GrafanaResources.CPU")
		if err != nil {
			return nil, err
		}

		grafanaResourcesMemory, err := defaults.Table("GrafanaResources.Memory")
		if err != nil {
			return nil, err
		}

		defaultValues.GrafanaCPULimit = grafanaResourcesCPU["Limit"].(string)
		defaultValues.GrafanaCPURequest = grafanaResourcesCPU["Request"].(string)
		defaultValues.GrafanaMemoryLimit = grafanaResourcesMemory["Limit"].(string)
		defaultValues.GrafanaMemoryRequest = grafanaResourcesMemory["Request"].(string)

		identityResourcesCPU, err := defaults.Table("IdentityResources.CPU")
		if err != nil {
			return nil, err
		}

		identityResourcesMemory, err := defaults.Table("IdentityResources.Memory")
		if err != nil {
			return nil, err
		}

		defaultValues.IdentityCPULimit = identityResourcesCPU["Limit"].(string)
		defaultValues.IdentityCPURequest = identityResourcesCPU["Request"].(string)
		defaultValues.IdentityMemoryLimit = identityResourcesMemory["Limit"].(string)
		defaultValues.IdentityMemoryRequest = identityResourcesMemory["Request"].(string)

		prometheusResourcesCPU, err := defaults.Table("PrometheusResources.CPU")
		if err != nil {
			return nil, err
		}

		prometheusResourcesMemory, err := defaults.Table("PrometheusResources.Memory")
		if err != nil {
			return nil, err
		}

		defaultValues.PrometheusCPULimit = prometheusResourcesCPU["Limit"].(string)
		defaultValues.PrometheusCPURequest = prometheusResourcesCPU["Request"].(string)
		defaultValues.PrometheusMemoryLimit = prometheusResourcesMemory["Limit"].(string)
		defaultValues.PrometheusMemoryRequest = prometheusResourcesMemory["Request"].(string)
	}

	return defaultValues, nil
}
