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
	helmDefaultChartNameCrds = "linkerd-crds"
	helmDefaultChartNameCP   = "linkerd-control-plane"

	errMsgCannotInitializeClient = `Unable to install the Linkerd control plane. Cannot connect to the Kubernetes cluster:

%s

You can use the --ignore-cluster flag if you just want to generate the installation config.`

	errMsgLinkerdConfigResourceConflict = "Can't install the Linkerd control plane in the '%s' namespace. Reason: %s.\nRun the command `linkerd upgrade`, if you are looking to upgrade Linkerd.\n"
)

var (
	templatesCrdFiles = []string{
		"templates/policy/authorization-policy.yaml",
		"templates/policy/meshtls-authentication.yaml",
		"templates/policy/network-authentication.yaml",
		"templates/policy/server.yaml",
		"templates/policy/server-authorization.yaml",
		"templates/serviceprofile.yaml",
	}

	templatesControlPlaneStage = []string{
		"templates/namespace.yaml",
		"templates/identity-rbac.yaml",
		"templates/destination-rbac.yaml",
		"templates/heartbeat-rbac.yaml",
		"templates/proxy-injector-rbac.yaml",
		"templates/psp.yaml",
		"templates/config.yaml",
		"templates/identity.yaml",
		"templates/destination.yaml",
		"templates/heartbeat.yaml",
		"templates/proxy-injector.yaml",
	}

	ignoreCluster bool
)

/* Commands */
func newCmdInstall() *cobra.Command {
	values, err := l5dcharts.NewValues()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	var crds bool
	var options valuespkg.Options

	installOnlyFlags, installOnlyFlagSet := makeInstallFlags(values)
	installUpgradeFlags, installUpgradeFlagSet, err := makeInstallUpgradeFlags(values)
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}
	proxyFlags, proxyFlagSet := makeProxyFlags(values)

	flags := flattenFlags(installOnlyFlags, installUpgradeFlags, proxyFlags)

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
			return install(cmd.Context(), os.Stdout, values, flags, crds, options)
		},
	}

	cmd.Flags().AddFlagSet(installOnlyFlagSet)
	cmd.Flags().AddFlagSet(installUpgradeFlagSet)
	cmd.Flags().AddFlagSet(proxyFlagSet)
	cmd.Flags().BoolVar(&crds, "crds", false, "Install Linkerd CRDs")
	cmd.PersistentFlags().BoolVar(&ignoreCluster, "ignore-cluster", false,
		"Ignore the current Kubernetes cluster when checking for existing cluster configuration (default false)")

	flagspkg.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}

func install(ctx context.Context, w io.Writer, values *l5dcharts.Values, flags []flag.Flag, crds bool, options valuespkg.Options) error {
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
		// Ensure k8s is reachable
		if err := errAfterRunningChecks(values.CNIEnabled); err != nil {
			if healthcheck.IsCategoryError(err, healthcheck.KubernetesAPIChecks) {
				fmt.Fprintf(os.Stderr, errMsgCannotInitializeClient, err)
			}
			os.Exit(1)
		}

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

		if !crds {
			err = healthcheck.CheckCustomResourceDefinitions(ctx, k8sAPI, true)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Linkerd CRDs must be installed first. Run linkerd install with the --crds flag.")
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

	return render(w, values, crds, valuesOverrides)
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

func render(w io.Writer, values *l5dcharts.Values, crds bool, valuesOverrides map[string]interface{}) error {

	crdFiles := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
	}
	cpFiles := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
	}

	if crds {
		for _, template := range templatesCrdFiles {
			crdFiles = append(crdFiles,
				&loader.BufferedFile{Name: template},
			)
		}
		if err := charts.FilesReader(static.Templates, l5dcharts.HelmChartDirCrds+"/", crdFiles); err != nil {
			return err
		}
	} else {
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
	files := append(crdFiles, cpFiles...)
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
		return fmt.Errorf("failed to render the template: %w", err)
	}

	// Merge templates and inject
	var buf bytes.Buffer
	for _, tmpl := range chart.Templates {
		t := path.Join(chart.Metadata.Name, tmpl.Name)
		if _, err := buf.WriteString(renderedTemplates[t]); err != nil {
			return err
		}
	}

	if !crds {
		overrides, err := renderOverrides(vals, false)
		if err != nil {
			return err
		}
		buf.WriteString(yamlSep)
		buf.WriteString(string(overrides))
	}

	_, err = w.Write(buf.Bytes())
	if err == nil {
		fmt.Fprintln(os.Stderr, "Installing Linkerd CRDs...")
		fmt.Fprintln(os.Stderr, "Next, run `linkerd install | kubectl apply -f -` to install the control plane.")
		fmt.Fprintln(os.Stderr)
	}
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
			var ce healthcheck.CategoryError
			if errors.As(result.Err, &ce) && ce.Category == healthcheck.KubernetesAPIChecks {
				k8sAPIError = ce
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
