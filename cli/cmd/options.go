package cmd

import (
	"context"
	"errors"

	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/cli/flag"
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/issuercerts"
	"github.com/linkerd/linkerd2/pkg/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"
)

// makeInstallUpgradeFlags builds the set of flags which are used during the
// "control-plane" stage of install and upgrade.  These flags control the
// majority of how the control plane is configured.
func makeInstallUpgradeFlags(defaults *l5dcharts.Values) ([]flag.Flag, *pflag.FlagSet, error) {
	installUpgradeFlags := pflag.NewFlagSet("install", pflag.ExitOnError)

	issuanceLifetime, err := time.ParseDuration(defaults.Identity.Issuer.IssuanceLifetime)
	if err != nil {
		return nil, nil, err
	}
	clockSkewAllowance, err := time.ParseDuration(defaults.Identity.Issuer.ClockSkewAllowance)
	if err != nil {
		return nil, nil, err
	}

	flags := []flag.Flag{
		flag.NewUintFlag(installUpgradeFlags, "controller-replicas", defaults.ControllerReplicas,
			"Replicas of the controller to deploy", func(values *l5dcharts.Values, value uint) error {
				values.ControllerReplicas = value
				return nil
			}),

		flag.NewStringFlag(installUpgradeFlags, "controller-log-level", defaults.Global.ControllerLogLevel,
			"Log level for the controller and web components", func(values *l5dcharts.Values, value string) error {
				values.Global.ControllerLogLevel = value
				return nil
			}),

		flag.NewBoolFlag(installUpgradeFlags, "ha", false, "Enable HA deployment config for the control plane (default false)",
			func(values *l5dcharts.Values, value bool) error {
				values.Global.HighAvailability = value
				if value {
					haValues, err := l5dcharts.NewValues(true)
					if err != nil {
						return err
					}
					// The HA flag must be processed before flags that set these values individually so that the
					// individual flags can override the HA defaults.  This means that the HA flag must appear
					// before the individual flags in the slice passed to flags.ApplyIfSet.
					values.ControllerReplicas = haValues.ControllerReplicas
					values.Global.Proxy.Resources.CPU.Request = haValues.Global.Proxy.Resources.CPU.Request
					values.Global.Proxy.Resources.Memory.Request = haValues.Global.Proxy.Resources.Memory.Request
					values.Global.Proxy.Resources.CPU.Limit = haValues.Global.Proxy.Resources.CPU.Limit
					values.Global.Proxy.Resources.Memory.Limit = haValues.Global.Proxy.Resources.Memory.Limit
				}
				return nil
			}),

		flag.NewInt64Flag(installUpgradeFlags, "controller-uid", defaults.ControllerUID,
			"Run the control plane components under this user ID", func(values *l5dcharts.Values, value int64) error {
				values.ControllerUID = value
				return nil
			}),

		flag.NewBoolFlag(installUpgradeFlags, "disable-h2-upgrade", !defaults.EnableH2Upgrade,
			"Prevents the controller from instructing proxies to perform transparent HTTP/2 upgrading (default false)",
			func(values *l5dcharts.Values, value bool) error {
				values.EnableH2Upgrade = !value
				return nil
			}),

		flag.NewBoolFlag(installUpgradeFlags, "disable-heartbeat", defaults.DisableHeartBeat,
			"Disables the heartbeat cronjob (default false)", func(values *l5dcharts.Values, value bool) error {
				values.DisableHeartBeat = value
				return nil
			}),

		flag.NewDurationFlag(installUpgradeFlags, "identity-issuance-lifetime", issuanceLifetime,
			"The amount of time for which the Identity issuer should certify identity",
			func(values *l5dcharts.Values, value time.Duration) error {
				values.Identity.Issuer.IssuanceLifetime = value.String()
				return nil
			}),

		flag.NewDurationFlag(installUpgradeFlags, "identity-clock-skew-allowance", clockSkewAllowance,
			"The amount of time to allow for clock skew within a Linkerd cluster",
			func(values *l5dcharts.Values, value time.Duration) error {
				values.Identity.Issuer.ClockSkewAllowance = value.String()
				return nil
			}),

		flag.NewBoolFlag(installUpgradeFlags, "omit-webhook-side-effects", defaults.OmitWebhookSideEffects,
			"Omit the sideEffects flag in the webhook manifests, This flag must be provided during install or upgrade for Kubernetes versions pre 1.12",
			func(values *l5dcharts.Values, value bool) error {
				values.OmitWebhookSideEffects = value
				return nil
			}),

		flag.NewBoolFlag(installUpgradeFlags, "control-plane-tracing", defaults.Global.ControlPlaneTracing,
			"Enables Control Plane Tracing with the defaults", func(values *l5dcharts.Values, value bool) error {
				defaults.Global.ControlPlaneTracing = value
				return nil
			}),

		flag.NewStringFlag(installUpgradeFlags, "identity-issuer-certificate-file", "",
			"A path to a PEM-encoded file containing the Linkerd Identity issuer certificate (generated by default)",
			func(values *l5dcharts.Values, value string) error {
				if value != "" {
					crt, err := loadCrtPEM(value)
					if err != nil {
						return err
					}
					values.Identity.Issuer.TLS.CrtPEM = crt
				}
				return nil
			}),

		flag.NewStringFlag(installUpgradeFlags, "identity-issuer-key-file", "",
			"A path to a PEM-encoded file containing the Linkerd Identity issuer private key (generated by default)",
			func(values *l5dcharts.Values, value string) error {
				if value != "" {
					key, err := loadKeyPEM(value)
					if err != nil {
						return err
					}
					values.Identity.Issuer.TLS.KeyPEM = key
				}
				return nil
			}),

		flag.NewStringFlag(installUpgradeFlags, "identity-trust-anchors-file", "",
			"A path to a PEM-encoded file containing Linkerd Identity trust anchors (generated by default)",
			func(values *l5dcharts.Values, value string) error {
				if value != "" {
					data, err := ioutil.ReadFile(value)
					if err != nil {
						return err
					}
					values.Global.IdentityTrustAnchorsPEM = string(data)
				}
				return nil
			}),

		flag.NewBoolFlag(installUpgradeFlags, "enable-endpoint-slices", defaults.Global.EnableEndpointSlices,
			"Enables the usage of EndpointSlice informers and resources for destination service",
			func(values *l5dcharts.Values, value bool) error {
				values.Global.EnableEndpointSlices = value
				return nil
			}),

		flag.NewStringFlag(installUpgradeFlags, "control-plane-version", defaults.ControllerImageVersion,
			"Tag to be used for the control plane component images",
			func(values *l5dcharts.Values, value string) error {
				values.ControllerImageVersion = value
				values.Global.ControllerImageVersion = value
				return nil
			}),
	}

	// Hide developer focused flags in release builds.
	release, err := version.IsReleaseChannel(version.Version)
	if err != nil {
		log.Errorf("Unable to parse version: %s", version.Version)
	}
	if release {
		installUpgradeFlags.MarkHidden("control-plane-version")
	}
	installUpgradeFlags.MarkHidden("control-plane-tracing")

	return flags, installUpgradeFlags, nil
}

