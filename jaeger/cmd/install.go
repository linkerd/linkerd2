package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/linkerd/linkerd2/jaeger/static"
	"github.com/linkerd/linkerd2/pkg/charts"
	partials "github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/engine"
)

var (
	templatesJaeger = []string{
		"templates/namespace.yaml",
		"templates/proxy-mutator.yaml",
		"templates/rbac.yaml",
		"templates/tracing.yaml",
	}
)

func newCmdInstall() *cobra.Command {
	var skipChecks bool
	var options values.Options

	cmd := &cobra.Command{
		Use:   "install [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes resources to install jaeger extension",
		Long:  `Output Kubernetes resources to install jaeger extension.`,
		Example: `  # Default install.
  linkerd jaeger install | kubectl apply -f -
  # Install Jaeger extension into a non-default namespace.
  linkerd jaeger install --namespace custom | kubectl apply -f -
  
Configurable fields can be found at https://www.github.com/linkerd/linkerd2/tree/main/jaeger/charts/jaeger
  `,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !skipChecks {
				// Ensure there is a Linkerd installation.
				kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
				if err != nil {
					return err
				}

				exists, err := healthcheck.CheckIfLinkerdExists(cmd.Context(), kubeAPI, controlPlaneNamespace)
				if err != nil {
					return fmt.Errorf("could not check for Linkerd existence: %s", err)
				}

				if !exists {
					return fmt.Errorf("could not find a Linkerd installation")
				}
			}

			return install(os.Stdout, options)
		},
	}

	cmd.Flags().BoolVar(
		&skipChecks, "skip-checks", false,
		`Skip checks for namespace existence`,
	)

	flags.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}

func install(w io.Writer, options values.Options) error {

	// Create values override
	valuesOverrides, err := options.MergeValues(nil)
	if err != nil {
		return err
	}

	// TODO: Add any validation logic here

	return render(w, valuesOverrides)
}

func render(w io.Writer, valuesOverrides map[string]interface{}) error {

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
	if err := charts.FilesReader(static.Templates, "jaeger/", files); err != nil {
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
