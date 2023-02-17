package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/linkerd/linkerd2/pkg/charts"
	cnicharts "github.com/linkerd/linkerd2/pkg/charts/cni"
	"github.com/linkerd/linkerd2/pkg/charts/static"
	"github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/version"
	valuespkg "helm.sh/helm/v3/pkg/cli/values"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
)

var (
	// this doesn't include the namespace-metadata.* templates, which are Helm-only
	templatesCniFiles = []string{
		"templates/cni-plugin.yaml",
	}
)

func newCmdInstallCNIPlugin() *cobra.Command {
	var linkerdVersion string
	var registry string
	var wait time.Duration

	values, err := cnicharts.NewValues()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	var options valuespkg.Options

	cmd := &cobra.Command{
		Use:   "install-cni [flags]",
		Short: "Output Kubernetes configs to install Linkerd CNI",
		Long: `Output Kubernetes configs to install Linkerd CNI.

This command installs a DaemonSet into the Linkerd control plane. The DaemonSet
copies the necessary linkerd-cni plugin binaries and configs onto the host. It
assumes that the 'linkerd install' command will be executed with the
'--linkerd-cni-enabled' flag. This command needs to be executed before the
'linkerd install --linkerd-cni-enabled' command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return install(os.Stdout, values, options, registry)
		},
	}

	cmd.PersistentFlags().StringVarP(&linkerdVersion, "linkerd-version", "v", version.Version, "Tag to be used for Linkerd images")
	cmd.Flags().StringVar(&registry, "registry", "cr.l5d.io/linkerd",
		fmt.Sprintf("Docker registry to pull cni-plugin image from ($%s)", flags.EnvOverrideDockerRegistry))
	cmd.Flags().BoolVar(&ignoreCluster, "ignore-cluster", false,
		"Ignore the current Kubernetes cluster when checking for existing cluster configuration (default false)")
	cmd.Flags().DurationVar(&wait, "wait", 300*time.Second, "Wait for core control-plane components to be available")	

	flags.AddValueOptionsFlags(cmd.Flags(), &options)

	return cmd
}

func install(w io.Writer, values *cnicharts.Values, options valuespkg.Options, registry string) error {

	// Create values override
	valuesOverrides, err := options.MergeValues(nil)
	if err != nil {
		return err
	}

	// TODO: Add any validation logic here

	return renderCNIPlugin(w, values, valuesOverrides, registry)
}


func renderCNIPlugin(w io.Writer, values *cnicharts.Values, valuesOverrides map[string]interface{}, registry string) error {

	files := []*loader.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: chartutil.ValuesfileName},
	}

	for _, template := range templatesCniFiles {
		files = append(files, &loader.BufferedFile{Name: template})
	}

	// Load all linkerd-cni chart files into buffer
	if err := charts.FilesReader(static.Templates, cnicharts.HelmDefaultCNIChartDir +"/", files); err != nil {
		return err
	}

	valuesMap, err := values.ToMap()
	if err != nil {
		return err
	}
	// ....

	var partials []*loader.BufferedFile
	for _, template := range charts.L5dPartials {
		partials = append(partials, &loader.BufferedFile{Name: template})
	}
	if err := charts.FilesReader(static.Templates, "", partials); err != nil {
		return err
	}
	chart, err := loader.LoadFiles(append(files, partials...))
	if err != nil {
		return err
	}

	// Store final Values generated from values.yaml and CLI flags
	chart.Values = valuesMap

	vals, err := chartutil.CoalesceValues(chart, valuesOverrides)
	if err != nil {
		return err
	}

	regOrig := vals["webhook"].(map[string]interface{})["image"].(map[string]interface{})["name"].(string)
	if registry != "" {
		vals["webhook"].(map[string]interface{})["image"].(map[string]interface{})["name"] = cmd.RegistryOverride(regOrig, registry)
	}
	// env var overrides CLI flag
	if override := os.Getenv(flags.EnvOverrideDockerRegistry); override != "" {
		vals["webhook"].(map[string]interface{})["image"].(map[string]interface{})["name"] = cmd.RegistryOverride(regOrig, override)
	}

	fullValues := map[string]interface{}{
		"Values": vals,
		"Release": map[string]interface{}{
			"Namespace": defaultCNINamespace,
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

	_, err = w.Write(buf.Bytes())
	return err
}