func loadCrtPEM(path string) (string, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}

	crt, err := tls.DecodePEMCrt(string(data))
	if err != nil {
		return "", err
	}
	return crt.EncodeCertificatePEM(), nil
}

func loadKeyPEM(path string) (string, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}

	key, err := tls.DecodePEMKey(string(data))
	if err != nil {
		return "", err
	}
	cred := tls.Cred{PrivateKey: key}
	return cred.EncodePrivateKeyPEM(), nil
}

// makeAllStageFlags builds the set of flags which are used during all stages
// of install and upgrade.  These flags influence cluster level configuration
// and therefore are available during the "config" stage.
func makeAllStageFlags(defaults *l5dcharts.Values) ([]flag.Flag, *pflag.FlagSet) {

	allStageFlags := pflag.NewFlagSet("all-stage", pflag.ExitOnError)

	flags := []flag.Flag{
		flag.NewBoolFlag(allStageFlags, "linkerd-cni-enabled", defaults.Global.CNIEnabled,
			"Omit the NET_ADMIN capability in the PSP and the proxy-init container when injecting the proxy; requires the linkerd-cni plugin to already be installed",
			func(values *l5dcharts.Values, value bool) error {
				values.Global.CNIEnabled = value
				return nil
			}),

		flag.NewBoolFlag(allStageFlags, "restrict-dashboard-privileges", defaults.RestrictDashboardPrivileges,
			"Restrict the Linkerd Dashboard's default privileges to disallow Tap and Check",
			func(values *l5dcharts.Values, value bool) error {
				values.RestrictDashboardPrivileges = value
				return nil
			}),

		flag.NewStringFlag(allStageFlags, "addon-config", "",
			"A path to a configuration file of add-ons. If add-on config already exists, this new config gets merged with the existing one (unless --addon-overwrite is used)",
			func(values *l5dcharts.Values, value string) error {
				if value != "" {
					data, err := ioutil.ReadFile(value)
					if err != nil {
						return err
					}
					addonConfigs := make(map[string]interface{})
					err = yaml.Unmarshal(data, &addonConfigs)
					if err != nil {
						return err
					}
					if addOnOverwrite {
						if _, ok := addonConfigs["prometheus"]; ok {
							values.Prometheus = make(l5dcharts.Prometheus)
						}
						if _, ok := addonConfigs["grafana"]; ok {
							values.Grafana = make(l5dcharts.Grafana)
						}
						if _, ok := addonConfigs["tracing"]; ok {
							values.Tracing = make(l5dcharts.Tracing)
						}
					}
					err = yaml.Unmarshal(data, &values)
					if err != nil {
						return err
					}
				}
				return nil
			}),
	}

	return flags, allStageFlags
}

