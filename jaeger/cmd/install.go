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
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli/values"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	var options values.Options

	// Inser Values file into options
	options.ValueFiles = []string{path.Join(static.GetRepoRoot(), "jaeger/charts", "jaeger", "values.yaml")}

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
				// Ensure there is a Linkerd installation.
				exists, err := checkIfLinkerdExists(cmd.Context())
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

	cmd.Flags().StringArrayVar(
		&options.Values, "set", []string{},
		"set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)",
	)
	return cmd
}

// TODO: Remove even this values and use Values.options for everything
func install(w io.Writer, options values.Options) error {

	var values jaeger.Values
	// Merge and create final set of values
	vals, err := options.MergeValues(nil)
	if err != nil {
		return err
	}

	// Convert vals map into Values struct
	rawMap, err := yaml.Marshal(vals)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(rawMap, &values)
	if err != nil {
		return err
	}

	// TODO: Add any validation logic here

	return render(w, &values)
}

func render(w io.Writer, values *jaeger.Values) error {
	// Render raw values and create chart config
	rawValues, err := yaml.Marshal(values)
	if err != nil {
		return err
	}

	files := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
	}

	for _, template := range templatesJaeger {
		files = append(files,
			&loader.BufferedFile{Name: template},
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

func checkIfLinkerdExists(ctx context.Context) (bool, error) {
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return false, err
	}

	_, err = kubeAPI.CoreV1().Namespaces().Get(ctx, controlPlaneNamespace, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	_, _, err = healthcheck.FetchCurrentConfiguration(ctx, kubeAPI, controlPlaneNamespace)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}
