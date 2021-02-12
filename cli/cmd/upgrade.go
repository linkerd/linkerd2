package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/linkerd/linkerd2/cli/flag"
	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/config"
	flagspkg "github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	valuespkg "helm.sh/helm/v3/pkg/cli/values"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	controlPlaneMessage    = "Don't forget to run `linkerd upgrade control-plane`!"
	failMessage            = "For troubleshooting help, visit: https://linkerd.io/upgrade/#troubleshooting\n"
	trustRootChangeMessage = "Rotating the trust anchors will affect existing proxies\nSee https://linkerd.io/2/tasks/rotating_identity_certificates/ for more information"
)

var (
	addOnOverwrite bool
	manifests      string
	force          bool
)

/* The upgrade commands all follow the same flow:
 * 1. Load default values from the Linkerd2 chart
 * 2. Update the values with stored overrides
 * 3. Apply flags to further modify the values
 * 4. Render the chart using those values
 *
 * The individual commands (upgrade, upgrade config, and upgrade control-plane)
 * differ in which flags are available to each, what pre-check validations
 * are done, and which subset of the chart is rendered.
 */

// newCmdUpgradeConfig is a subcommand for `linkerd upgrade config`
func newCmdUpgradeConfig(values *l5dcharts.Values) *cobra.Command {
	allStageFlags, allStageFlagSet := makeAllStageFlags(values)
	var options valuespkg.Options

	cmd := &cobra.Command{
		Use:   "config [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes cluster-wide resources to upgrade an existing Linkerd",
		Long: `Output Kubernetes cluster-wide resources to upgrade an existing Linkerd.

Note that this command should be followed by "linkerd upgrade control-plane".

The upgrade can be configured by using the --set, --values, --set-string and --set-file flags.
A full list of configurable values can be found at https://www.github.com/linkerd/linkerd2/tree/main/charts/linkerd2/README.md
`,
		Example: `  # Default upgrade.
  linkerd upgrade config | kubectl apply -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {

			k, err := k8sClient(manifests)
			if err != nil {
				return err
			}
			return upgradeRunE(cmd.Context(), k, allStageFlags, configStage, options)
		},
	}

	cmd.Flags().AddFlagSet(allStageFlagSet)
	flagspkg.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}

// newCmdUpgradeControlPlane is a subcommand for `linkerd upgrade control-plane`
func newCmdUpgradeControlPlane(values *l5dcharts.Values) *cobra.Command {
	var options valuespkg.Options

	allStageFlags, allStageFlagSet := makeAllStageFlags(values)
	installUpgradeFlags, installUpgradeFlagSet, err := makeInstallUpgradeFlags(values)
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
	proxyFlags, proxyFlagSet := makeProxyFlags(values)

	flags := flattenFlags(allStageFlags, installUpgradeFlags, proxyFlags)

	cmd := &cobra.Command{
		Use:   "control-plane [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes control plane resources to upgrade an existing Linkerd",
		Long: `Output Kubernetes control plane resources to upgrade an existing Linkerd.

Note that the default flag values for this command come from the Linkerd control
plane. The default values displayed in the Flags section below only apply to the
install command. It should be run after "linkerd upgrade config".

The upgrade can be configured by using the --set, --values, --set-string and --set-file flags.
A full list of configurable values can be found at https://www.github.com/linkerd/linkerd2/tree/main/charts/linkerd2/README.md
`,
		Example: `  # Default upgrade.
  linkerd upgrade control-plane | kubectl apply -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := k8sClient(manifests)
			if err != nil {
				return err
			}
			return upgradeRunE(cmd.Context(), k, flags, controlPlaneStage, options)
		},
	}

	cmd.Flags().AddFlagSet(allStageFlagSet)
	cmd.Flags().AddFlagSet(installUpgradeFlagSet)
	cmd.Flags().AddFlagSet(proxyFlagSet)
	flagspkg.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}