// makeInstallFlags builds the set of flags which are used during the
// "control-plane" stage of install.  These flags should not be changed during
// an upgrade and are not available to the upgrade command.
func makeInstallFlags(defaults *l5dcharts.Values) ([]flag.Flag, *pflag.FlagSet) {

	installOnlyFlags := pflag.NewFlagSet("install-only", pflag.ExitOnError)

	flags := []flag.Flag{
		flag.NewStringFlag(installOnlyFlags, "cluster-domain", defaults.Global.ClusterDomain,
			"Set custom cluster domain", func(values *l5dcharts.Values, value string) error {
				values.Global.ClusterDomain = value
				return nil
			}),

		flag.NewStringFlag(installOnlyFlags, "identity-trust-domain", defaults.Global.IdentityTrustDomain,
			"Configures the name suffix used for identities.", func(values *l5dcharts.Values, value string) error {
				values.Global.IdentityTrustDomain = value
				return nil
			}),

		flag.NewBoolFlag(installOnlyFlags, "identity-external-issuer", false,
			"Whether to use an external identity issuer (default false)", func(values *l5dcharts.Values, value bool) error {
				if value {
					values.Identity.Issuer.Scheme = string(corev1.SecretTypeTLS)
				} else {
					values.Identity.Issuer.Scheme = consts.IdentityIssuerSchemeLinkerd
				}
				return nil
			}),
	}

	return flags, installOnlyFlags
}

