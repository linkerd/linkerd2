package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/linkerd/linkerd2/cli/flag"
	"github.com/linkerd/linkerd2/pkg/charts"
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/charts/static"
	flagspkg "github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tree"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	valuespkg "helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/engine"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	configStage       = "config"
	controlPlaneStage = "control-plane"

	helmDefaultChartNameCrds = "linkerd-crds"
	helmDefaultChartNameCP   = "linkerd-control-plane"

	errMsgCannotInitializeClient = `Unable to install the Linkerd control plane. Cannot connect to the Kubernetes cluster:

%s

You can use the --ignore-cluster flag if you just want to generate the installation config.`

	errMsgGlobalResourcesExist = `Unable to install the Linkerd control plane. It appears that there is an existing installation:

%s

If you are sure you'd like to have a fresh install, remove these resources with:

    linkerd install --ignore-cluster | kubectl delete -f -

Otherwise, you can use the --ignore-cluster flag to overwrite the existing global resources.
`

	errMsgLinkerdConfigResourceConflict = "Can't install the Linkerd control plane in the '%s' namespace. Reason: %s.\nRun the command `linkerd upgrade`, if you are looking to upgrade Linkerd.\n"
	errMsgGlobalResourcesMissing        = "Can't install the Linkerd control plane in the '%s' namespace. The required Linkerd global resources are missing.\nIf this is expected, use the --skip-checks flag to continue the installation.\n"
)

var (
	templatesCrdFiles = []string{
		"templates/policy/server.yaml",
		"templates/policy/serverauthorization.yaml",
		"templates/serviceprofile.yaml",
	}

	templatesConfigStage = []string{
		"templates/namespace.yaml",
		"templates/identity-rbac.yaml",
		"templates/destination-rbac.yaml",
		"templates/heartbeat-rbac.yaml",
		"templates/proxy-injector-rbac.yaml",
		"templates/psp.yaml",
	}

	templatesControlPlaneStage = []string{
		"templates/config.yaml",
		"templates/identity.yaml",
		"templates/destination.yaml",
		"templates/heartbeat.yaml",
		"templates/proxy-injector.yaml",
	}

	ignoreCluster bool
)

/* Commands */

/* The install commands all follow the same flow:
 * 1. Load default values from the Linkerd2 chart
 * 2. Apply flags to modify the values
 * 3. Render the chart using those values
 *
 * The individual commands (install, install config, and install control-plane)
 * differ in which flags are available to each, what pre-check validations
 * are done, and which subset of the chart is rendered.
 */

func newCmdInstallConfig(values *l5dcharts.Values) *cobra.Command {
	flags, flagSet := makeAllStageFlags(values)
	var options valuespkg.Options

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
  linkerd install config -L linkerdtest | kubectl apply -f -

The installation can be configured by using the --set, --values, --set-string and --set-file flags.
A full list of configurable values can be found at https://www.github.com/linkerd/linkerd2/tree/main/charts/linkerd2/README.md`,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := flag.ApplySetFlags(values, flags)
			if err != nil {
				return err
			}

			// Create values override
			valuesOverrides, err := options.MergeValues(nil)
			if err != nil {
				return err
			}

			if !ignoreCluster {
				// Ensure k8s is reachable and that Linkerd is not already installed.
				if err := errAfterRunningChecks(values.CNIEnabled); err != nil {
					if healthcheck.IsCategoryError(err, healthcheck.KubernetesAPIChecks) {
						fmt.Fprintf(os.Stderr, errMsgCannotInitializeClient, err)
					} else {
						fmt.Fprintf(os.Stderr, errMsgGlobalResourcesExist, err)
					}
					os.Exit(1)
				}

				// Initialize the k8s API which is used for the proxyInit
				// runAsRoot check.
				k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 30*time.Second)
				if err != nil {
					return err
				}
				if !isRunAsRoot(valuesOverrides) {
					err = healthcheck.CheckNodesHaveNonDockerRuntime(cmd.Context(), k8sAPI)
					if err != nil {
						fmt.Fprintln(os.Stderr, err)
						os.Exit(1)
					}
				}
			}
			return render(os.Stdout, values, configStage, valuesOverrides)
		},
	}
	flagspkg.AddValueOptionsFlags(cmd.Flags(), &options)

	cmd.Flags().AddFlagSet(flagSet)

	return cmd
}

