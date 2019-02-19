package cmd

import (
	"bytes"
	"io"
	"os"
	"text/template"

	"github.com/linkerd/linkerd2/cli/installsp"
	"github.com/spf13/cobra"
)

type installSPConfig struct {
	Namespace string
}

func newCmdInstallSP() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install-sp [flags]",
		Short: "Output Kubernetes configs to install Linkerd Service Profiles",
		Long: `Output Kubernetes configs to install Linkerd Service Profiles.

This command installs Service Profiles into the Linkerd control plane. A
cluster-wide Linkerd control-plane is a prerequisite. To confirm Service Profile
support, verify "kubectl api-versions" outputs "linkerd.io/v1alpha1".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return renderSP(os.Stdout, controlPlaneNamespace)
		},
	}

	return cmd
}

func renderSP(w io.Writer, namespace string) error {
	template, err := template.New("linkerd").Parse(installsp.Template)
	if err != nil {
		return err
	}
	buf := &bytes.Buffer{}
	err = template.Execute(buf, map[string]string{"Namespace": namespace})
	if err != nil {
		return err
	}

	w.Write(buf.Bytes())
	w.Write([]byte("---\n"))

	return nil
}