func newCmdUpgrade() *cobra.Command {
	values, err := l5dcharts.NewValues()
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}

	var options valuespkg.Options
	allStageFlags, allStageFlagSet := makeAllStageFlags(values)
	installUpgradeFlags, installUpgradeFlagSet, err := makeInstallUpgradeFlags(values)
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
	proxyFlags, proxyFlagSet := makeProxyFlags(values)
	flags := flattenFlags(allStageFlags, installUpgradeFlags, proxyFlags)

	upgradeFlagSet := makeUpgradeFlags()

	cmd := &cobra.Command{
		Use:   "upgrade [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes configs to upgrade an existing Linkerd control plane",
		Long: `Output Kubernetes configs to upgrade an existing Linkerd control plane.

Note that the default flag values for this command come from the Linkerd control
plane. The default values displayed in the Flags section below only apply to the
install command.

The upgrade can be configured by using the --set, --values, --set-string and --set-file flags.
A full list of configurable values can be found at https://www.github.com/linkerd/linkerd2/tree/main/charts/linkerd2/README.md
`,

		Example: `  # Default upgrade.
  linkerd upgrade | kubectl apply --prune -l linkerd.io/control-plane-ns=linkerd -f -

  # Similar to install, upgrade may also be broken up into two stages, by user
  # privilege.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := k8sClient(manifests)
			if err != nil {
				return err
			}
			err = upgradeRunE(cmd.Context(), k, flags, "", options)
			if err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().AddFlagSet(allStageFlagSet)
	cmd.Flags().AddFlagSet(installUpgradeFlagSet)
	cmd.Flags().AddFlagSet(proxyFlagSet)
	cmd.PersistentFlags().AddFlagSet(upgradeFlagSet)
	flagspkg.AddValueOptionsFlags(cmd.Flags(), &options)

	cmd.AddCommand(newCmdUpgradeConfig(values))
	cmd.AddCommand(newCmdUpgradeControlPlane(values))

	return cmd
}

func k8sClient(manifestsFile string) (*k8s.KubernetesAPI, error) {
	// We need a Kubernetes client to fetch configs and issuer secrets.
	var k *k8s.KubernetesAPI
	var err error
	if manifestsFile != "" {
		readers, err := read(manifestsFile)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse manifests from %s: %s", manifestsFile, err)
		}

		k, err = k8s.NewFakeAPIFromManifests(readers)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse Kubernetes objects from manifest %s: %s", manifestsFile, err)
		}
	} else {
		k, err = k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
		if err != nil {
			return nil, fmt.Errorf("Failed to create a kubernetes client: %s", err)
		}
	}
	return k, nil
}

// makeUpgradeFlags returns a FlagSet of flags that are only accessible at upgrade-time
// and not at install-time.  These flags do not configure the Values used to
// render the chart but instead modify the behavior of the upgrade command itself.
// They are not persisted in any way.
func makeUpgradeFlags() *pflag.FlagSet {
	upgradeFlags := pflag.NewFlagSet("upgrade-only", pflag.ExitOnError)

	upgradeFlags.StringVar(
		&manifests, "from-manifests", "",
		"Read config from a Linkerd install YAML rather than from Kubernetes",
	)
	upgradeFlags.BoolVar(
		&force, "force", false,
		"Force upgrade operation even when issuer certificate does not work with the trust anchors of all proxies",
	)
	upgradeFlags.BoolVar(
		&addOnOverwrite, "addon-overwrite", false,
		"Overwrite add-on configuration instead of loading the existing config (or reset to defaults if no new config is specified)",
	)
	return upgradeFlags
}

func upgradeRunE(ctx context.Context, k *k8s.KubernetesAPI, flags []flag.Flag, stage string, options valuespkg.Options) error {

	buf, err := upgrade(ctx, k, flags, stage, options)
	if err != nil {
		return err
	}

	for _, flag := range flags {
		if flag.Name() == "identity-trust-anchors-file" && flag.IsSet() {
			fmt.Fprintf(os.Stderr, "\n%s %s\n\n", warnStatus, trustRootChangeMessage)
		}
	}
	if stage == configStage {
		fmt.Fprintf(os.Stderr, "%s\n\n", controlPlaneMessage)
	}

	buf.WriteTo(os.Stdout)

	return nil
}

func upgrade(ctx context.Context, k *k8s.KubernetesAPI, flags []flag.Flag, stage string, options valuespkg.Options) (bytes.Buffer, error) {
	values, err := loadStoredValues(ctx, k)
	if err != nil {
		return bytes.Buffer{}, err
	}
	// If there is no linkerd-config-overrides secret, assume we are upgrading
	// from a verion of Linkerd prior to the introduction of this secret.  In
	// this case we load the values from the legacy linkerd-config configmap.
	if values == nil {
		values, err = loadStoredValuesLegacy(ctx, k)
		if err != nil {
			return bytes.Buffer{}, err
		}
	}

	// If values is still nil, then neither the linkerd-config-overrides secret
	// nor the legacy values were found. This means either means that Linkerd
	// was installed with Helm or that the installation needs to be repaired.
	if values == nil {
		return bytes.Buffer{}, errors.New(
			`Could not find the Linkerd config. If Linkerd was installed with Helm, please
use Helm to perform upgrades. If Linkerd was not installed with Helm, please use
the 'linkerd repair' command to repair the Linkerd config`)
	}

	err = flag.ApplySetFlags(values, flags)
	if err != nil {
		return bytes.Buffer{}, err
	}

	if values.Identity.Issuer.Scheme == string(corev1.SecretTypeTLS) {
		for _, flag := range flags {
			if (flag.Name() == "identity-issuer-certificate-file" || flag.Name() == "identity-issuer-key-file") && flag.IsSet() {
				return bytes.Buffer{}, errors.New("cannot update issuer certificates if you are using external cert management solution")
			}
		}
	}

	err = validateValues(ctx, k, values)
	if err != nil {
		return bytes.Buffer{}, err
	}
	if !force && values.Identity.Issuer.Scheme == k8s.IdentityIssuerSchemeLinkerd {
		err = ensureIssuerCertWorksWithAllProxies(ctx, k, values)
		if err != nil {
			return bytes.Buffer{}, err
		}
	}

	// rendering to a buffer and printing full contents of buffer after
	// render is complete, to ensure that okStatus prints separately
	var buf bytes.Buffer
	if err = render(&buf, values, stage, options); err != nil {
		upgradeErrorf("Could not render upgrade configuration: %s", err)
	}

	return buf, nil
}

func loadStoredValues(ctx context.Context, k *k8s.KubernetesAPI) (*charts.Values, error) {
	// Load the default values from the chart.
	values, err := charts.NewValues()
	if err != nil {
		return nil, err
	}

	// Load the stored overrides from the linkerd-config-overrides secret.
	secret, err := k.CoreV1().Secrets(controlPlaneNamespace).Get(ctx, "linkerd-config-overrides", metav1.GetOptions{})
	if kerrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	bytes, ok := secret.Data["linkerd-config-overrides"]
	if !ok {
		return nil, errors.New("secret/linkerd-config-overrides is missing linkerd-config-overrides data")
	}

	bytes, err = config.RemoveGlobalFieldIfPresent(bytes)
	if err != nil {
		return nil, err
	}

	// Unmarshal the overrides directly onto the values.  This has the effect
	// of merging the two with the overrides taking priority.
	err = yaml.Unmarshal(bytes, values)
	if err != nil {
		return nil, err
	}

	return values, nil
}

// upgradeErrorf prints the error message and quits the upgrade process
func upgradeErrorf(format string, a ...interface{}) {
	template := fmt.Sprintf("%s %s\n%s\n", failStatus, format, failMessage)
	fmt.Fprintf(os.Stderr, template, a...)
	os.Exit(1)
}

func ensureIssuerCertWorksWithAllProxies(ctx context.Context, k *k8s.KubernetesAPI, values *l5dcharts.Values) error {
	cred, err := tls.ValidateAndCreateCreds(
		values.Identity.Issuer.TLS.CrtPEM,
		values.Identity.Issuer.TLS.KeyPEM,
	)
	if err != nil {
		return err
	}

	meshedPods, err := healthcheck.GetMeshedPodsIdentityData(ctx, k, "")
	var problematicPods []string
	if err != nil {
		return err
	}
	for _, pod := range meshedPods {
		anchors, err := tls.DecodePEMCertPool(pod.Anchors)

		if anchors != nil {
			err = cred.Verify(anchors, "", time.Time{})
		}

		if err != nil {
			problematicPods = append(problematicPods, fmt.Sprintf("* %s/%s", pod.Namespace, pod.Name))
		}
	}

	if len(problematicPods) > 0 {
		errorMessageHeader := "You are attempting to use an issuer certificate which does not validate against the trust anchors of the following pods:"
		errorMessageFooter := "These pods do not have the current trust bundle and must be restarted.  Use the --force flag to proceed anyway (this will likely prevent those pods from sending or receiving traffic)."
		return fmt.Errorf("%s\n\t%s\n%s", errorMessageHeader, strings.Join(problematicPods, "\n\t"), errorMessageFooter)
	}
	return nil
}
