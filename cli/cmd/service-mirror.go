package cmd

import (
	"bytes"
	"io"
	"os"
	"text/template"

	"github.com/linkerd/linkerd2/cli/install"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
)

type installServiceMirrorOptions struct {
	Namespace string
	LinkerdVersion string
}

func newServiceMirrorOptions() *installServiceMirrorOptions {
	return &installServiceMirrorOptions{
		Namespace: "srv-mirror",
		LinkerdVersion:  version.Version,
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

	cmd.PersistentFlags().StringVarP(&options.LinkerdVersion, "linkerd-version", "v", options.LinkerdVersion, "Version")
	cmd.PersistentFlags().StringVarP(&options.Namespace, "namespace", "n", options.Namespace, "Namespace")
	return cmd
}


func renderServiceMirror(w io.Writer, config *installServiceMirrorOptions) error {
	template, err := template.New("linkerd-service-mirror").Parse(install.ServiceMirrorTemplate)
	if err != nil {
		return err
	}
	buf := &bytes.Buffer{}
	err = template.Execute(buf, config)
	if err != nil {
		return err
	}

	w.Write(buf.Bytes())
	w.Write([]byte("---\n"))

	return nil
}