// makeProxyFlags builds the set of flags which affect how the proxy is
// configured.  These flags are available to the inject command and to the
// install and upgrade commands in the "control-plane" stage.
func makeProxyFlags(defaults *l5dcharts.Values) ([]flag.Flag, *pflag.FlagSet) {

	proxyFlags := pflag.NewFlagSet("proxy", pflag.ExitOnError)

	flags := []flag.Flag{
		flag.NewStringFlagP(proxyFlags, "proxy-version", "v", defaults.Global.Proxy.Image.Version, "Tag to be used for the Linkerd proxy images",
			func(values *l5dcharts.Values, value string) error {
				values.Global.Proxy.Image.Version = value
				return nil
			}),

		flag.NewStringFlag(proxyFlags, "proxy-image", defaults.Global.Proxy.Image.Name, "Linkerd proxy container image name",
			func(values *l5dcharts.Values, value string) error {
				values.Global.Proxy.Image.Name = value
				return nil
			}),

		flag.NewStringFlag(proxyFlags, "init-image", defaults.Global.ProxyInit.Image.Name, "Linkerd init container image name",
			func(values *l5dcharts.Values, value string) error {
				values.Global.ProxyInit.Image.Name = value
				return nil
			}),

		flag.NewStringFlag(proxyFlags, "init-image-version", defaults.Global.ProxyInit.Image.Version,
			"Linkerd init container image version", func(values *l5dcharts.Values, value string) error {
				values.Global.ProxyInit.Image.Version = value
				return nil
			}),

		flag.NewStringFlag(proxyFlags, "registry", defaultDockerRegistry, "Docker registry to pull images from",
			func(values *l5dcharts.Values, value string) error {
				values.WebImage = registryOverride(values.WebImage, value)
				values.ControllerImage = registryOverride(values.ControllerImage, value)
				values.DebugContainer.Image.Name = registryOverride(values.DebugContainer.Image.Name, value)
				values.Global.Proxy.Image.Name = registryOverride(values.Global.Proxy.Image.Name, value)
				values.DebugContainer.Image.Name = registryOverride(values.DebugContainer.Image.Name, value)
				values.Global.ProxyInit.Image.Name = registryOverride(values.Global.ProxyInit.Image.Name, value)
				return nil
			}),

		flag.NewStringFlag(proxyFlags, "image-pull-policy", defaults.Global.ImagePullPolicy,
			"Docker image pull policy", func(values *l5dcharts.Values, value string) error {
				values.Global.ImagePullPolicy = value
				return nil
			}),

		flag.NewUintFlag(proxyFlags, "inbound-port", uint(defaults.Global.Proxy.Ports.Inbound),
			"Proxy port to use for inbound traffic", func(values *l5dcharts.Values, value uint) error {
				values.Global.Proxy.Ports.Inbound = int32(value)
				return nil
			}),

		flag.NewUintFlag(proxyFlags, "outbound-port", uint(defaults.Global.Proxy.Ports.Outbound),
			"Proxy port to use for outbound traffic", func(values *l5dcharts.Values, value uint) error {
				values.Global.Proxy.Ports.Outbound = int32(value)
				return nil
			}),

		flag.NewStringSliceFlag(proxyFlags, "skip-inbound-ports", nil, "Ports and/or port ranges (inclusive) that should skip the proxy and send directly to the application",
			func(values *l5dcharts.Values, value []string) error {
				values.Global.ProxyInit.IgnoreInboundPorts = strings.Join(value, ",")
				return nil
			}),

		flag.NewStringSliceFlag(proxyFlags, "skip-outbound-ports", nil, "Outbound ports and/or port ranges (inclusive) that should skip the proxy",
			func(values *l5dcharts.Values, value []string) error {
				values.Global.ProxyInit.IgnoreOutboundPorts = strings.Join(value, ",")
				return nil
			}),

		flag.NewInt64Flag(proxyFlags, "proxy-uid", defaults.Global.Proxy.UID, "Run the proxy under this user ID",
			func(values *l5dcharts.Values, value int64) error {
				values.Global.Proxy.UID = value
				return nil
			}),

		flag.NewStringFlag(proxyFlags, "proxy-log-level", defaults.Global.Proxy.LogLevel, "Log level for the proxy",
			func(values *l5dcharts.Values, value string) error {
				values.Global.Proxy.LogLevel = value
				return nil
			}),

		flag.NewUintFlag(proxyFlags, "control-port", uint(defaults.Global.Proxy.Ports.Control), "Proxy port to use for control",
			func(values *l5dcharts.Values, value uint) error {
				values.Global.Proxy.Ports.Control = int32(value)
				return nil
			}),

		flag.NewUintFlag(proxyFlags, "admin-port", uint(defaults.Global.Proxy.Ports.Admin), "Proxy port to serve metrics on",
			func(values *l5dcharts.Values, value uint) error {
				values.Global.Proxy.Ports.Admin = int32(value)
				return nil
			}),

		flag.NewStringFlag(proxyFlags, "proxy-cpu-request", defaults.Global.Proxy.Resources.CPU.Request, "Amount of CPU units that the proxy sidecar requests",
			func(values *l5dcharts.Values, value string) error {
				values.Global.Proxy.Resources.CPU.Request = value
				return nil
			}),

		flag.NewStringFlag(proxyFlags, "proxy-memory-request", defaults.Global.Proxy.Resources.Memory.Request, "Amount of Memory that the proxy sidecar requests",
			func(values *l5dcharts.Values, value string) error {
				values.Global.Proxy.Resources.Memory.Request = value
				return nil
			}),

		flag.NewStringFlag(proxyFlags, "proxy-cpu-limit", defaults.Global.Proxy.Resources.CPU.Limit, "Maximum amount of CPU units that the proxy sidecar can use",
			func(values *l5dcharts.Values, value string) error {
				values.Global.Proxy.Resources.CPU.Limit = value
				return nil
			}),

		flag.NewStringFlag(proxyFlags, "proxy-memory-limit", defaults.Global.Proxy.Resources.Memory.Limit, "Maximum amount of Memory that the proxy sidecar can use",
			func(values *l5dcharts.Values, value string) error {
				values.Global.Proxy.Resources.Memory.Limit = value
				return nil
			}),

		flag.NewBoolFlag(proxyFlags, "enable-external-profiles", defaults.Global.Proxy.EnableExternalProfiles, "Enable service profiles for non-Kubernetes services",
			func(values *l5dcharts.Values, value bool) error {
				values.Global.Proxy.EnableExternalProfiles = value
				return nil
			}),

		// Deprecated flags

		flag.NewStringFlag(proxyFlags, "proxy-memory", defaults.Global.Proxy.Resources.Memory.Request, "Amount of Memory that the proxy sidecar requests",
			func(values *l5dcharts.Values, value string) error {
				values.Global.Proxy.Resources.Memory.Request = value
				return nil
			}),

		flag.NewStringFlag(proxyFlags, "proxy-cpu", defaults.Global.Proxy.Resources.CPU.Request, "Amount of CPU units that the proxy sidecar requests",
			func(values *l5dcharts.Values, value string) error {
				values.Global.Proxy.Resources.CPU.Request = value
				return nil
			}),
	}

	proxyFlags.MarkDeprecated("proxy-memory", "use --proxy-memory-request instead")
	proxyFlags.MarkDeprecated("proxy-cpu", "use --proxy-cpu-request instead")

	// Hide developer focused flags in release builds.
	release, err := version.IsReleaseChannel(version.Version)
	if err != nil {
		log.Errorf("Unable to parse version: %s", version.Version)
	}
	if release {
		proxyFlags.MarkHidden("proxy-image")
		proxyFlags.MarkHidden("proxy-version")
		proxyFlags.MarkHidden("image-pull-policy")
		proxyFlags.MarkHidden("init-image")
		proxyFlags.MarkHidden("init-image-version")
	}

	return flags, proxyFlags
}

