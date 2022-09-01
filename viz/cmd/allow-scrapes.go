package cmd

import (
	"bytes"
	"io"
	"os"
	"path"

	"github.com/linkerd/linkerd2/pkg/charts"
	partials "github.com/linkerd/linkerd2/pkg/charts/static"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/viz/static"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
)

const (
	allowScrapesRoutesPath string = "templates/allow-scrapes-routes.yaml"
	// a different policy for proxy metrics endpoints is used when installing
	// `linkerd-viz`, because the Prometheus instance must be allowed to scrape
	// its own proxy, which will not be TLS'd. therefore, the policy in the
	// `linkerd-viz` namespace currently allows anyone to scrape the metrics
	// endpoint, while the policies installed in other namespaces do not.
	allowScrapesVizPolicyPath string = "templates/allow-scrapes-viz-policy.yaml"
	allowScrapesPolicyPath    string = "templates/allow-scrapes-policy.yml"
)

// newCmdAllowScrapes creates a new cobra command `allow-scrapes`
func newCmdAllowScrapes() *cobra.Command {
	var targetNs string
	cmd := &cobra.Command{
		Use:   "allow-scrapes {-n | --namespace } namespace",
		Short: "Output Kubernetes resources to authorize Prometheus scrapes",
		Long:  `Output Kubernetes resources to authorize Prometheus scrapes in a namespace or cluster with config.linkerd.io/default-inbound-policy: deny.`,
		Example: `# Allow scrapes in the 'emojivoto' namespace
linkerd viz allow-scrapes --namespace emojivoto | kubectl apply -f -`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return cmd.MarkFlagRequired("namespace")
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return renderAllowScrapes(os.Stdout, targetNs)
		},
	}
	cmd.Flags().StringVarP(&targetNs, "namespace", "n", targetNs, "The namespace in which to authorize Prometheus scrapes.")

	pkgcmd.ConfigureNamespaceFlagCompletion(
		cmd, []string{"n", "namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)
	return cmd
}

func renderAllowScrapes(w io.Writer, namespace string) error {
	files := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: chartutil.ValuesfileName},
		{Name: allowScrapesRoutesPath},
		{Name: allowScrapesPolicyPath},
	}

	var partialFiles []*loader.BufferedFile
	for _, template := range charts.L5dPartials {
		partialFiles = append(partialFiles,
			&loader.BufferedFile{Name: template},
		)
	}

	if err := charts.FilesReader(static.Templates, vizChartName+"/", files); err != nil {
		return err
	}

	if err := charts.FilesReader(partials.Templates, "", partialFiles); err != nil {
		return err
	}

	// Create a Chart obj from the files
	chart, err := loader.LoadFiles(append(files, partialFiles...))
	if err != nil {
		return err
	}

	vals, err := chartutil.CoalesceValues(chart, make(map[string]interface{}))
	if err != nil {
		return err
	}

	vals, err = charts.InsertVersionValues(vals)
	if err != nil {
		return err
	}

	fullValues := map[string]interface{}{
		"Values": vals,
		"Release": map[string]interface{}{
			// set this to the namespace we want to create the resources in, *not*
			// the linkerd-viz namespace.
			"Namespace":    namespace,
			"VizNamespace": defaultNamespace,
			"Service":      "CLI",
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

	_, err = w.Write(buf.Bytes())
	return err
}