func newCmdInstallControlPlane(values *l5dcharts.Values) *cobra.Command {
	var skipChecks bool
	var options valuespkg.Options

	allStageFlags, allStageFlagSet := makeAllStageFlags(values)
	installOnlyFlags, installOnlyFlagSet := makeInstallFlags(values)
	installUpgradeFlags, installUpgradeFlagSet, err := makeInstallUpgradeFlags(values)
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
	proxyFlags, proxyFlagSet := makeProxyFlags(values)

	flags := flattenFlags(allStageFlags, installOnlyFlags, installUpgradeFlags, proxyFlags)

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
  linkerd install control-plane -l linkerdtest | kubectl apply -f -

The installation can be configured by using the --set, --values, --set-string and --set-file flags.
A full list of configurable values can be found at https://www.github.com/linkerd/linkerd2/tree/main/charts/linkerd2/README.md
  `,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !ignoreCluster {
				if !skipChecks {
					// check if global resources exist to determine if the `install config`
					// stage succeeded
					if err := errAfterRunningChecks(values.CNIEnabled); err == nil {
						if healthcheck.IsCategoryError(err, healthcheck.KubernetesAPIChecks) {
							fmt.Fprintf(os.Stderr, errMsgCannotInitializeClient, err)
						} else {
							fmt.Fprintf(os.Stderr, errMsgGlobalResourcesMissing, controlPlaneNamespace)
						}
						os.Exit(1)
					}
				}

				// Ensure there is not already an existing Linkerd installation.
				if err := errIfLinkerdConfigConfigMapExists(cmd.Context()); err != nil {
					fmt.Fprintf(os.Stderr, errMsgLinkerdConfigResourceConflict, controlPlaneNamespace, err.Error())
					os.Exit(1)
				}
			}

			return install(cmd.Context(), os.Stdout, values, flags, controlPlaneStage, options)
		},
	}

	cmd.Flags().AddFlagSet(allStageFlagSet)
	cmd.Flags().AddFlagSet(installOnlyFlagSet)
	cmd.Flags().AddFlagSet(installUpgradeFlagSet)
	cmd.Flags().AddFlagSet(proxyFlagSet)
	flagspkg.AddValueOptionsFlags(cmd.Flags(), &options)

	cmd.Flags().BoolVar(
		&skipChecks, "skip-checks", false,
		`Skip checks for namespace existence`,
	)

	return cmd
}

func newCmdInstall() *cobra.Command {
	values, err := l5dcharts.NewValues()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	var options valuespkg.Options

	allStageFlags, allStageFlagSet := makeAllStageFlags(values)
	installOnlyFlags, installOnlyFlagSet := makeInstallFlags(values)
	installUpgradeFlags, installUpgradeFlagSet, err := makeInstallUpgradeFlags(values)
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
	proxyFlags, proxyFlagSet := makeProxyFlags(values)

	flags := flattenFlags(allStageFlags, installOnlyFlags, installUpgradeFlags, proxyFlags)

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
  # subcommands.

The installation can be configured by using the --set, --values, --set-string and --set-file flags.
A full list of configurable values can be found at https://www.github.com/linkerd/linkerd2/tree/main/charts/linkerd2/README.md`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return install(cmd.Context(), os.Stdout, values, flags, "", options)
		},
	}

	cmd.Flags().AddFlagSet(allStageFlagSet)
	cmd.Flags().AddFlagSet(installOnlyFlagSet)
	cmd.Flags().AddFlagSet(installUpgradeFlagSet)
	cmd.Flags().AddFlagSet(proxyFlagSet)
	cmd.PersistentFlags().BoolVar(&ignoreCluster, "ignore-cluster", false,
		"Ignore the current Kubernetes cluster when checking for existing cluster configuration (default false)")

	cmd.AddCommand(newCmdInstallConfig(values))
	cmd.AddCommand(newCmdInstallControlPlane(values))

	flagspkg.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}