// makeInjectFlags builds the set of flags which are exclusive to the inject
// command.  These flags configure the proxy but are not available to the
// install and upgrade commands.  This is generally for proxy configuration
// which is intended to be set on individual workloads rather than being
// cluster wide.
func makeInjectFlags(defaults *l5dcharts.Values) ([]flag.Flag, *pflag.FlagSet) {
	injectFlags := pflag.NewFlagSet("inject", pflag.ExitOnError)

	flags := []flag.Flag{
		flag.NewInt64Flag(injectFlags, "wait-before-exit-seconds", int64(defaults.Global.Proxy.WaitBeforeExitSeconds),
			"The period during which the proxy sidecar must stay alive while its pod is terminating. "+
				"Must be smaller than terminationGracePeriodSeconds for the pod (default 0)",
			func(values *l5dcharts.Values, value int64) error {
				values.Global.Proxy.WaitBeforeExitSeconds = uint64(value)
				return nil
			}),

		flag.NewBoolFlag(injectFlags, "disable-identity", defaults.Global.Proxy.DisableIdentity,
			"Disables resources from participating in TLS identity", func(values *l5dcharts.Values, value bool) error {
				values.Global.Proxy.DisableIdentity = value
				return nil
			}),

		flag.NewBoolFlag(injectFlags, "disable-tap", defaults.Global.Proxy.DisableTap,
			"Disables resources from being tapped", func(values *l5dcharts.Values, value bool) error {
				values.Global.Proxy.DisableTap = value
				return nil
			}),

		flag.NewStringFlag(injectFlags, "trace-collector", defaults.Global.Proxy.Trace.CollectorSvcAddr,
			"Collector Service address for the proxies to send Trace Data", func(values *l5dcharts.Values, value string) error {
				values.Global.Proxy.Trace.CollectorSvcAddr = value
				return nil
			}),

		flag.NewStringFlag(injectFlags, "trace-collector-svc-account", defaults.Global.Proxy.Trace.CollectorSvcAccount,
			"Service account associated with the Trace collector instance", func(values *l5dcharts.Values, value string) error {
				values.Global.Proxy.Trace.CollectorSvcAccount = value
				return nil
			}),

		flag.NewStringSliceFlag(injectFlags, "require-identity-on-inbound-ports", strings.Split(defaults.Global.Proxy.RequireIdentityOnInboundPorts, ","),
			"Inbound ports on which the proxy should require identity", func(values *l5dcharts.Values, value []string) error {
				values.Global.Proxy.RequireIdentityOnInboundPorts = strings.Join(value, ",")
				return nil
			}),
	}

	return flags, injectFlags
}

