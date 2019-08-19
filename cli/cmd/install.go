package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/google/uuid"
	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

type (
	installValues struct {
		stage                       string
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
		WebResources *charts.Resources

		Identity         *installIdentityValues
		ProxyInjector    *charts.ProxyInjector
		ProfileValidator *charts.ProfileValidator
		Tap              *charts.Tap
		Proxy            *charts.Proxy
		ProxyInit        *charts.ProxyInit
	}

	configJSONs struct{ Global, Proxy, Install string }

	installIdentityValues charts.Identity

	// installOptions holds values for command line flags that apply to the install
	// command. All fields in this struct should have corresponding flags added in
	// the newCmdInstall func later in this file. It also embeds proxyConfigOptions
	// in order to hold values for command line flags that apply to both inject and
	// install.
	installOptions struct {
		controlPlaneVersion         string
		controllerReplicas          uint
		controllerLogLevel          string
		highAvailability            bool
		controllerUID               int64
		disableH2Upgrade            bool
		noInitContainer             bool
		skipChecks                  bool
		omitWebhookSideEffects      bool
		restrictDashboardPrivileges bool
		identityOptions             *installIdentityOptions
		*proxyConfigOptions

		recordedFlags []*pb.Install_Flag

		// function pointers that can be overridden for tests
		generateUUID      func() string
		heartbeatSchedule func() string
	}

	installIdentityOptions struct {
		replicas    uint
		trustDomain string

		issuanceLifetime   time.Duration
		clockSkewAllowance time.Duration

		trustPEMFile, crtPEMFile, keyPEMFile string
	}
)

const (
	configStage       = "config"
	controlPlaneStage = "control-plane"

	defaultIdentityIssuanceLifetime   = 24 * time.Hour
	defaultIdentityClockSkewAllowance = 20 * time.Second

	helmDefaultChartName = "linkerd2"
	helmDefaultChartDir  = "linkerd2"

	errMsgGlobalResourcesExist = `Unable to install the Linkerd control plane. It appears that there is an existing installation:

%s

If you are sure you'd like to have a fresh install, remove these resources with:

    linkerd install --ignore-cluster | kubectl delete -f -

Otherwise, you can use the --ignore-cluster flag to overwrite the existing global resources.
`

	errMsgLinkerdConfigConfigMapNotFound = "Can't install the Linkerd control plane in the '%s' namespace. Reason: %s.\nIf this is expected, use the --ignore-cluster flag to continue the installation.\n"
	errMsgGlobalResourcesMissing         = "Can't install the Linkerd control plane in the '%s' namespace. The required Linkerd global resources are missing.\nIf this is expected, use the --skip-checks flag to continue the installation.\n"
)

// newInstallOptionsWithDefaults initializes install options with default
// control plane and proxy options.
//
// These options may be overridden on the CLI at install-time and will be
// persisted in Linkerd's control plane configuration to be used at
// injection-time.
func newInstallOptionsWithDefaults() (*installOptions, error) {
	chartDir := fmt.Sprintf("%s/", helmDefaultChartDir)
	defaults, err := charts.ReadDefaults(chartDir, false)
	if err != nil {
		return nil, err
	}

	return &installOptions{
		controlPlaneVersion:         version.Version,
		controllerReplicas:          defaults.ControllerReplicas,
		controllerLogLevel:          defaults.ControllerLogLevel,
		highAvailability:            false,
		controllerUID:               defaults.ControllerUID,
		disableH2Upgrade:            !defaults.EnableH2Upgrade,
		noInitContainer:             false,
		omitWebhookSideEffects:      defaults.OmitWebhookSideEffects,
		restrictDashboardPrivileges: false,
		proxyConfigOptions: &proxyConfigOptions{
			proxyVersion:           version.Version,
			ignoreCluster:          false,
			proxyImage:             defaults.ProxyImageName,
			initImage:              defaults.ProxyInitImageName,
			initImageVersion:       version.ProxyInitVersion,
			dockerRegistry:         defaultDockerRegistry,
			imagePullPolicy:        defaults.ImagePullPolicy,
			ignoreInboundPorts:     nil,
			ignoreOutboundPorts:    nil,
			proxyUID:               defaults.ProxyUID,
			proxyLogLevel:          defaults.ProxyLogLevel,
			proxyControlPort:       defaults.ProxyControlPort,
			proxyAdminPort:         defaults.ProxyAdminPort,
			proxyInboundPort:       defaults.ProxyInboundPort,
			proxyOutboundPort:      defaults.ProxyOutboundPort,
			proxyCPURequest:        defaults.ProxyCPURequest,
			proxyMemoryRequest:     defaults.ProxyMemoryRequest,
			proxyCPULimit:          defaults.ProxyCPULimit,
			proxyMemoryLimit:       defaults.ProxyMemoryLimit,
			enableExternalProfiles: defaults.EnableExternalProfiles,
		},
		identityOptions: &installIdentityOptions{
			trustDomain:        defaults.IdentityTrustDomain,
			issuanceLifetime:   defaults.IdentityIssuerIssuanceLifetime,
			clockSkewAllowance: defaults.IdentityIssuerClockSkewAllowance,
		},

		generateUUID: func() string {
			id, err := uuid.NewRandom()
			if err != nil {
				log.Fatalf("Could not generate UUID: %s", err)
			}
			return id.String()
		},

		heartbeatSchedule: func() string {
			// Some of the heartbeat Prometheus queries rely on 5m resolution, which
			// means at least 5 minutes of data available. Start the first CronJob 10
			// minutes after `linkerd install` is run, to give the user 5 minutes to
			// install.
			t := time.Now().Add(10 * time.Minute).UTC()
			return fmt.Sprintf("%d %d * * * ", t.Minute(), t.Hour())
		},
	}, nil
}

