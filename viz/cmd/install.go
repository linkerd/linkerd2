package cmd

import (
	"bytes"
	"io"
	"os"
	"path"
	"time"

	"github.com/linkerd/linkerd2/pkg/charts"
	partials "github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	api "github.com/linkerd/linkerd2/pkg/public"
	"github.com/linkerd/linkerd2/viz/static"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/engine"
)

var (
	templatesVIz = []string{
		"templates/namespace.yaml",
		"templates/metrics-api-rbac.yaml",
		"templates/grafana-rbac.yaml",
		"templates/prometheus-rbac.yaml",
		"templates/tap-rbac.yaml",
		"templates/web-rbac.yaml",
		"templates/psp.yaml",
		"templates/metrics-api.yaml",
		"templates/grafana.yaml",
		"templates/prometheus.yaml",
		"templates/tap.yaml",
		"templates/tap-injector-rbac.yaml",
		"templates/tap-injector.yaml",
		"templates/web.yaml",
	}
)

func newCmdInstall() *cobra.Command {
	var skipChecks bool
	var ha bool
	var wait time.Duration
	var options values.Options

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
		RunE: func(cmd *cobra.Command, args []string) error {
			if !skipChecks {
				// Wait for the core control-plane to be up and running
				api.CheckPublicAPIClientOrRetryOrExit(healthcheck.Options{
					ControlPlaneNamespace: controlPlaneNamespace,
					KubeConfig:            kubeconfigPath,
					KubeContext:           kubeContext,
					Impersonate:           impersonate,
					ImpersonateGroup:      impersonateGroup,
					APIAddr:               apiAddr,
					RetryDeadline:         time.Now().Add(wait),
				})

			}
			return install(os.Stdout, options, ha)
		},
	}

	cmd.Flags().BoolVar(&skipChecks, "skip-checks", false, `Skip checks for linkerd core control-plane existence`)
	cmd.Flags().BoolVar(&ha, "ha", false, `Install Viz Extension in High Availability mode.`)
	cmd.Flags().DurationVar(&wait, "wait", 300*time.Second, "Wait for core control-plane components to be available")

	flags.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}

func install(w io.Writer, options values.Options, ha bool) error {

	// Create values override
	valuesOverrides, err := options.MergeValues(nil)
	if err != nil {
		return err
	}

	// if using -L to specify a non-standard CP namespace, make sure
	// the linkerdNamespace Helm value is synced
	if controlPlaneNamespace != defaultLinkerdNamespace {
		valuesOverrides["linkerdNamespace"] = controlPlaneNamespace
	}

	if ha {
		valuesOverrides, err = charts.OverrideFromFile(valuesOverrides, static.Templates, vizChartName, "values-ha.yaml")
		if err != nil {
			return err
		}
	}

	// TODO: Add any validation logic here

	return render(w, valuesOverrides)
}

func render(w io.Writer, valuesOverrides map[string]interface{}) error {

	files := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: chartutil.ValuesfileName},
	}

	for _, template := range templatesVIz {
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

	// Load all Viz chart files into buffer
	if err := charts.FilesReader(static.Templates, vizChartName+"/", files); err != nil {
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

	// Attach the final values into the `Values` field for rendering to work
	renderedTemplates, err := engine.Render(chart, map[string]interface{}{"Values": vals})
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

	_, err = w.Write(buf.Bytes())
	return err
}
