package cmd

import (
	"bytes"
	"io"
	"os"
	"text/template"

	"github.com/linkerd/linkerd2/cli/installsp"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
)

func newCmdInstallSP() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install-sp [flags]",
		Short: "Output Kubernetes configs to install Linkerd Service Profiles",
		Long: `Output Kubernetes configs to install Linkerd Service Profiles.

This command installs Service Profiles into the Linkerd control plane. A
cluster-wide Linkerd control-plane is a prerequisite. To confirm Service Profile
support, verify "kubectl api-versions" outputs "linkerd.io/v1alpha2".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			_, values, err := healthcheck.FetchCurrentConfiguration(cmd.Context(), k8sAPI, controlPlaneNamespace)
			if err != nil {
				return err
			}

			clusterDomain := values.ClusterDomain
			if clusterDomain == "" {
				clusterDomain = defaultClusterDomain
			}

			return renderSP(os.Stdout, controlPlaneNamespace, clusterDomain)
		},
	}

	return cmd
}

func renderSP(w io.Writer, namespace, clusterDomain string) error {
	template, err := template.New("linkerd").Parse(installsp.Template)
	if err != nil {
		return err
	}
	buf := &bytes.Buffer{}
	err = template.Execute(buf, map[string]string{"Namespace": namespace, "ClusterDomain": clusterDomain})
	if err != nil {
		return err
	}

	w.Write(buf.Bytes())
	w.Write([]byte("---\n"))

	return nil
}