/* Validation */

func validateValues(ctx context.Context, k *k8s.KubernetesAPI, values *l5dcharts.Values) error {
	if !alphaNumDashDot.MatchString(values.ControllerImageVersion) {
		return fmt.Errorf("%s is not a valid version", values.ControllerImageVersion)
	}

	if _, err := log.ParseLevel(values.Global.ControllerLogLevel); err != nil {
		return fmt.Errorf("--controller-log-level must be one of: panic, fatal, error, warn, info, debug")
	}

	if values.Global.Proxy.LogLevel == "" {
		return errors.New("--proxy-log-level must not be empty")
	}

	if values.Global.EnableEndpointSlices && k != nil {
		k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
		if err != nil {
			return err
		}

		err = k8s.EndpointSliceAccess(ctx, k8sAPI)
		if err != nil {
			return err
		}
	}

	if errs := validation.IsDNS1123Subdomain(values.Global.IdentityTrustDomain); len(errs) > 0 {
		return fmt.Errorf("invalid trust domain '%s': %s", values.Global.IdentityTrustDomain, errs[0])
	}

	err := validateProxyValues(values)
	if err != nil {
		return err
	}

	if values.Identity.Issuer.Scheme == string(corev1.SecretTypeTLS) {
		if values.Identity.Issuer.TLS.CrtPEM != "" {
			return errors.New("--identity-issuer-certificate-file must not be specified if --identity-external-issuer=true")
		}
		if values.Identity.Issuer.TLS.KeyPEM != "" {
			return errors.New("--identity-issuer-key-file must not be specified if --identity-external-issuer=true")
		}
	}

	if values.Identity.Issuer.Scheme == string(corev1.SecretTypeTLS) && k != nil {
		externalIssuerData, err := issuercerts.FetchExternalIssuerData(ctx, k, controlPlaneNamespace)
		if err != nil {
			return err
		}
		_, err = externalIssuerData.VerifyAndBuildCreds(issuerName(values.Global.IdentityTrustDomain))
		if err != nil {
			return fmt.Errorf("failed to validate issuer credentials: %s", err)
		}
	}

	if values.Identity.Issuer.Scheme == consts.IdentityIssuerSchemeLinkerd {
		issuerData := issuercerts.IssuerCertData{
			IssuerCrt:    values.Identity.Issuer.TLS.CrtPEM,
			IssuerKey:    values.Identity.Issuer.TLS.KeyPEM,
			TrustAnchors: values.Global.IdentityTrustAnchorsPEM,
		}
		_, err := issuerData.VerifyAndBuildCreds(issuerName(values.Global.IdentityTrustDomain))
		if err != nil {
			return fmt.Errorf("failed to validate issuer credentials: %s", err)
		}
	}

	return nil
}

