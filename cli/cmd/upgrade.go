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
	failMessage            = "For troubleshooting help, visit: https://linkerd.io/upgrade/#troubleshooting\n"
	trustRootChangeMessage = "Rotating the trust anchors will affect existing proxies\nSee https://linkerd.io/2/tasks/rotating_identity_certificates/ for more information"
)

var (
	manifests string
	force     bool
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
func newCmdUpgrade() *cobra.Command {
	values, err := l5dcharts.NewValues()
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}

	var crds bool
	var options valuespkg.Options
	installUpgradeFlags, installUpgradeFlagSet, err := makeInstallUpgradeFlags(values)
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
	proxyFlags, proxyFlagSet := makeProxyFlags(values)
	flags := flattenFlags(installUpgradeFlags, proxyFlags)

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

		Example: `  # Upgrade CRDs first
  linkerd upgrade --crds | kubectl apply --prune --prune-whitelist=apiextensions.k8s.io/v1/customresourcedefinitions

  # Then upgrade the controle-plane and remove linkerd resources that no longer exist in the current version
  linkerd upgrade | kubectl apply --prune -l linkerd.io/control-plane-ns=linkerd -f -

  # Then run this again to make sure that certain cluster-scoped resources are correctly pruned
  linkerd upgrade | kubectl apply --prune -l linkerd.io/control-plane-ns=linkerd \
  --prune-whitelist=rbac.authorization.k8s.io/v1/clusterrole \
  --prune-whitelist=rbac.authorization.k8s.io/v1/clusterrolebinding \
  --prune-whitelist=apiregistration.k8s.io/v1/apiservice -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if crds {
				// The CRD chart is not configurable.
				// TODO(ver): Error if values have been configured?
				if _, err := upgradeCRDs(options).WriteTo(os.Stdout); err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}
				return nil
			}

			k, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return fmt.Errorf("failed to create a kubernetes client: %w", err)
			}

			if err = upgradeControlPlaneRunE(cmd.Context(), k, flags, options, manifests); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
			return nil
		},
	}

	cmd.Flags().AddFlagSet(installUpgradeFlagSet)
	cmd.Flags().AddFlagSet(proxyFlagSet)
	cmd.PersistentFlags().AddFlagSet(upgradeFlagSet)
	flagspkg.AddValueOptionsFlags(cmd.Flags(), &options)
	cmd.Flags().BoolVar(&crds, "crds", false, "Upgrade Linkerd CRDs")

	return cmd
}

// makeConfigClient is used to re-initialize the Kubernetes client in order
// to fetch existing configuration. It accepts two arguments: a Kubernetes
// client, and a path to a manifest file. If the manifest path is empty, the
// client will not be re-initialized. When non-empty, the client will be
// replaced by a fake Kubernetes client that will hold the values parsed from
// the manifest.
func makeConfigClient(k *k8s.KubernetesAPI, localManifestPath string) (*k8s.KubernetesAPI, error) {
	if localManifestPath == "" {
		return k, nil
	}

	// We need a Kubernetes client to fetch configs and issuer secrets.
	readers, err := read(localManifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifests from %s: %w", localManifestPath, err)
	}

	k, err = k8s.NewFakeAPIFromManifests(readers)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Kubernetes objects from manifest %s: %w", localManifestPath, err)
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
	return upgradeFlags
}

func upgradeControlPlaneRunE(ctx context.Context, k *k8s.KubernetesAPI, flags []flag.Flag, options valuespkg.Options, localManifestPath string) error {

	crds := bytes.Buffer{}
	err := renderCRDs(&crds, options)
	if err != nil {
		return err
	}

	err = healthcheck.CheckCustomResourceDefinitions(ctx, k, crds.String())
	if err != nil {
		return fmt.Errorf("Linkerd CRDs must be installed first. Run linkerd upgrade with the --crds flag:\n%w", err)
	}

	// Re-initialize client if a local manifest path is used
	k, err = makeConfigClient(k, localManifestPath)
	if err != nil {
		return err
	}

	buf, err := upgradeControlPlane(ctx, k, flags, options)
	if err != nil {
		return err
	}

	for _, flag := range flags {
		if flag.Name() == "identity-trust-anchors-file" && flag.IsSet() {
			fmt.Fprintf(os.Stderr, "\n%s %s\n\n", warnStatus, trustRootChangeMessage)
		}
	}

	_, err = buf.WriteTo(os.Stdout)
	return err
}

func upgradeCRDs(options valuespkg.Options) *bytes.Buffer {
	var buf bytes.Buffer
	if err := renderCRDs(&buf, options); err != nil {
		upgradeErrorf("Could not render upgrade configuration: %s", err)
	}
	return &buf
}

func upgradeControlPlane(ctx context.Context, k *k8s.KubernetesAPI, flags []flag.Flag, options valuespkg.Options) (*bytes.Buffer, error) {
	values, err := loadStoredValues(ctx, k)
	if err != nil {
		return nil, fmt.Errorf("failed to load stored values: %w", err)
	}

	if values == nil {
		return nil, errors.New(
			`Could not find the linkerd-config-overrides secret.
			If Linkerd was installed with Helm, please use Helm to perform upgrades`)
	}

	err = flag.ApplySetFlags(values, flags)
	if err != nil {
		return nil, err
	}

	if values.Identity.Issuer.Scheme == string(corev1.SecretTypeTLS) {
		for _, flag := range flags {
			if (flag.Name() == "identity-issuer-certificate-file" || flag.Name() == "identity-issuer-key-file") && flag.IsSet() {
				return nil, errors.New("cannot update issuer certificates if you are using external cert management solution")
			}
		}
	}

	err = validateValues(ctx, k, values)
	if err != nil {
		return nil, err
	}
	if !force && values.Identity.Issuer.Scheme == k8s.IdentityIssuerSchemeLinkerd {
		err = ensureIssuerCertWorksWithAllProxies(ctx, k, values)
		if err != nil {
			return nil, err
		}
	}

	// Create values override
	valuesOverrides, err := options.MergeValues(nil)
	if err != nil {
		return nil, err
	}
	if !isRunAsRoot(valuesOverrides) {
		err = healthcheck.CheckNodesHaveNonDockerRuntime(ctx, k)
		if err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	if err = renderControlPlane(&buf, values, valuesOverrides); err != nil {
		upgradeErrorf("Could not render upgrade configuration: %s", err)
	}
	return &buf, nil
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
		// Skip control plane pods since they load their trust anchors from the linkerd-identity-trust-anchors configmap.
		if pod.Namespace == controlPlaneNamespace {
			continue
		}
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