// Flag configuration matrix
//
//                                 | recordableFlagSet | allStageFlagSet | installOnlyFlagSet | installPersistentFlagSet | upgradeOnlyFlagSet | "skip-checks" |
// `linkerd install`               |        X          |       X         |         X          |            X             |                    |               |
// `linkerd install config`        |                   |       X         |                    |            X             |                    |               |
// `linkerd install control-plane` |        X          |       X         |         X          |            X             |                    |       X       |
// `linkerd upgrade`               |        X          |       X         |                    |                          |          X         |               |
// `linkerd upgrade config`        |                   |       X         |                    |                          |                    |               |
// `linkerd upgrade control-plane` |        X          |       X         |                    |                          |          X         |               |
//
// allStageFlagSet is a subset of recordableFlagSet, but is also added to `linkerd [install|upgrade] config`
// proxyConfigOptions.flagSet is a subset of recordableFlagSet, and is used by `linkerd inject`.

// newCmdInstallConfig is a subcommand for `linkerd install config`
func newCmdInstallConfig(options *installOptions, parentFlags *pflag.FlagSet) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes cluster-wide resources to install Linkerd",
		Long: `Output Kubernetes cluster-wide resources to install Linkerd.

This command provides Kubernetes configs necessary to install cluster-wide
resources for the Linkerd control plane. This command should be followed by
"linkerd install control-plane".`,
		Example: `  # Default install.
  linkerd install config | kubectl apply -f -

  # Install Linkerd into a non-default namespace.
  linkerd install config -l linkerdtest | kubectl apply -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := errIfGlobalResourcesExist(options); err != nil && !options.ignoreCluster {
				fmt.Fprintf(os.Stderr, errMsgGlobalResourcesExist, err)
				os.Exit(1)
			}

			return installRunE(options, configStage, parentFlags)
		},
	}

	cmd.Flags().AddFlagSet(options.allStageFlagSet())

	return cmd
}

// newCmdInstallControlPlane is a subcommand for `linkerd install control-plane`
func newCmdInstallControlPlane(options *installOptions) *cobra.Command {
	// The base flags are recorded separately so that they can be serialized into
	// the configuration in validateAndBuild.
	flags := options.recordableFlagSet()
	installOnlyFlags := options.installOnlyFlagSet()

	cmd := &cobra.Command{
		Use:   "control-plane [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes control plane resources to install Linkerd",
		Long: `Output Kubernetes control plane resources to install Linkerd.

This command provides Kubernetes configs necessary to install the Linkerd
control plane. It should be run after "linkerd install config".`,
		Example: `  # Default install.
  linkerd install control-plane | kubectl apply -f -

  # Install Linkerd into a non-default namespace.
  linkerd install control-plane -l linkerdtest | kubectl apply -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// check if global resources exist to determine if the `install config`
			// stage succeeded
			if err := errIfGlobalResourcesExist(options); err == nil && !options.skipChecks {
				fmt.Fprintf(os.Stderr, errMsgGlobalResourcesMissing, controlPlaneNamespace)
				os.Exit(1)
			}

			if err := errIfLinkerdConfigConfigMapExists(); err != nil && !options.ignoreCluster {
				fmt.Fprintf(os.Stderr, errMsgLinkerdConfigConfigMapNotFound, controlPlaneNamespace, err.Error())
				os.Exit(1)
			}

			return installRunE(options, controlPlaneStage, flags)
		},
	}

	cmd.PersistentFlags().BoolVar(
		&options.skipChecks, "skip-checks", options.skipChecks,
		`Skip checks for namespace existence`,
	)
	cmd.PersistentFlags().AddFlagSet(flags)
	// Some flags are not available during upgrade, etc.
	cmd.PersistentFlags().AddFlagSet(installOnlyFlags)

	return cmd
}