func validateProxyValues(values *l5dcharts.Values) error {
	networks := strings.Split(values.Global.Proxy.DestinationGetNetworks, ",")
	for _, network := range networks {
		if _, _, err := net.ParseCIDR(network); err != nil {
			return fmt.Errorf("cannot parse destination get networks: %s", err)
		}
	}

	if values.Global.Proxy.DisableIdentity && len(values.Global.Proxy.RequireIdentityOnInboundPorts) > 0 {
		return errors.New("Identity must be enabled when  --require-identity-on-inbound-ports is specified")
	}

	if values.Global.Proxy.Image.Version != "" && !alphaNumDashDot.MatchString(values.Global.Proxy.Image.Version) {
		return fmt.Errorf("%s is not a valid version", values.Global.Proxy.Image.Version)
	}

	if !alphaNumDashDot.MatchString(values.Global.ProxyInit.Image.Version) {
		return fmt.Errorf("%s is not a valid version", values.Global.ProxyInit.Image.Version)
	}

	if values.Global.ImagePullPolicy != "Always" && values.Global.ImagePullPolicy != "IfNotPresent" && values.Global.ImagePullPolicy != "Never" {
		return fmt.Errorf("--image-pull-policy must be one of: Always, IfNotPresent, Never")
	}

	if values.Global.Proxy.Resources.CPU.Request != "" {
		if _, err := k8sResource.ParseQuantity(values.Global.Proxy.Resources.CPU.Request); err != nil {
			return fmt.Errorf("Invalid cpu request '%s' for --proxy-cpu-request flag", values.Global.Proxy.Resources.CPU.Request)
		}
	}

	if values.Global.Proxy.Resources.Memory.Request != "" {
		if _, err := k8sResource.ParseQuantity(values.Global.Proxy.Resources.Memory.Request); err != nil {
			return fmt.Errorf("Invalid memory request '%s' for --proxy-memory-request flag", values.Global.Proxy.Resources.Memory.Request)
		}
	}

	if values.Global.Proxy.Resources.CPU.Limit != "" {
		cpuLimit, err := k8sResource.ParseQuantity(values.Global.Proxy.Resources.CPU.Limit)
		if err != nil {
			return fmt.Errorf("Invalid cpu limit '%s' for --proxy-cpu-limit flag", values.Global.Proxy.Resources.CPU.Limit)
		}
		// Not checking for error because option proxyCPURequest was already validated
		if cpuRequest, _ := k8sResource.ParseQuantity(values.Global.Proxy.Resources.CPU.Request); cpuRequest.MilliValue() > cpuLimit.MilliValue() {
			return fmt.Errorf("The cpu limit '%s' cannot be lower than the cpu request '%s'", values.Global.Proxy.Resources.CPU.Limit, values.Global.Proxy.Resources.CPU.Request)
		}
	}

	if values.Global.Proxy.Resources.Memory.Limit != "" {
		memoryLimit, err := k8sResource.ParseQuantity(values.Global.Proxy.Resources.Memory.Limit)
		if err != nil {
			return fmt.Errorf("Invalid memory limit '%s' for --proxy-memory-limit flag", values.Global.Proxy.Resources.Memory.Limit)
		}
		// Not checking for error because option proxyMemoryRequest was already validated
		if memoryRequest, _ := k8sResource.ParseQuantity(values.Global.Proxy.Resources.Memory.Request); memoryRequest.Value() > memoryLimit.Value() {
			return fmt.Errorf("The memory limit '%s' cannot be lower than the memory request '%s'", values.Global.Proxy.Resources.Memory.Limit, values.Global.Proxy.Resources.Memory.Request)
		}
	}

	if !validProxyLogLevel.MatchString(values.Global.Proxy.LogLevel) {
		return fmt.Errorf("\"%s\" is not a valid proxy log level - for allowed syntax check https://docs.rs/env_logger/0.6.0/env_logger/#enabling-logging",
			values.Global.Proxy.LogLevel)
	}

	if values.Global.ProxyInit.IgnoreInboundPorts != "" {
		if err := validateRangeSlice(strings.Split(values.Global.ProxyInit.IgnoreInboundPorts, ",")); err != nil {
			return err
		}
	}

	if values.Global.ProxyInit.IgnoreOutboundPorts != "" {
		if err := validateRangeSlice(strings.Split(values.Global.ProxyInit.IgnoreOutboundPorts, ",")); err != nil {
			return err
		}
	}

	return nil
}