func install(ctx context.Context, w io.Writer, values *l5dcharts.Values, flags []flag.Flag, stage string, options valuespkg.Options) error {
	err := flag.ApplySetFlags(values, flags)
	if err != nil {
		return err
	}

	// Create values override
	valuesOverrides, err := options.MergeValues(nil)
	if err != nil {
		return err
	}

	var k8sAPI *k8s.KubernetesAPI
	if !ignoreCluster {
		// Ensure there is not already an existing Linkerd installation.
		k8sAPI, err = k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 30*time.Second)
		if err != nil {
			return err
		}

		// We just want to check if `linkerd-configmap` exists
		_, err := k8sAPI.CoreV1().ConfigMaps(controlPlaneNamespace).Get(ctx, k8s.ConfigConfigMapName, metav1.GetOptions{})
		if err == nil {
			fmt.Fprintf(os.Stderr, errMsgLinkerdConfigResourceConflict, controlPlaneNamespace, "ConfigMap/linkerd-config already exists")
			os.Exit(1)
		}
		if !kerrors.IsNotFound(err) {
			return err
		}

		if !isRunAsRoot(valuesOverrides) {
			err = healthcheck.CheckNodesHaveNonDockerRuntime(ctx, k8sAPI)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	}

	err = initializeIssuerCredentials(ctx, k8sAPI, values)
	if err != nil {
		return err
	}

	err = validateValues(ctx, k8sAPI, values)
	if err != nil {
		return err
	}

	return render(w, values, stage, valuesOverrides)
}

func isRunAsRoot(values map[string]interface{}) bool {
	if proxyInit, ok := values["proxyInit"]; ok {
		if val, ok := proxyInit.(map[string]interface{})["runAsRoot"]; ok {
			if truth, ok := template.IsTrue(val); ok {
				return truth
			}
		}
	}
	return false
}

func render(w io.Writer, values *l5dcharts.Values, stage string, valuesOverrides map[string]interface{}) error {

	values.Stage = stage

	crdFiles := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
	}
	configFiles := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
	}

	if stage == "" || stage == configStage {
		for _, template := range templatesCrdFiles {
			crdFiles = append(crdFiles,
				&loader.BufferedFile{Name: template},
			)
		}
		if err := charts.FilesReader(static.Templates, l5dcharts.HelmChartDirCrds+"/", crdFiles); err != nil {
			return err
		}

		for _, template := range templatesConfigStage {
			configFiles = append(configFiles,
				&loader.BufferedFile{Name: template},
			)
		}
		if err := charts.FilesReader(static.Templates, l5dcharts.HelmChartDirCP+"/", configFiles); err != nil {
			return err
		}

	}

	var cpFiles []*loader.BufferedFile
	if stage == controlPlaneStage {
		cpFiles = append(cpFiles, &loader.BufferedFile{Name: chartutil.ChartfileName})
	}

	if stage == "" || stage == controlPlaneStage {
		for _, template := range templatesControlPlaneStage {
			cpFiles = append(cpFiles,
				&loader.BufferedFile{Name: template},
			)
		}
		if err := charts.FilesReader(static.Templates, l5dcharts.HelmChartDirCP+"/", cpFiles); err != nil {
			return err
		}
	}

	var partialFiles []*loader.BufferedFile
	for _, template := range charts.L5dPartials {
		partialFiles = append(partialFiles,
			&loader.BufferedFile{Name: template},
		)
	}

	// Load all partial chart files into buffer
	if err := charts.FilesReader(static.Templates, "", partialFiles); err != nil {
		return err
	}

	// Create a Chart obj from the files
	configStageFiles := append(crdFiles, configFiles...)
	files := append(configStageFiles, cpFiles...)
	files = append(files, partialFiles...)
	chart, err := loader.LoadFiles(files)
	if err != nil {
		return err
	}

	// Store final Values generated from values.yaml and CLI flags
	chart.Values, err = values.ToMap()
	if err != nil {
		return err
	}

	vals, err := chartutil.CoalesceValues(chart, valuesOverrides)
	if err != nil {
		return err
	}

	fullValues := map[string]interface{}{
		"Values": vals,
		"Release": map[string]interface{}{
			"Namespace": controlPlaneNamespace,
			"Service":   "CLI",
		},
	}

	// Attach the final values into the `Values` field for rendering to work
	renderedTemplates, err := engine.Render(chart, fullValues)
	if err != nil {
		return fmt.Errorf("failed to render the template: %s", err)
	}

	// Merge templates and inject
	var buf bytes.Buffer
	for _, tmpl := range chart.Templates {
		t := path.Join(chart.Metadata.Name, tmpl.Name)
		if _, err := buf.WriteString(renderedTemplates[t]); err != nil {
			return err
		}
	}

	if stage == "" || stage == controlPlaneStage {
		overrides, err := renderOverrides(vals, false)
		if err != nil {
			return err
		}
		buf.WriteString(yamlSep)
		buf.WriteString(string(overrides))
	}

	_, err = w.Write(buf.Bytes())
	return err
}

