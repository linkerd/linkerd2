package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/linkerd/linkerd2/jaeger/static"
	"github.com/linkerd/linkerd2/pkg/charts"
	partials "github.com/linkerd/linkerd2/pkg/charts/static"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/engine"
)

var (
	// this doesn't include the namespace-metadata.* templates, which are Helm-only
	templatesJaeger = []string{
		"templates/namespace.yaml",
		"templates/jaeger-injector.yaml",
		"templates/jaeger-injector-policy.yaml",
		"templates/rbac.yaml",
		"templates/psp.yaml",
		"templates/tracing.yaml",
		"templates/tracing-policy.yaml",
	}
)

func newCmdInstall() *cobra.Command {
	var registry string
	var cniEnabled bool
	var skipChecks bool
	var ignoreCluster bool
	var wait time.Duration
	var options values.Options
	var output string

	// If LINKERD_DOCKER_REGISTRY is not null, use it as default registry path.
	// If --registry option is provided, it will override the env variable.
	defaultDockerRegistry := pkgcmd.DefaultDockerRegistry
	if regOverride := os.Getenv(flags.EnvOverrideDockerRegistry); regOverride != "" {
		defaultDockerRegistry = regOverride
	}

	cmd := &cobra.Command{
		Use:   "install [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes resources to install jaeger extension",
		Long:  `Output Kubernetes resources to install jaeger extension.`,
		Example: `  # Default install.
  linkerd jaeger install | kubectl apply -f -
  # Install Jaeger extension into a non-default namespace.
  linkerd jaeger install --namespace custom | kubectl apply -f -

The installation can be configured by using the --set, --values, --set-string and --set-file flags.
A full list of configurable values can be found at https://www.github.com/linkerd/linkerd2/tree/main/jaeger/charts/linkerd-jaeger/README.md
  `,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !skipChecks && !ignoreCluster {
				// Wait for the core control-plane to be up and running
				hc := healthcheck.NewWithCoreChecks(&healthcheck.Options{
					ControlPlaneNamespace: controlPlaneNamespace,
					KubeConfig:            kubeconfigPath,
					KubeContext:           kubeContext,
					Impersonate:           impersonate,
					ImpersonateGroup:      impersonateGroup,
					APIAddr:               apiAddr,
					RetryDeadline:         time.Now().Add(wait),
				})
				hc.RunWithExitOnError()
				cniEnabled = hc.CNIEnabled
			}

			return install(os.Stdout, options, registry, cniEnabled, output)
		},
	}

	cmd.Flags().StringVar(&registry, "registry", defaultDockerRegistry,
		fmt.Sprintf("Docker registry to pull jaeger-webhook image from ($%s)", flags.EnvOverrideDockerRegistry))
	cmd.Flags().BoolVar(&skipChecks, "skip-checks", false, `Skip checks for linkerd core control-plane existence`)
	cmd.Flags().BoolVar(&ignoreCluster, "ignore-cluster", false,
		"Ignore the current Kubernetes cluster when checking for existing cluster configuration (default false)")
	cmd.Flags().DurationVar(&wait, "wait", 300*time.Second, "Wait for core control-plane components to be available")
	cmd.PersistentFlags().StringVarP(&output, "output", "o", "yaml", "Output format. One of: json|yaml")

	flags.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}

func install(w io.Writer, options values.Options, registry string, cniEnabled bool, format string) error {

	// Create values override
	valuesOverrides, err := options.MergeValues(nil)
	if err != nil {
		return err
	}

	if cniEnabled {
		valuesOverrides["cniEnabled"] = true
	}

	// TODO: Add any validation logic here

	return render(w, valuesOverrides, registry, format)
}

func render(w io.Writer, valuesOverrides map[string]interface{}, registry string, format string) error {

	files := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: chartutil.ValuesfileName},
	}

	for _, template := range templatesJaeger {
		files = append(files,
			&loader.BufferedFile{Name: template},
		)
	}

	var partialFiles []*loader.BufferedFile
	for _, template := range charts.L5dPartials {
		partialFiles = append(partialFiles,
			&loader.BufferedFile{Name: template},
		)
	}

	// Load all jaeger chart files into buffer
	if err := charts.FilesReader(static.Templates, "linkerd-jaeger/", files); err != nil {
		return err
	}

	// Load all partial chart files into buffer
	if err := charts.FilesReader(partials.Templates, "", partialFiles); err != nil {
		return err
	}

	// Create a Chart obj from the files
	chart, err := loader.LoadFiles(append(files, partialFiles...))
	if err != nil {
		return err
	}

	vals, err := chartutil.CoalesceValues(chart, valuesOverrides)
	if err != nil {
		return err
	}

	vals, err = charts.InsertVersionValues(vals)
	if err != nil {
		return err
	}

	regOrig := vals["webhook"].(map[string]interface{})["image"].(map[string]interface{})["name"].(string)

	// registry variable can never be empty. The precedence are as:
	// 1. --registry
	// 2. EnvOverrideDockerRegistry
	// 3. DefaultDockerRegistry
	if registry != "" {
		vals["webhook"].(map[string]interface{})["image"].(map[string]interface{})["name"] = pkgcmd.RegistryOverride(regOrig, registry)
	}

	fullValues := map[string]interface{}{
		"Values": vals,
		"Release": map[string]interface{}{
			"Namespace": defaultJaegerNamespace,
			"Service":   "CLI",
		},
	}

	// Attach the final values into the `Values` field for rendering to work
	renderedTemplates, err := engine.Render(chart, fullValues)
	if err != nil {
		return err
	}

	// Merge templates and inject
	var buf bytes.Buffer
	for _, tmpl := range chart.Templates {
		t := path.Join(chart.Metadata.Name, tmpl.Name)
		if _, err := buf.WriteString(renderedTemplates[t]); err != nil {
			return err
		}
	}

	return pkgcmd.RenderYAMLAs(&buf, w, format)
}