// initializeIssuerCredentials populates the identity issuer TLS credentials.
// If we are using an externally managed issuer secret, all we need to do here
// is copy the trust root from the issuer secret.  Otherwise, if no credentials
// have already been supplied, we generate them.
func initializeIssuerCredentials(ctx context.Context, k *k8s.KubernetesAPI, values *l5dcharts.Values) error {
	if values.Identity.Issuer.Scheme == string(corev1.SecretTypeTLS) {
		// Using externally managed issuer credentials.  We need to copy the
		// trust root.
		if k == nil {
			return errors.New("--ignore-cluster is not supported when --identity-external-issuer=true")
		}
		externalIssuerData, err := issuercerts.FetchExternalIssuerData(ctx, k, controlPlaneNamespace)
		if err != nil {
			return err
		}
		values.Global.IdentityTrustAnchorsPEM = externalIssuerData.TrustAnchors
	} else if values.Identity.Issuer.TLS.CrtPEM != "" || values.Identity.Issuer.TLS.KeyPEM != "" || values.Global.IdentityTrustAnchorsPEM != "" {
		// If any credentials have already been supplied, check that they are
		// all present.
		if values.Global.IdentityTrustAnchorsPEM == "" {
			return errors.New("a trust anchors file must be specified if other credentials are provided")
		}
		if values.Identity.Issuer.TLS.CrtPEM == "" {
			return errors.New("a certificate file must be specified if other credentials are provided")
		}
		if values.Identity.Issuer.TLS.KeyPEM == "" {
			return errors.New("a private key file must be specified if other credentials are provided")
		}
	} else {
		// No credentials have been supplied so we will generate them.
		root, err := tls.GenerateRootCAWithDefaults(issuerName(values.Global.IdentityTrustDomain))
		if err != nil {
			return fmt.Errorf("failed to generate root certificate for identity: %s", err)
		}
		values.Identity.Issuer.CrtExpiry = root.Cred.Crt.Certificate.NotAfter
		values.Identity.Issuer.TLS.KeyPEM = root.Cred.EncodePrivateKeyPEM()
		values.Identity.Issuer.TLS.CrtPEM = root.Cred.Crt.EncodeCertificatePEM()
		values.Global.IdentityTrustAnchorsPEM = root.Cred.Crt.EncodeCertificatePEM()
	}
	return nil
}

func issuerName(trustDomain string) string {
	return fmt.Sprintf("identity.%s.%s", controlPlaneNamespace, trustDomain)
}