// renderOverrides outputs the Secret/linkerd-config-overrides resource which
// contains the subset of the values which have been changed from their defaults.
// This secret is used by the upgrade command the load configuration which was
// specified at install time.  Note that if identity issuer credentials were
// supplied to the install command or if they were generated by the install
// command, those credentials will be saved here so that they are preserved
// during upgrade.  Note also that this Secret/linkerd-config-overrides
// resource is not part of the Helm chart and will not be present when installing
// with Helm. If stringData is set to true, the secret will be rendered using
// the StringData field instead of the Data field, making the output more
// human readable.
func renderOverrides(values chartutil.Values, stringData bool) ([]byte, error) {
	defaults, err := l5dcharts.NewValues()
	if err != nil {
		return nil, err
	}
	// Remove unnecessary fields, including fields added by helm's `chartutil.CoalesceValues`
	delete(values, "configs")
	delete(values, "partials")
	delete(values, "stage")

	overrides, err := tree.Diff(defaults, values)
	if err != nil {
		return nil, err
	}

	overridesBytes, err := yaml.Marshal(overrides)
	if err != nil {
		return nil, err
	}

	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{Kind: "Secret", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "linkerd-config-overrides",
			Namespace: controlPlaneNamespace,
			Labels: map[string]string{
				k8s.ControllerNSLabel: controlPlaneNamespace,
			},
		},
	}
	if stringData {
		secret.StringData = map[string]string{
			"linkerd-config-overrides": string(overridesBytes),
		}
	} else {
		secret.Data = map[string][]byte{
			"linkerd-config-overrides": overridesBytes,
		}
	}
	bytes, err := yaml.Marshal(secret)
	if err != nil {
		return nil, err
	}
	return bytes, nil
}

func errAfterRunningChecks(cniEnabled bool) error {
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.LinkerdPreInstallGlobalResourcesChecks,
	}
	hc := healthcheck.NewHealthChecker(checks, &healthcheck.Options{
		ControlPlaneNamespace: controlPlaneNamespace,
		KubeConfig:            kubeconfigPath,
		Impersonate:           impersonate,
		ImpersonateGroup:      impersonateGroup,
		KubeContext:           kubeContext,
		APIAddr:               apiAddr,
		CNIEnabled:            cniEnabled,
	})

	var k8sAPIError error
	errMsgs := []string{}
	hc.RunChecks(func(result *healthcheck.CheckResult) {
		if result.Err != nil {
			if ce, ok := result.Err.(*healthcheck.CategoryError); ok {
				if ce.Category == healthcheck.KubernetesAPIChecks {
					k8sAPIError = ce
				} else if re, ok := ce.Err.(*healthcheck.ResourceError); ok {
					// resource error, print in kind.group/name format
					for _, res := range re.Resources {
						errMsgs = append(errMsgs, res.String())
					}
				} else {
					// unknown category error, just print it
					errMsgs = append(errMsgs, result.Err.Error())
				}
			} else {
				// unknown error, just print it
				errMsgs = append(errMsgs, result.Err.Error())
			}
		}
	})

	// errors from the KubernetesAPIChecks category take precedence
	if k8sAPIError != nil {
		return k8sAPIError
	}

	if len(errMsgs) > 0 {
		return errors.New(strings.Join(errMsgs, "\n"))
	}

	return nil
}

func errIfLinkerdConfigConfigMapExists(ctx context.Context) error {
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return err
	}

	_, err = kubeAPI.CoreV1().Namespaces().Get(ctx, controlPlaneNamespace, metav1.GetOptions{})
	if err != nil {
		return err
	}

	_, _, err = healthcheck.FetchCurrentConfiguration(ctx, kubeAPI, controlPlaneNamespace)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return fmt.Errorf("'linkerd-config' config map already exists")
}
