package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"

	jaeger "github.com/linkerd/linkerd2/jaeger/values"
	"github.com/linkerd/linkerd2/pkg/charts"
	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/spf13/cobra"
	"k8s.io/helm/pkg/chartutil"
	"sigs.k8s.io/yaml"
)

var (
	templatesJaeger = []string{
		"templates/namespace.yaml",
		"templates/rbac.yaml",
		"templates/tracing.yaml",
	}
)

func newCmdInstall() *cobra.Command {
	var skipChecks bool

	values, err := jaeger.NewValues()
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		os.Exit(1)
	}

	cmd := &cobra.Command{
		Use:   "install [flags]",
		Args:  cobra.NoArgs,
		Short: "Output Kubernetes resources to install jaeger extension",
		Long:  `Output Kubernetes resources to install jaeger extension.`,
		Example: `  # Default install.
  linkerd jaeger install | kubectl apply -f -

  # Install Jaeger extension into a non-default namespace.
  linkerd jaeger install --namespace custom | kubectl apply -f -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !skipChecks {
				// TODO: Add Checks for checking if linkerd exists
				// Also check for jaeger
			}

			return install(cmd.Context(), os.Stdout, values)
		},
	}

	cmd.Flags().BoolVar(
		&skipChecks, "skip-checks", false,
		`Skip checks for namespace existence`,
	)

	// TODO: Add --set flag set and also config

	return cmd
}

func install(ctx context.Context, w io.Writer, values *jaeger.Values) error {

	// TODO: Add any validation logic here

	return render(w, values)
}

func render(w io.Writer, values *jaeger.Values) error {
	// Render raw values and create chart config
	rawValues, err := yaml.Marshal(values)
	if err != nil {
		return err
	}

	files := []*chartutil.BufferedFile{
		{Name: chartutil.ChartfileName},
	}

	for _, template := range templatesJaeger {
		files = append(files,
			&chartutil.BufferedFile{Name: template},
		)
	}

	chart := &charts.Chart{
		Name:      "jaeger",
		Dir:       "jaeger",
		Namespace: values.Namespace,
		RawValues: rawValues,
		Files:     files,
		Fs:        http.Dir(path.Join(static.GetRepoRoot(), "jaeger/charts")),
	}
	buf, err := chart.Render()
	if err != nil {
		return err
	}

	_, err = w.Write(buf.Bytes())
	return err
}
