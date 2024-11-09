package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/linkerd/linkerd2/cli/flag"
	"github.com/linkerd/linkerd2/pkg/charts"
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/charts/static"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
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
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"
)

const (
	helmDefaultChartNameCrds = "linkerd-crds"
	helmDefaultChartNameCP   = "linkerd-control-plane"

	errMsgCannotInitializeClient = `Unable to install the Linkerd control plane. Cannot connect to the Kubernetes cluster:

%s

You can use the --ignore-cluster flag if you just want to generate the installation config.
`

	errMsgLinkerdConfigResourceConflict = "Can't install the Linkerd control plane in the '%s' namespace. Reason: %s.\nRun the command `linkerd upgrade`, if you are looking to upgrade Linkerd.\n"
)

var (
	TemplatesCrdFiles = []string{
		"templates/policy/authorization-policy.yaml",
		"templates/policy/egress-network.yaml",
		"templates/policy/http-local-ratelimit-policy.yaml",
		"templates/policy/httproute.yaml",
		"templates/policy/meshtls-authentication.yaml",
		"templates/policy/network-authentication.yaml",
		"templates/policy/server-authorization.yaml",
		"templates/policy/server.yaml",
		"templates/serviceprofile.yaml",
		"templates/gateway.networking.k8s.io_httproutes.yaml",
		"templates/gateway.networking.k8s.io_grpcroutes.yaml",
		"templates/gateway.networking.k8s.io_tlsroutes.yaml",
		"templates/gateway.networking.k8s.io_tcproutes.yaml",
		"templates/workload/external-workload.yaml",
	}

	TemplatesControlPlane = []string{
		"templates/namespace.yaml",
		"templates/identity-rbac.yaml",
		"templates/destination-rbac.yaml",
		"templates/heartbeat-rbac.yaml",
		"templates/podmonitor.yaml",
		"templates/proxy-injector-rbac.yaml",
		"templates/psp.yaml",
		"templates/config.yaml",
		"templates/config-rbac.yaml",
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
	var output string

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
		Example: `  # Install CRDs first.
  linkerd install --crds | kubectl apply -f -

  # Install the core control plane.
  linkerd install | kubectl apply -f -

The installation can be configured by using the --set, --values, --set-string and --set-file flags.
A full list of configurable values can be found at https://artifacthub.io/packages/helm/linkerd2/linkerd-control-plane#values`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var k8sAPI *k8s.KubernetesAPI
			if !ignoreCluster {
				// Ensure k8s is reachable
				if err := errAfterRunningChecks(values.CNIEnabled); err != nil {
					fmt.Fprintf(os.Stderr, errMsgCannotInitializeClient, err)
					os.Exit(1)
				}

				// Ensure there is not already an existing Linkerd installation.
				k8sAPI, err = k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 30*time.Second)
				if err != nil {
					return err
				}

				if !crds {
					crds := bytes.Buffer{}
					err := renderCRDs(&crds, options, "yaml")
					if err != nil {
						fmt.Fprintf(os.Stderr, "%q", err)
						os.Exit(1)
					}
					err = healthcheck.CheckCustomResourceDefinitions(cmd.Context(), k8sAPI, crds.String())
					if err != nil {
						fmt.Fprintln(os.Stderr, "Linkerd CRDs must be installed first. Run linkerd install with the --crds flag.")
						os.Exit(1)
					}
				}
			}

			if crds {
				// The CRD chart is not configurable.
				// TODO(ver): Error if values have been configured?
				if err = installCRDs(cmd.Context(), k8sAPI, os.Stdout, options, output); err != nil {
					return err
				}

				fmt.Fprintln(os.Stderr, "Rendering Linkerd CRDs...")
				fmt.Fprintln(os.Stderr, "Next, run `linkerd install | kubectl apply -f -` to install the control plane.")
				fmt.Fprintln(os.Stderr)
				return nil
			}

			return installControlPlane(cmd.Context(), k8sAPI, os.Stdout, values, flags, options, output)
		},
	}

	cmd.Flags().AddFlagSet(installOnlyFlagSet)
	cmd.Flags().AddFlagSet(installUpgradeFlagSet)
	cmd.Flags().AddFlagSet(proxyFlagSet)
	cmd.Flags().BoolVar(&crds, "crds", false, "Install Linkerd CRDs")
	cmd.PersistentFlags().BoolVar(&ignoreCluster, "ignore-cluster", false,
		"Ignore the current Kubernetes cluster when checking for existing cluster configuration (default false)")
	cmd.PersistentFlags().StringVarP(&output, "output", "o", "yaml", "Output format. One of: json|yaml")

	flagspkg.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}

