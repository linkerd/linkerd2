package cmd

import (
	"bytes"
	"fmt"
	"os"
	"time"

	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/config"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	okMessage           = "You're on your way to upgrading Linkerd!"
	controlPlaneMessage = "Don't forget to run `linkerd upgrade control-plane`!"
	visitMessage        = "Visit this URL for further instructions: https://linkerd.io/upgrade/#nextsteps"
	failMessage         = "For troubleshooting help, visit: https://linkerd.io/upgrade/#troubleshooting\n"
)

type upgradeOptions struct {
	manifests string
	*installOptions

	verifyTLS func(tls *tlsValues, service string) error
}

func newUpgradeOptionsWithDefaults() *upgradeOptions {
	return &upgradeOptions{
		manifests:      "",
		installOptions: newInstallOptionsWithDefaults(),
		verifyTLS:      verifyWebhookTLS,
	}
}

// upgradeOnlyFlagSet includes flags that are only accessible at upgrade-time
// and not at install-time. also these flags are not intended to be persisted
// via linkerd-config ConfigMap, unlike recordableFlagSet
func (options *upgradeOptions) upgradeOnlyFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("upgrade-only", pflag.ExitOnError)

	flags.StringVar(
		&options.manifests, "from-manifests", options.manifests,
		"Read config from a Linkerd install YAML rather than from Kubernetes",
	)

	return flags
}

// newCmdUpgradeConfig is a subcommand for `linkerd upgrade config`
func newCmdUpgradeConfig(options *upgradeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes cluster-wide resources to upgrade an existing Linkerd",
		Long: `Output Kubernetes cluster-wide resources to upgrade an existing Linkerd.

Note that this command should be followed by "linkerd upgrade control-plane".`,
		Example: `  # Default upgrade.
  linkerd upgrade config | kubectl apply -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return upgradeRunE(options, configStage, nil)
		},
	}

	return cmd
}

// newCmdUpgradeControlPlane is a subcommand for `linkerd upgrade control-plane`
func newCmdUpgradeControlPlane(options *upgradeOptions) *cobra.Command {
	flags := options.recordableFlagSet()

	cmd := &cobra.Command{
		Use:   "control-plane [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes control plane resources to upgrade an existing Linkerd",
		Long: `Output Kubernetes control plane resources to upgrade an existing Linkerd.

Note that the default flag values for this command come from the Linkerd control
plane. The default values displayed in the Flags section below only apply to the
install command. It should be run after "linkerd upgrade config".`,
		Example: `  # Default upgrade.
  linkerd upgrade control-plane | kubectl apply -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return upgradeRunE(options, controlPlaneStage, flags)
		},
	}

	cmd.PersistentFlags().AddFlagSet(flags)

	return cmd
}