func newCmdInstall() *cobra.Command {
	options, err := newInstallOptionsWithDefaults()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s", err)
		os.Exit(1)
	}

	// The base flags are recorded separately so that they can be serialized into
	// the configuration in validateAndBuild.
	flags := options.recordableFlagSet()
	installOnlyFlags := options.installOnlyFlagSet()
	installPersistentFlags := options.installPersistentFlagSet()

	cmd := &cobra.Command{
		Use:   "install [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes configs to install Linkerd",
		Long: `Output Kubernetes configs to install Linkerd.

This command provides all Kubernetes configs necessary to install the Linkerd
control plane.`,
		Example: `  # Default install.
  linkerd install | kubectl apply -f -

  # Install Linkerd into a non-default namespace.
  linkerd install -l linkerdtest | kubectl apply -f -

  # Installation may also be broken up into two stages by user privilege, via
  # subcommands.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := errIfGlobalResourcesExist(options); err != nil && !options.ignoreCluster {
				fmt.Fprintf(os.Stderr, errMsgGlobalResourcesExist, err)
				os.Exit(1)
			}

			return installRunE(options, "", flags)
		},
	}

	cmd.Flags().AddFlagSet(flags)

	// Some flags are not available during upgrade, etc.
	cmd.Flags().AddFlagSet(installOnlyFlags)
	cmd.PersistentFlags().AddFlagSet(installPersistentFlags)

	cmd.AddCommand(newCmdInstallConfig(options, flags))
	cmd.AddCommand(newCmdInstallControlPlane(options))

	return cmd
}

func installRunE(options *installOptions, stage string, flags *pflag.FlagSet) error {
	values, configs, err := options.validateAndBuild(stage, flags)
	if err != nil {
		return err
	}

	return values.render(os.Stdout, configs)
}

func (options *installOptions) validateAndBuild(stage string, flags *pflag.FlagSet) (*installValues, *pb.All, error) {
	if err := options.validate(); err != nil {
		return nil, nil, err
	}

	options.recordFlags(flags)

	identityValues, err := options.identityOptions.validateAndBuild()
	if err != nil {
		return nil, nil, err
	}

	configs := options.configs(identityValues.toIdentityContext())

	values, err := options.buildValuesWithoutIdentity(configs)
	if err != nil {
		return nil, nil, err
	}
	values.Identity = identityValues

	values.stage = stage

	return values, configs, nil
}

// recordableFlagSet returns flags usable during install or upgrade.
func (options *installOptions) recordableFlagSet() *pflag.FlagSet {
	e := pflag.ExitOnError

	flags := pflag.NewFlagSet("install", e)

	flags.AddFlagSet(options.proxyConfigOptions.flagSet(e))
	flags.AddFlagSet(options.allStageFlagSet())

	flags.UintVar(
		&options.controllerReplicas, "controller-replicas", options.controllerReplicas,
		"Replicas of the controller to deploy",
	)

	flags.StringVar(
		&options.controllerLogLevel, "controller-log-level", options.controllerLogLevel,
		"Log level for the controller and web components",
	)
	flags.BoolVar(
		&options.highAvailability, "ha", options.highAvailability,
		"Enable HA deployment config for the control plane (default false)",
	)
	flags.Int64Var(
		&options.controllerUID, "controller-uid", options.controllerUID,
		"Run the control plane components under this user ID",
	)
	flags.BoolVar(
		&options.disableH2Upgrade, "disable-h2-upgrade", options.disableH2Upgrade,
		"Prevents the controller from instructing proxies to perform transparent HTTP/2 upgrading (default false)",
	)
	flags.DurationVar(
		&options.identityOptions.issuanceLifetime, "identity-issuance-lifetime", options.identityOptions.issuanceLifetime,
		"The amount of time for which the Identity issuer should certify identity",
	)
	flags.DurationVar(
		&options.identityOptions.clockSkewAllowance, "identity-clock-skew-allowance", options.identityOptions.clockSkewAllowance,
		"The amount of time to allow for clock skew within a Linkerd cluster",
	)
	flags.BoolVar(
		&options.omitWebhookSideEffects, "omit-webhook-side-effects", options.omitWebhookSideEffects,
		"Omit the sideEffects flag in the webhook manifests, This flag must be provided during install or upgrade for Kubernetes versions pre 1.12",
	)

	flags.StringVarP(&options.controlPlaneVersion, "control-plane-version", "", options.controlPlaneVersion, "(Development) Tag to be used for the control plane component images")
	flags.MarkHidden("control-plane-version")

	return flags
}

// allStageFlagSet returns flags usable for single and multi-stage  installs and
// upgrades. For multi-stage installs, users must set these flags consistently
// across commands.
func (options *installOptions) allStageFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("all-stage", pflag.ExitOnError)

	flags.BoolVar(&options.noInitContainer, "linkerd-cni-enabled", options.noInitContainer,
		"Experimental: Omit the NET_ADMIN capability in the PSP and the proxy-init container when injecting the proxy; requires the linkerd-cni plugin to already be installed",
	)

	flags.BoolVar(
		&options.restrictDashboardPrivileges, "restrict-dashboard-privileges", options.restrictDashboardPrivileges,
		"Restrict the Linkerd Dashboard's default privileges to disallow Tap",
	)

	return flags
}

// installOnlyFlagSet includes flags that are only accessible at install-time
// and not at upgrade-time.
func (options *installOptions) installOnlyFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("install-only", pflag.ExitOnError)

	flags.StringVar(
		&options.identityOptions.trustDomain, "identity-trust-domain", options.identityOptions.trustDomain,
		"Configures the name suffix used for identities.",
	)
	flags.StringVar(
		&options.identityOptions.trustPEMFile, "identity-trust-anchors-file", options.identityOptions.trustPEMFile,
		"A path to a PEM-encoded file containing Linkerd Identity trust anchors (generated by default)",
	)
	flags.StringVar(
		&options.identityOptions.crtPEMFile, "identity-issuer-certificate-file", options.identityOptions.crtPEMFile,
		"A path to a PEM-encoded file containing the Linkerd Identity issuer certificate (generated by default)",
	)
	flags.StringVar(
		&options.identityOptions.keyPEMFile, "identity-issuer-key-file", options.identityOptions.keyPEMFile,
		"A path to a PEM-encoded file containing the Linkerd Identity issuer private key (generated by default)",
	)

	return flags
}

// installPersistentFlagSet includes flags that are only accessible at
// install-time, not at upgrade-time, and are also used by install subcommands.
func (options *installOptions) installPersistentFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("install-persist", pflag.ExitOnError)

	flags.BoolVar(
		&options.ignoreCluster, "ignore-cluster", options.ignoreCluster,
		"Ignore the current Kubernetes cluster when checking for existing cluster configuration (default false)",
	)

	return flags
}

func (options *installOptions) recordFlags(flags *pflag.FlagSet) {
	if flags == nil {
		return
	}

	flags.VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			switch f.Name {
			case "ignore-cluster", "control-plane-version", "proxy-version":
				// These flags don't make sense to record.
			default:
				options.recordedFlags = append(options.recordedFlags, &pb.Install_Flag{
					Name:  f.Name,
					Value: f.Value.String(),
				})
			}
		}
	})
}

func (options *installOptions) validate() error {
	if options.controlPlaneVersion != "" && !alphaNumDashDot.MatchString(options.controlPlaneVersion) {
		return fmt.Errorf("%s is not a valid version", options.controlPlaneVersion)
	}

	if options.identityOptions == nil {
		// Programmer error: identityOptions may be empty, but it must be set by the constructor.
		panic("missing identity options")
	}

	if _, err := log.ParseLevel(options.controllerLogLevel); err != nil {
		return fmt.Errorf("--controller-log-level must be one of: panic, fatal, error, warn, info, debug")
	}

	if err := options.proxyConfigOptions.validate(); err != nil {
		return err
	}
	if options.proxyLogLevel == "" {
		return errors.New("--proxy-log-level must not be empty")
	}

	return nil
}

func (options *installOptions) buildValuesWithoutIdentity(configs *pb.All) (*installValues, error) {
	// install values that can't be overridden by CLI options will be assigned
	// defaults from the values.yaml and values-ha.yaml files
	chartDir := fmt.Sprintf("%s/", helmDefaultChartDir)
	defaults, err := charts.ReadDefaults(chartDir, options.highAvailability)
	if err != nil {
		return nil, err
	}

	controllerResources := &charts.Resources{}
	identityResources := &charts.Resources{}
	grafanaResources := &charts.Resources{}
	prometheusResources := &charts.Resources{}

	// if HA mode, use HA defaults from values-ha.yaml
	if options.highAvailability {
		// should have at least more than 1 replicas
		if options.controllerReplicas == 1 {
			options.controllerReplicas = defaults.ControllerReplicas
		}

		if options.proxyCPURequest == "" {
			options.proxyCPURequest = defaults.ProxyCPURequest
		}

		if options.proxyMemoryRequest == "" {
			options.proxyMemoryRequest = defaults.ProxyMemoryRequest
		}

		if options.proxyCPULimit == "" {
			options.proxyCPULimit = defaults.ProxyCPULimit
		}

		if options.proxyMemoryLimit == "" {
			options.proxyMemoryLimit = defaults.ProxyMemoryLimit
		}

		if configs.Proxy.Resource.RequestCpu == "" {
			configs.Proxy.Resource.RequestCpu = options.proxyCPURequest
		}

		// `configs` was built before the HA option is evaluated, so we need
		// to make sure the HA proxy resources are added here.
		if configs.Proxy.Resource.RequestMemory == "" {
			configs.Proxy.Resource.RequestMemory = options.proxyMemoryRequest
		}

		if configs.Proxy.Resource.LimitCpu == "" {
			configs.Proxy.Resource.LimitCpu = options.proxyCPULimit
		}

		if configs.Proxy.Resource.LimitMemory == "" {
			configs.Proxy.Resource.LimitMemory = options.proxyMemoryLimit
		}

		options.identityOptions.replicas = options.controllerReplicas

		controllerResources = &charts.Resources{
			CPU: charts.Constraints{
				Request: defaults.ControllerCPURequest,
				Limit:   defaults.ControllerCPULimit,
			},
			Memory: charts.Constraints{
				Request: defaults.ControllerMemoryRequest,
				Limit:   defaults.ControllerMemoryLimit,
			},
		}

		grafanaResources = &charts.Resources{
			CPU: charts.Constraints{
				Limit:   defaults.GrafanaCPULimit,
				Request: defaults.GrafanaCPURequest,
			},
			Memory: charts.Constraints{
				Limit:   defaults.GrafanaMemoryLimit,
				Request: defaults.GrafanaMemoryRequest,
			},
		}

		identityResources = &charts.Resources{
			CPU: charts.Constraints{
				Limit:   defaults.IdentityCPULimit,
				Request: defaults.IdentityCPURequest,
			},
			Memory: charts.Constraints{
				Limit:   defaults.IdentityMemoryLimit,
				Request: defaults.IdentityMemoryRequest,
			},
		}

		prometheusResources = &charts.Resources{
			CPU: charts.Constraints{
				Limit:   defaults.PrometheusCPULimit,
				Request: defaults.PrometheusCPURequest,
			},
			Memory: charts.Constraints{
				Limit:   defaults.PrometheusMemoryLimit,
				Request: defaults.PrometheusMemoryRequest,
			},
		}
	}

	globalJSON, proxyJSON, installJSON, err := config.ToJSON(configs)
	if err != nil {
		return nil, err
	}

	values := &installValues{
		// Container images:
		ControllerImage:        fmt.Sprintf("%s/controller", options.dockerRegistry),
		ControllerImageVersion: configs.GetGlobal().GetVersion(),
		WebImage:               fmt.Sprintf("%s/web", options.dockerRegistry),
		GrafanaImage:           fmt.Sprintf("%s/grafana", options.dockerRegistry),
		PrometheusImage:        defaults.PrometheusImage,
		ImagePullPolicy:        options.imagePullPolicy,

		// Kubernetes labels/annotations/resources:
		CreatedByAnnotation:      k8s.CreatedByAnnotation,
		CliVersion:               k8s.CreatedByAnnotationValue(),
		ControllerComponentLabel: k8s.ControllerComponentLabel,
		ControllerNamespaceLabel: k8s.ControllerNSLabel,
		ProxyContainerName:       k8s.ProxyContainerName,
		ProxyInjectAnnotation:    k8s.ProxyInjectAnnotation,
		ProxyInjectDisabled:      k8s.ProxyInjectDisabled,
		LinkerdNamespaceLabel:    k8s.LinkerdNamespaceLabel,

		// Controller configuration:
		Namespace:                   controlPlaneNamespace,
		ClusterDomain:               configs.GetGlobal().GetClusterDomain(),
		UUID:                        configs.GetInstall().GetUuid(),
		ControllerReplicas:          options.controllerReplicas,
		ControllerLogLevel:          options.controllerLogLevel,
		ControllerUID:               options.controllerUID,
		HighAvailability:            options.highAvailability,
		EnablePodAntiAffinity:       options.highAvailability,
		EnableH2Upgrade:             !options.disableH2Upgrade,
		NoInitContainer:             options.noInitContainer,
		WebhookFailurePolicy:        defaults.WebhookFailurePolicy,
		OmitWebhookSideEffects:      options.omitWebhookSideEffects,
		RestrictDashboardPrivileges: options.restrictDashboardPrivileges,
		PrometheusLogLevel:          toPromLogLevel(strings.ToLower(options.controllerLogLevel)),
		HeartbeatSchedule:           options.heartbeatSchedule(),

		Configs: configJSONs{
			Global:  globalJSON,
			Proxy:   proxyJSON,
			Install: installJSON,
		},

		DestinationResources:   controllerResources,
		GrafanaResources:       grafanaResources,
		HeartbeatResources:     controllerResources,
		IdentityResources:      identityResources,
		PrometheusResources:    prometheusResources,
		ProxyInjectorResources: controllerResources,
		PublicAPIResources:     controllerResources,
		SPValidatorResources:   controllerResources,
		TapResources:           controllerResources,
		WebResources:           controllerResources,

		ProxyInjector:    &charts.ProxyInjector{TLS: &charts.TLS{}},
		ProfileValidator: &charts.ProfileValidator{TLS: &charts.TLS{}},
		Tap:              &charts.Tap{TLS: &charts.TLS{}},

		Proxy: &charts.Proxy{
			Component:              k8s.Deployment, // only Deployment workloads are injected
			EnableExternalProfiles: options.enableExternalProfiles,
			Image: &charts.Image{
				Name:       registryOverride(options.proxyImage, options.dockerRegistry),
				PullPolicy: options.imagePullPolicy,
				Version:    options.proxyVersion,
			},
			LogLevel: options.proxyLogLevel,
			Ports: &charts.Ports{
				Admin:    int32(options.proxyAdminPort),
				Control:  int32(options.proxyControlPort),
				Inbound:  int32(options.proxyInboundPort),
				Outbound: int32(options.proxyOutboundPort),
			},
			Resources: &charts.Resources{
				CPU: charts.Constraints{
					Limit:   options.proxyCPULimit,
					Request: options.proxyCPURequest,
				},
				Memory: charts.Constraints{
					Limit:   options.proxyMemoryLimit,
					Request: options.proxyMemoryRequest,
				},
			},
			UID: options.proxyUID,
		},

		ProxyInit: &charts.ProxyInit{
			Image: &charts.Image{
				Name:       registryOverride(options.initImage, options.dockerRegistry),
				PullPolicy: options.imagePullPolicy,
				Version:    options.initImageVersion,
			},

			Resources: &charts.Resources{
				CPU: charts.Constraints{
					Limit:   defaults.ProxyInitCPULimit,
					Request: defaults.ProxyInitCPURequest,
				},
				Memory: charts.Constraints{
					Limit:   defaults.ProxyInitMemoryLimit,
					Request: defaults.ProxyInitMemoryRequest,
				},
			},
		},
	}

	inboundPortStrs := []string{}
	for _, port := range options.ignoreInboundPorts {
		inboundPortStrs = append(inboundPortStrs, strconv.FormatUint(uint64(port), 10))
	}
	values.ProxyInit.IgnoreInboundPorts = strings.Join(inboundPortStrs, ",")

	outboundPortStrs := []string{}
	for _, port := range options.ignoreOutboundPorts {
		outboundPortStrs = append(outboundPortStrs, strconv.FormatUint(uint64(port), 10))
	}
	values.ProxyInit.IgnoreOutboundPorts = strings.Join(outboundPortStrs, ",")

	return values, nil
}

func toPromLogLevel(level string) string {
	switch level {
	case "panic", "fatal":
		return "error"
	default:
		return level
	}
}

// TODO: are `installValues.Configs` and `configs` redundant?
func (values *installValues) render(w io.Writer, configs *pb.All) error {
	// Render raw values and create chart config
	rawValues, err := yaml.Marshal(values)
	if err != nil {
		return err
	}

	files := []*chartutil.BufferedFile{
		{Name: chartutil.ChartfileName},
	}

	if values.stage == "" || values.stage == configStage {
		files = append(files, []*chartutil.BufferedFile{
			{Name: "templates/namespace.yaml"},
			{Name: "templates/identity-rbac.yaml"},
			{Name: "templates/controller-rbac.yaml"},
			{Name: "templates/heartbeat-rbac.yaml"},
			{Name: "templates/web-rbac.yaml"},
			{Name: "templates/serviceprofile-crd.yaml"},
			{Name: "templates/trafficsplit-crd.yaml"},
			{Name: "templates/prometheus-rbac.yaml"},
			{Name: "templates/grafana-rbac.yaml"},
			{Name: "templates/proxy-injector-rbac.yaml"},
			{Name: "templates/sp-validator-rbac.yaml"},
			{Name: "templates/tap-rbac.yaml"},
			{Name: "templates/psp.yaml"},
		}...)
	}

	if values.stage == "" || values.stage == controlPlaneStage {
		files = append(files, []*chartutil.BufferedFile{
			{Name: "templates/_validate.tpl"},
			{Name: "templates/_affinity.tpl"},
			{Name: "templates/_config.tpl"},
			{Name: "templates/_helpers.tpl"},
			{Name: "templates/config.yaml"},
			{Name: "templates/identity.yaml"},
			{Name: "templates/controller.yaml"},
			{Name: "templates/heartbeat.yaml"},
			{Name: "templates/web.yaml"},
			{Name: "templates/prometheus.yaml"},
			{Name: "templates/grafana.yaml"},
			{Name: "templates/proxy-injector.yaml"},
			{Name: "templates/sp-validator.yaml"},
			{Name: "templates/tap.yaml"},
		}...)
	}

	chart := &charts.Chart{
		Name:      helmDefaultChartName,
		Dir:       helmDefaultChartDir,
		Namespace: controlPlaneNamespace,
		RawValues: rawValues,
		Files:     files,
	}
	buf, err := chart.Render()
	if err != nil {
		return err
	}

	return processYAML(&buf, w, ioutil.Discard, resourceTransformerInject{
		injectProxy: true,
		configs:     configs,
	})
}

func (options *installOptions) configs(identity *pb.IdentityContext) *pb.All {
	return &pb.All{
		Global:  options.globalConfig(identity),
		Proxy:   options.proxyConfig(),
		Install: options.installConfig(),
	}
}

func (options *installOptions) globalConfig(identity *pb.IdentityContext) *pb.Global {
	return &pb.Global{
		LinkerdNamespace:       controlPlaneNamespace,
		CniEnabled:             options.noInitContainer,
		Version:                options.controlPlaneVersion,
		IdentityContext:        identity,
		OmitWebhookSideEffects: options.omitWebhookSideEffects,
		ClusterDomain:          defaultClusterDomain,
	}
}

func (options *installOptions) installConfig() *pb.Install {
	installID := ""
	if options.generateUUID != nil {
		installID = options.generateUUID()
	}

	return &pb.Install{
		Uuid:       installID,
		CliVersion: version.Version,
		Flags:      options.recordedFlags,
	}
}

func (options *installOptions) proxyConfig() *pb.Proxy {
	ignoreInboundPorts := []*pb.Port{}
	for _, port := range options.ignoreInboundPorts {
		ignoreInboundPorts = append(ignoreInboundPorts, &pb.Port{Port: uint32(port)})
	}

	ignoreOutboundPorts := []*pb.Port{}
	for _, port := range options.ignoreOutboundPorts {
		ignoreOutboundPorts = append(ignoreOutboundPorts, &pb.Port{Port: uint32(port)})
	}

	return &pb.Proxy{
		ProxyImage: &pb.Image{
			ImageName:  registryOverride(options.proxyImage, options.dockerRegistry),
			PullPolicy: options.imagePullPolicy,
		},
		ProxyInitImage: &pb.Image{
			ImageName:  registryOverride(options.initImage, options.dockerRegistry),
			PullPolicy: options.imagePullPolicy,
		},
		ControlPort: &pb.Port{
			Port: uint32(options.proxyControlPort),
		},
		IgnoreInboundPorts:  ignoreInboundPorts,
		IgnoreOutboundPorts: ignoreOutboundPorts,
		InboundPort: &pb.Port{
			Port: uint32(options.proxyInboundPort),
		},
		AdminPort: &pb.Port{
			Port: uint32(options.proxyAdminPort),
		},
		OutboundPort: &pb.Port{
			Port: uint32(options.proxyOutboundPort),
		},
		Resource: &pb.ResourceRequirements{
			RequestCpu:    options.proxyCPURequest,
			RequestMemory: options.proxyMemoryRequest,
			LimitCpu:      options.proxyCPULimit,
			LimitMemory:   options.proxyMemoryLimit,
		},
		ProxyUid: options.proxyUID,
		LogLevel: &pb.LogLevel{
			Level: options.proxyLogLevel,
		},
		DisableExternalProfiles: !options.enableExternalProfiles,
		ProxyVersion:            options.proxyVersion,
		ProxyInitImageVersion:   options.initImageVersion,
	}
}

func errIfGlobalResourcesExist(options *installOptions) error {
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.LinkerdPreInstallGlobalResourcesChecks,
	}

	hc := healthcheck.NewHealthChecker(checks, &healthcheck.Options{
		ControlPlaneNamespace: controlPlaneNamespace,
		KubeConfig:            kubeconfigPath,
		Impersonate:           impersonate,
		NoInitContainer:       options.noInitContainer,
	})

	errMsgs := []string{}
	hc.RunChecks(func(result *healthcheck.CheckResult) {
		if result.Err != nil {
			if re, ok := result.Err.(*healthcheck.ResourceError); ok {
				// resource error, print in kind.group/name format
				for _, res := range re.Resources {
					errMsgs = append(errMsgs, res.String())
				}
			} else {
				// unknown error, just print it
				errMsgs = append(errMsgs, result.Err.Error())
			}
		}
	})

	if len(errMsgs) > 0 {
		return errors.New(strings.Join(errMsgs, "\n"))
	}

	return nil
}

func errIfLinkerdConfigConfigMapExists() error {
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, 0)
	if err != nil {
		return err
	}

	_, err = kubeAPI.CoreV1().Namespaces().Get(controlPlaneNamespace, metav1.GetOptions{})
	if err != nil {
		return err
	}

	_, _, err = healthcheck.FetchLinkerdConfigMap(kubeAPI, controlPlaneNamespace)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return fmt.Errorf("'linkerd-config' config map already exists")
}

func (idopts *installIdentityOptions) validate() error {
	if idopts == nil {
		return nil
	}

	if idopts.trustDomain != "" {
		if errs := validation.IsDNS1123Subdomain(idopts.trustDomain); len(errs) > 0 {
			return fmt.Errorf("invalid trust domain '%s': %s", idopts.trustDomain, errs[0])
		}
	}

	if idopts.trustPEMFile != "" || idopts.crtPEMFile != "" || idopts.keyPEMFile != "" {
		if idopts.trustPEMFile == "" {
			return errors.New("a trust anchors file must be specified if other credentials are provided")
		}
		if idopts.crtPEMFile == "" {
			return errors.New("a certificate file must be specified if other credentials are provided")
		}
		if idopts.keyPEMFile == "" {
			return errors.New("a private key file must be specified if other credentials are provided")
		}

		for _, f := range []string{idopts.trustPEMFile, idopts.crtPEMFile, idopts.keyPEMFile} {
			stat, err := os.Stat(f)
			if err != nil {
				return fmt.Errorf("missing file: %s", err)
			}
			if stat.IsDir() {
				return fmt.Errorf("not a file: %s", f)
			}
		}
	}

	return nil
}

func (idopts *installIdentityOptions) validateAndBuild() (*installIdentityValues, error) {
	if idopts == nil {
		return nil, nil
	}

	if err := idopts.validate(); err != nil {
		return nil, err
	}

	if idopts.trustPEMFile != "" && idopts.crtPEMFile != "" && idopts.keyPEMFile != "" {
		return idopts.readValues()
	}

	return idopts.genValues()
}

func (idopts *installIdentityOptions) issuerName() string {
	return fmt.Sprintf("identity.%s.%s", controlPlaneNamespace, idopts.trustDomain)
}

func (idopts *installIdentityOptions) genValues() (*installIdentityValues, error) {
	root, err := tls.GenerateRootCAWithDefaults(idopts.issuerName())
	if err != nil {
		return nil, fmt.Errorf("failed to generate root certificate for identity: %s", err)
	}

	return &installIdentityValues{
		TrustDomain:     idopts.trustDomain,
		TrustAnchorsPEM: root.Cred.Crt.EncodeCertificatePEM(),
		Issuer: &charts.Issuer{
			ClockSkewAllowance:  idopts.clockSkewAllowance.String(),
			IssuanceLifetime:    idopts.issuanceLifetime.String(),
			CrtExpiry:           root.Cred.Crt.Certificate.NotAfter,
			CrtExpiryAnnotation: k8s.IdentityIssuerExpiryAnnotation,
			TLS: &charts.TLS{
				KeyPEM: root.Cred.EncodePrivateKeyPEM(),
				CrtPEM: root.Cred.Crt.EncodeCertificatePEM(),
			},
		},
	}, nil
}

// readValues attempts to read an issuer configuration from disk
// to produce an `installIdentityValues`.
//
// The identity options must have already been validated.
func (idopts *installIdentityOptions) readValues() (*installIdentityValues, error) {
	creds, err := tls.ReadPEMCreds(idopts.keyPEMFile, idopts.crtPEMFile)
	if err != nil {
		return nil, err
	}

	trustb, err := ioutil.ReadFile(idopts.trustPEMFile)
	if err != nil {
		return nil, err
	}
	trustAnchorsPEM := string(trustb)
	roots, err := tls.DecodePEMCertPool(trustAnchorsPEM)
	if err != nil {
		return nil, err
	}

	if err := creds.Verify(roots, idopts.issuerName()); err != nil {
		return nil, fmt.Errorf("invalid credentials: %s", err)
	}

	return &installIdentityValues{
		TrustDomain:     idopts.trustDomain,
		TrustAnchorsPEM: trustAnchorsPEM,
		Issuer: &charts.Issuer{
			ClockSkewAllowance:  idopts.clockSkewAllowance.String(),
			IssuanceLifetime:    idopts.issuanceLifetime.String(),
			CrtExpiry:           creds.Crt.Certificate.NotAfter,
			CrtExpiryAnnotation: k8s.IdentityIssuerExpiryAnnotation,
			TLS: &charts.TLS{
				KeyPEM: creds.EncodePrivateKeyPEM(),
				CrtPEM: creds.EncodeCertificatePEM(),
			},
		},
	}, nil
}

func (idvals *installIdentityValues) toIdentityContext() *pb.IdentityContext {
	if idvals == nil {
		return nil
	}

	il, err := time.ParseDuration(idvals.Issuer.IssuanceLifetime)
	if err != nil {
		il = defaultIdentityIssuanceLifetime
	}

	csa, err := time.ParseDuration(idvals.Issuer.ClockSkewAllowance)
	if err != nil {
		csa = defaultIdentityClockSkewAllowance
	}

	return &pb.IdentityContext{
		TrustDomain:        idvals.TrustDomain,
		TrustAnchorsPem:    idvals.TrustAnchorsPEM,
		IssuanceLifetime:   ptypes.DurationProto(il),
		ClockSkewAllowance: ptypes.DurationProto(csa),
	}
}
