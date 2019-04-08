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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	okMessage   = "You're on your way to upgrading Linkerd!\nVisit this URL for further instructions: https://linkerd.io/upgrade/#nextsteps\n"
	failMessage = "For troubleshooting help, visit: https://linkerd.io/upgrade/#troubleshooting\n"
)

type (
	upgradeOptions struct{ *installOptions }
)

func newUpgradeOptionsWithDefaults() *upgradeOptions {
	return &upgradeOptions{newInstallOptionsWithDefaults()}
}

func newCmdUpgrade() *cobra.Command {
	options := newUpgradeOptionsWithDefaults()
	flags := options.recordableFlagSet(pflag.ExitOnError)

	cmd := &cobra.Command{
		Use:   "upgrade [flags]",
		Short: "Output Kubernetes configs to upgrade an existing Linkerd control plane",
		Long: `Output Kubernetes configs to upgrade an existing Linkerd control plane.

Note that the default flag values for this command come from the Linkerd control
plane. The default values displayed in the Flags section below only apply to the
install command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// We need a Kubernetes client to fetch configs and issuer secrets.
			k, err := options.newK8s()
			if err != nil {
				upgradeErrorf("Failed to create a kubernetes client: %s", err)
			}

			values, configs, err := options.validateAndBuild(k, flags)
			if err != nil {
				return err
			}

			// rendering to a buffer and printing full contents of buffer after
			// render is complete, to ensure that okStatus prints separately
			var buf bytes.Buffer
			if err = values.render(&buf, configs); err != nil {
				upgradeErrorf("Could not render install configuration: %s", err)
			}

			buf.WriteTo(os.Stdout)

			fmt.Fprintf(os.Stderr, "\n%s %s\n", okStatus, okMessage)

			return nil
		},
	}

	cmd.PersistentFlags().AddFlagSet(flags)
	return cmd
}

func (options *upgradeOptions) validateAndBuild(k kubernetes.Interface, flags *pflag.FlagSet) (*installValues, *pb.All, error) {
	// We fetch the configs directly from kubernetes because we need to be able
	// to upgrade/reinstall the control plane when the API is not available; and
	// this also serves as a passive check that we have privileges to access this
	// control plane.
	configs, err := fetchConfigs(k)
	if err != nil {
		upgradeErrorf("Could not fetch configs from kubernetes: %s", err)
	}

	// If the install config needs to be repaired--either because it did not
	// exist or because it is missing expected fields, repair it.
	options.repairInstall(configs.Install)

	// We recorded flags during a prior install. If we haven't overridden the
	// flag on this upgrade, reset that prior value as if it were specified now.
	//
	// This implies that the default flag values for the upgrade command come
	// from the control-plane, and not from the defaults specified in the FlagSet.
	setOptionsFromInstall(flags, configs.GetInstall())

	if err = options.validate(); err != nil {
		return nil, nil, err
	}

	// Save off the updated set of flags into the installOptions so it gets
	// persisted with the upgraded config.
	options.recordFlags(flags)

	// Update the configs from the synthesized options.
	options.overrideConfigs(configs, map[string]string{})
	configs.GetInstall().Flags = options.recordedFlags

	var identity *installIdentityValues
	idctx := configs.GetGlobal().GetIdentityContext()
	if idctx.GetTrustDomain() == "" || idctx.GetTrustAnchorsPem() == "" {
		// If there wasn't an idctx, or if it doesn't specify the required fields, we
		// must be upgrading from a version that didn't support identity, so generate it anew...
		identity, err = options.installOptions.identityOptions.genValues()
		if err != nil {
			upgradeErrorf("Unable to generate issuer credentials.\nError: %s", err)
		}
		configs.GetGlobal().IdentityContext = identity.toIdentityContext()
	} else {
		identity, err = fetchIdentityValues(k, options.controllerReplicas, idctx)
		if err != nil {
			upgradeErrorf("Unable to fetch the existing issuer credentials from Kubernetes.\nError: %s", err)
		}
	}

	// Values have to be generated after any missing identity is generated,
	// otherwise it will be missing from the generated configmap.
	values, err := options.buildValuesWithoutIdentity(configs)
	if err != nil {
		upgradeErrorf("Could not build install configuration: %s", err)
	}
	values.Identity = identity

	return values, configs, nil
}

func setOptionsFromInstall(flags *pflag.FlagSet, install *pb.Install) {
	for _, i := range install.GetFlags() {
		if f := flags.Lookup(i.GetName()); f != nil && !f.Changed {
			f.Value.Set(i.GetValue())
			f.Changed = true
		}
	}
}
func (options *upgradeOptions) overrideConfigs(configs *pb.All, overrideAnnotations map[string]string) {
	options.installOptions.overrideConfigs(configs, overrideAnnotations)

	if options.proxyAutoInject {
		configs.GetGlobal().AutoInjectContext = &pb.AutoInjectContext{}
	}
}

func (options *upgradeOptions) newK8s() (kubernetes.Interface, error) {
	if options.ignoreCluster {
		panic("ignore cluster must be unset") // Programmer error.
	}

	c, err := k8s.GetConfig(kubeconfigPath, kubeContext)
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(c)
}

func (options *upgradeOptions) repairInstall(install *pb.Install) {
	if install == nil {
		install = &pb.Install{}
	}

	if install.GetUuid() == "" {
		install.Uuid = options.generateUUID()
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
