package cmd

import (
	"bytes"
	"io"
	"os"
	"path"
	"time"

	chartspkg "github.com/linkerd/linkerd2/pkg/charts"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/viz/charts"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/engine"
)

var (
	// this doesn't include the namespace-metadata.* templates, which are Helm-only
	templatesViz = []string{
		"templates/namespace.yaml",
		"templates/metrics-api-rbac.yaml",
		"templates/prometheus-rbac.yaml",
		"templates/tap-rbac.yaml",
		"templates/web-rbac.yaml",
		"templates/psp.yaml",
		"templates/metrics-api.yaml",
		"templates/metrics-api-policy.yaml",
		"templates/admin-policy.yaml",
		"templates/prometheus.yaml",
		"templates/prometheus-policy.yaml",
		"templates/tap.yaml",
		"templates/tap-policy.yaml",
		"templates/tap-injector-rbac.yaml",
		"templates/tap-injector.yaml",
		"templates/tap-injector-policy.yaml",
		"templates/web.yaml",
		"templates/service-profiles.yaml",
	}
)

func newCmdInstall() *cobra.Command {
	var skipChecks bool
	var ignoreCluster bool
	var ha bool
	var cniEnabled bool
	var wait time.Duration
	var options values.Options
	var output string

	cmd := &cobra.Command{
		Use:   "install [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes resources to install linkerd-viz extension",
		Long:  `Output Kubernetes resources to install linkerd-viz extension.`,
		Example: `  # Default install.
  linkerd viz install | kubectl apply -f -

The installation can be configured by using the --set, --values, --set-string and --set-file flags.
A full list of configurable values can be found at https://www.github.com/linkerd/linkerd2/tree/main/viz/charts/linkerd-viz/README.md
  `,
		RunE: func(_ *cobra.Command, _ []string) error {
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
			return install(os.Stdout, options, ha, cniEnabled, output)
		},
	}

	cmd.Flags().BoolVar(&skipChecks, "skip-checks", false, `Skip checks for linkerd core control-plane existence`)
	cmd.Flags().BoolVar(&ignoreCluster, "ignore-cluster", false,
		"Ignore the current Kubernetes cluster when checking for existing cluster configuration (default false)")
	cmd.Flags().BoolVar(&ha, "ha", false, `Install Viz Extension in High Availability mode.`)
	cmd.Flags().DurationVar(&wait, "wait", 300*time.Second, "Wait for core control-plane components to be available")
	cmd.PersistentFlags().StringVarP(&output, "output", "o", "yaml", "Output format. One of: json|yaml")

	flags.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}

func install(w io.Writer, options values.Options, ha, cniEnabled bool, format string) error {

	// Create values override
	valuesOverrides, err := options.MergeValues(nil)
	if err != nil {
		return err
	}

	// sync values overrides with Helm values
	if controlPlaneNamespace != defaultLinkerdNamespace {
		valuesOverrides["linkerdNamespace"] = controlPlaneNamespace
	}
	if reg := os.Getenv(flags.EnvOverrideDockerRegistry); reg != "" {
		valuesOverrides["defaultRegistry"] = reg
	}

	if ha {
		valuesOverrides, err = chartspkg.OverrideFromFile(valuesOverrides, charts.Templates, VizChartName, "values-ha.yaml")
		if err != nil {
			return err
		}
	}

	if cniEnabled {
		valuesOverrides["cniEnabled"] = true
	}

	// TODO: Add any validation logic here

	return render(w, valuesOverrides, format)
}

func render(w io.Writer, valuesOverrides map[string]interface{}, format string) error {

	files := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: chartutil.ValuesfileName},
	}

	for _, template := range templatesViz {
		files = append(files,
			&loader.BufferedFile{Name: template},
		)
	}

	// Load all Viz chart files into buffer
	if err := chartspkg.FilesReader(charts.Templates, VizChartName+"/", files); err != nil {
		return err
	}

	partialFiles, err := chartspkg.LoadPartials()
	if err != nil {
		return err
	}

	// Create a Chart obj from the files
	chart, err := loader.LoadFiles(append(files, partialFiles...))
	if err != nil {
		return err
	}
	println("Built chart with files:")
	for _, f := range files {
		println("  -", f.Name)
	}
	for _, f := range partialFiles {
		println("  -", f.Name)
	}

	vals, err := chartutil.CoalesceValues(chart, valuesOverrides)
	if err != nil {
		return err
	}

	vals, err = chartspkg.InsertVersionValues(vals)
	if err != nil {
		return err
	}

	fullValues := map[string]interface{}{
		"Values": vals,
		"Release": map[string]interface{}{
			"Namespace": defaultNamespace,
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
