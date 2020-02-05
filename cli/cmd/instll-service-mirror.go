package cmd

import (
	"github.com/linkerd/linkerd2/pkg/charts"
	service_mirror "github.com/linkerd/linkerd2/pkg/charts/service-mirror"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
	"io"
	"k8s.io/helm/pkg/chartutil"
	"os"
	"sigs.k8s.io/yaml"
)

type installServiceMirrorOptions struct {
	Namespace string
}

const helmServiceMirrorDefaultChartName = "linkerd2-service-mirror"

func newServiceMirrorOptions() *installServiceMirrorOptions {
	return &installServiceMirrorOptions{
		Namespace: "srv-mirror",
	}
}

func newCmdInstallServiceMirror() *cobra.Command {
	options := newServiceMirrorOptions()

	cmd := &cobra.Command{
		Use:   "install-service-mirror [flags]",
		Short: "Output Kubernetes configs to install Linkerd Service Mirror",
		Long:  "Output Kubernetes configs to install Linkerd Service Mirror",
		RunE: func(cmd *cobra.Command, args []string) error {
			return renderServiceMirror(os.Stdout, options)
		},
	}

	cmd.PersistentFlags().StringVarP(&options.Namespace, "namespace", "n", options.Namespace, "Namespace")
	return cmd
}

func renderServiceMirror(w io.Writer, config *installServiceMirrorOptions) error {

	values, err := service_mirror.NewValues()
	if err != nil {
		return err
	}
	values.Namespace = config.Namespace
	values.ServiceMirrorVersion = version.Version

	// Render raw values and create chart config
	rawValues, err := yaml.Marshal(values)
	if err != nil {
		return err
	}

	files := []*chartutil.BufferedFile{
		{Name: chartutil.ChartfileName},
		{Name: "templates/service-mirror.yaml"},
	}

	chart := &charts.Chart{
		Name:      helmServiceMirrorDefaultChartName,
		Dir:       helmServiceMirrorDefaultChartName,
		Namespace: controlPlaneNamespace,
		RawValues: rawValues,
		Files:     files,
	}
	buf, err := chart.RenderServiceMirror()
	if err != nil {
		return err
	}
	w.Write(buf.Bytes())
	w.Write([]byte("---\n"))

	return nil

}