func checkNoConfig(ctx context.Context, k8sAPI *k8s.KubernetesAPI) error {
	if k8sAPI == nil {
		// When `ingoreCluster` is set, there is no k8sAPI.
		return nil
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

	return nil
}

func installCRDs(ctx context.Context, k8sAPI *k8s.KubernetesAPI, w io.Writer, options valuespkg.Options, format string) error {
	if err := checkNoConfig(ctx, k8sAPI); err != nil {
		return err
	}

	return renderCRDs(w, options, format)
}

func installControlPlane(ctx context.Context, k8sAPI *k8s.KubernetesAPI, w io.Writer, values *l5dcharts.Values, flags []flag.Flag, options valuespkg.Options, format string) error {
	err := flag.ApplySetFlags(values, flags)
	if err != nil {
		return err
	}

	if err := checkNoConfig(ctx, k8sAPI); err != nil {
		return err
	}

	// Create values override
	valuesOverrides, err := options.MergeValues(nil)
	if err != nil {
		return err
	}

	if k8sAPI != nil {
		// We just want to check if `linkerd-configmap` exists
		_, err = k8sAPI.CoreV1().ConfigMaps(controlPlaneNamespace).Get(ctx, k8s.ConfigConfigMapName, metav1.GetOptions{})
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

		// Check 'kubernetes' service in default namespace to see what ports the API
		// Server listens on. If the ports are different from the default ('443,6443')
		// then replace with ports from the service spec.
		apiSrvPorts := getApiServerPorts(ctx, k8sAPI)
		if apiSrvPorts != "" {
			values.ProxyInit.KubeAPIServerPorts = apiSrvPorts
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

	return renderControlPlane(w, values, valuesOverrides, format)
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

// renderChartToBuffer takes a slice of loaded template files and configuration values and renders
// them into a buffer. The coalesced values are also returned so that they may be rendered via
// `renderOverrides` if appropriate.
func renderChartToBuffer(files []*loader.BufferedFile, values map[string]interface{}, valuesOverrides map[string]interface{}) (*bytes.Buffer, chartutil.Values, error) {
	// Load the partials in addition to the main chart.
	var partials []*loader.BufferedFile
	for _, template := range charts.L5dPartials {
		partials = append(partials, &loader.BufferedFile{Name: template})
	}
	if err := charts.FilesReader(static.Templates, "", partials); err != nil {
		return nil, nil, err
	}
	chart, err := loader.LoadFiles(append(files, partials...))
	if err != nil {
		return nil, nil, err
	}

	// Store final Values generated from values.yaml and CLI flags
	chart.Values = values

	vals, err := chartutil.CoalesceValues(chart, valuesOverrides)
	if err != nil {
		return nil, nil, err
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
		return nil, nil, fmt.Errorf("failed to render the template: %w", err)
	}

	// Merge templates and inject
	var buf bytes.Buffer
	for _, tmpl := range chart.Templates {
		t := path.Join(chart.Metadata.Name, tmpl.Name)
		if _, err := buf.WriteString(renderedTemplates[t]); err != nil {
			return nil, nil, err
		}
	}

	return &buf, vals, nil
}

func renderCRDs(w io.Writer, options valuespkg.Options, format string) error {
	files := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
	}
	for _, template := range TemplatesCrdFiles {
		files = append(files, &loader.BufferedFile{Name: template})
	}
	if err := charts.FilesReader(static.Templates, l5dcharts.HelmChartDirCrds+"/", files); err != nil {
		return err
	}

	// Load defaults from values.yaml
	valuesFile := &loader.BufferedFile{Name: l5dcharts.HelmChartDirCrds + "/values.yaml"}
	if err := charts.ReadFile(static.Templates, "/", valuesFile); err != nil {
		return err
	}
	// Ensure the map is not nil, even if the default `values.yaml` is empty ---
	// if there are no values in the YAML file, `yaml.Unmarshal` will not
	// allocate the map, and the subsequent assignment to `cliVersion` will
	// panic because the map is nil.
	defaultValues := make(map[string]interface{})
	err := yaml.Unmarshal(valuesFile.Data, &defaultValues)
	if err != nil {
		return err
	}
	defaultValues["cliVersion"] = k8s.CreatedByAnnotationValue()

	// Create values override
	valuesOverrides, err := options.MergeValues(nil)
	if err != nil {
		return err
	}

	buf, _, err := renderChartToBuffer(files, defaultValues, valuesOverrides)
	if err != nil {
		return err
	}

	return pkgcmd.RenderYAMLAs(buf, w, format)
}

func renderControlPlane(w io.Writer, values *l5dcharts.Values, valuesOverrides map[string]interface{}, format string) error {
	files := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
	}
	for _, template := range TemplatesControlPlane {
		files = append(files, &loader.BufferedFile{Name: template})
	}
	if err := charts.FilesReader(static.Templates, l5dcharts.HelmChartDirCP+"/", files); err != nil {
		return err
	}

	valuesMap, err := values.ToMap()
	if err != nil {
		return err
	}
	buf, vals, err := renderChartToBuffer(files, valuesMap, valuesOverrides)
	if err != nil {
		return err
	}

	overrides, err := renderOverrides(vals, false)
	if err != nil {
		return err
	}
	buf.WriteString(yamlSep)
	buf.WriteString(string(overrides))

	return pkgcmd.RenderYAMLAs(buf, w, format)
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

	var err error
	hc.RunChecks(func(result *healthcheck.CheckResult) {
		if result.Err != nil {
			err = result.Err
		}
	})

	return err
}

// getApiServerPorts looks at the 'kubernetes' service in the 'default'
// namespace and returns the ClusterIP port for the API Server (by default 443),
// and the port that the API Server backend is expecting TLS connections on (by
// default 6443.)
func getApiServerPorts(ctx context.Context, api *k8s.KubernetesAPI) string {
	service, err := api.CoreV1().Services("default").Get(ctx, "kubernetes", metav1.GetOptions{})
	if err != nil {
		return ""
	}

	ports := make([]string, 0)
	for _, port := range service.Spec.Ports {
		ports = append(ports, strconv.Itoa(int(port.Port)))
		// We only care about int ports since string ports (e.g targetPort: web)
		// correspond to a named port in a pod spec.
		if port.TargetPort.Type == intstr.Int {
			ports = append(ports, strconv.Itoa(port.TargetPort.IntValue()))
		}
	}

	return strings.Join(ports, ",")
}