func newCmdUpgrade() *cobra.Command {
	options := newUpgradeOptionsWithDefaults()
	flags := options.recordableFlagSet()
	upgradeOnlyFlags := options.upgradeOnlyFlagSet()

	cmd := &cobra.Command{
		Use:   "upgrade [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes configs to upgrade an existing Linkerd control plane",
		Long: `Output Kubernetes configs to upgrade an existing Linkerd control plane.

Note that the default flag values for this command come from the Linkerd control
plane. The default values displayed in the Flags section below only apply to the
install command.`,

		Example: `  # Default upgrade.
  linkerd upgrade | kubectl apply -f -

  # Similar to install, upgrade may also be broken up into two stages, by user
  # privilege.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return upgradeRunE(options, "", flags)
		},
	}

	cmd.Flags().AddFlagSet(flags)
	cmd.PersistentFlags().AddFlagSet(upgradeOnlyFlags)

	cmd.AddCommand(newCmdUpgradeConfig(options))
	cmd.AddCommand(newCmdUpgradeControlPlane(options))

	return cmd
}

func upgradeRunE(options *upgradeOptions, stage string, flags *pflag.FlagSet) error {
	if options.ignoreCluster {
		panic("ignore cluster must be unset") // Programmer error.
	}

	// We need a Kubernetes client to fetch configs and issuer secrets.
	var k kubernetes.Interface
	var err error
	if options.manifests != "" {
		readers, err := read(options.manifests)
		if err != nil {
			upgradeErrorf("Failed to parse manifests from %s: %s", options.manifests, err)
		}

		k, err = k8s.NewFakeAPIFromManifests(readers)
		if err != nil {
			upgradeErrorf("Failed to parse Kubernetes objects from manifest %s: %s", options.manifests, err)
		}
	} else {
		k, err = k8s.NewAPI(kubeconfigPath, kubeContext, 0)
		if err != nil {
			upgradeErrorf("Failed to create a kubernetes client: %s", err)
		}
	}

	values, configs, err := options.validateAndBuild(stage, k, flags)
	if err != nil {
		upgradeErrorf("Failed to build upgrade configuration: %s", err)
	}

	// rendering to a buffer and printing full contents of buffer after
	// render is complete, to ensure that okStatus prints separately
	var buf bytes.Buffer
	if err = values.render(&buf, configs); err != nil {
		upgradeErrorf("Could not render upgrade configuration: %s", err)
	}

	buf.WriteTo(os.Stdout)

	fmt.Fprintf(os.Stderr, "\n%s %s\n", okStatus, okMessage)
	if stage == configStage {
		fmt.Fprintf(os.Stderr, "%s\n", controlPlaneMessage)
	}
	fmt.Fprintf(os.Stderr, "%s\n\n", visitMessage)

	return nil
}

func (options *upgradeOptions) validateAndBuild(stage string, k kubernetes.Interface, flags *pflag.FlagSet) (*installValues, *pb.All, error) {
	if err := options.validate(); err != nil {
		return nil, nil, err
	}

	// We fetch the configs directly from kubernetes because we need to be able
	// to upgrade/reinstall the control plane when the API is not available; and
	// this also serves as a passive check that we have privileges to access this
	// control plane.
	configs, err := fetchConfigs(k)
	if err != nil {
		return nil, nil, fmt.Errorf("could not fetch configs from kubernetes: %s", err)
	}

	// If the install config needs to be repaired--either because it did not
	// exist or because it is missing expected fields, repair it.
	repairInstall(options.generateUUID, configs.Install)

	// We recorded flags during a prior install. If we haven't overridden the
	// flag on this upgrade, reset that prior value as if it were specified now.
	//
	// This implies that the default flag values for the upgrade command come
	// from the control-plane, and not from the defaults specified in the FlagSet.
	setFlagsFromInstall(flags, configs.GetInstall().GetFlags())

	// Save off the updated set of flags into the installOptions so it gets
	// persisted with the upgraded config.
	options.recordFlags(flags)

	// Update the configs from the synthesized options.
	options.overrideConfigs(configs, map[string]string{})
	if options.controlPlaneVersion != "" {
		configs.GetGlobal().Version = options.controlPlaneVersion
	}
	configs.GetInstall().Flags = options.recordedFlags

	var identity *installIdentityValues
	idctx := configs.GetGlobal().GetIdentityContext()
	if idctx.GetTrustDomain() == "" || idctx.GetTrustAnchorsPem() == "" {
		// If there wasn't an idctx, or if it doesn't specify the required fields, we
		// must be upgrading from a version that didn't support identity, so generate it anew...
		identity, err = options.identityOptions.genValues()
		if err != nil {
			return nil, nil, fmt.Errorf("unable to generate issuer credentials: %s", err)
		}
		configs.GetGlobal().IdentityContext = identity.toIdentityContext()
	} else {
		identity, err = fetchIdentityValues(k, options.controllerReplicas, idctx)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to fetch the existing issuer credentials from Kubernetes: %s", err)
		}
	}

	// Values have to be generated after any missing identity is generated,
	// otherwise it will be missing from the generated configmap.
	values, err := options.buildValuesWithoutIdentity(configs)
	if err != nil {
		return nil, nil, fmt.Errorf("could not build install configuration: %s", err)
	}
	values.Identity = identity

	// if exist, re-use the proxy injector and profile validator TLS secrets,
	// otherwise, generate new ones.
	webhooksTLS, err := fetchWebhooksTLS(k, options)
	if err != nil {
		return nil, nil, fmt.Errorf("could not fetch existing CA Trust: %s", err)
	}
	values.ProxyInjector = &proxyInjectorValues{
		webhooksTLS[k8s.ProxyInjectorWebhookServiceName],
	}
	values.ProfileValidator = &profileValidatorValues{
		webhooksTLS[k8s.SPValidatorWebhookServiceName],
	}

	values.stage = stage

	return values, configs, nil
}

func setFlagsFromInstall(flags *pflag.FlagSet, installFlags []*pb.Install_Flag) {
	for _, i := range installFlags {
		if f := flags.Lookup(i.GetName()); f != nil && !f.Changed {
			f.Value.Set(i.GetValue())
			f.Changed = true
		}
	}
}

func repairInstall(generateUUID func() string, install *pb.Install) {
	if install == nil {
		install = &pb.Install{}
	}

	if install.GetUuid() == "" {
		install.Uuid = generateUUID()
	}

	// ALWAYS update the CLI version to the most recent.
	install.CliVersion = version.Version

	// Install flags are updated separately.
}

// fetchConfigs checks the kubernetes API to fetch an existing
// linkerd configuration.
//
// This bypasses the public API so that upgrades can proceed when the API pod is
// not available.
func fetchConfigs(k kubernetes.Interface) (*pb.All, error) {
	configMap, err := k.CoreV1().
		ConfigMaps(controlPlaneNamespace).
		Get(k8s.ConfigConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return config.FromConfigMap(configMap.Data)
}

func fetchWebhooksTLS(k kubernetes.Interface, options *upgradeOptions) (map[string]*tlsValues, error) {
	values := map[string]*tlsValues{}

	for _, webhook := range []string{k8s.ProxyInjectorWebhookServiceName, k8s.SPValidatorWebhookServiceName} {
		secretName, err := webhookSecretName(webhook)
		if err != nil {
			return nil, err
		}

		var value *tlsValues
		secret, err := k.CoreV1().
			Secrets(controlPlaneNamespace).
			Get(secretName, metav1.GetOptions{})
		if err != nil {
			if !kerrors.IsNotFound(err) {
				return nil, err
			}

			value, err = options.generateWebhookTLS(webhook)
			if err != nil {
				return nil, err
			}
		} else {
			value = &tlsValues{
				KeyPEM: string(secret.Data["key.pem"]),
				CrtPEM: string(secret.Data["crt.pem"]),
			}
		}

		if err := options.verifyTLS(value, webhook); err != nil {
			return nil, err
		}

		values[webhook] = value
	}

	return values, nil
}

// fetchIdentityValue checks the kubernetes API to fetch an existing
// linkerd identity configuration.
//
// This bypasses the public API so that we can access secrets and validate
// permissions.
func fetchIdentityValues(k kubernetes.Interface, replicas uint, idctx *pb.IdentityContext) (*installIdentityValues, error) {
	if idctx == nil {
		return nil, nil
	}

	keyPEM, crtPEM, expiry, err := fetchIssuer(k, idctx.GetTrustAnchorsPem())
	if err != nil {
		return nil, err
	}

	return &installIdentityValues{
		Replicas:        replicas,
		TrustDomain:     idctx.GetTrustDomain(),
		TrustAnchorsPEM: idctx.GetTrustAnchorsPem(),
		Issuer: &issuerValues{
			ClockSkewAllowance:  idctx.GetClockSkewAllowance().String(),
			IssuanceLifetime:    idctx.GetIssuanceLifetime().String(),
			CrtExpiryAnnotation: k8s.IdentityIssuerExpiryAnnotation,

			KeyPEM:    keyPEM,
			CrtPEM:    crtPEM,
			CrtExpiry: expiry,
		},
	}, nil
}

func fetchIssuer(k kubernetes.Interface, trustPEM string) (string, string, time.Time, error) {
	roots, err := tls.DecodePEMCertPool(trustPEM)
	if err != nil {
		return "", "", time.Time{}, err
	}

	secret, err := k.CoreV1().
		Secrets(controlPlaneNamespace).
		Get(k8s.IdentityIssuerSecretName, metav1.GetOptions{})
	if err != nil {
		return "", "", time.Time{}, err
	}

	keyPEM := string(secret.Data[k8s.IdentityIssuerKeyName])
	key, err := tls.DecodePEMKey(keyPEM)
	if err != nil {
		return "", "", time.Time{}, err
	}

	crtPEM := string(secret.Data[k8s.IdentityIssuerCrtName])
	crt, err := tls.DecodePEMCrt(crtPEM)
	if err != nil {
		return "", "", time.Time{}, err
	}

	cred := &tls.Cred{PrivateKey: key, Crt: *crt}
	if err = cred.Verify(roots, ""); err != nil {
		return "", "", time.Time{}, fmt.Errorf("invalid issuer credentials: %s", err)
	}

	return keyPEM, crtPEM, crt.Certificate.NotAfter, nil
}

// upgradeErrorf prints the error message and quits the upgrade process
func upgradeErrorf(format string, a ...interface{}) {
	template := fmt.Sprintf("%s %s\n%s\n", failStatus, format, failMessage)
	fmt.Fprintf(os.Stderr, template, a...)
	os.Exit(1)
}
