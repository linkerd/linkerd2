package cmd

import (
	"bytes"
	"fmt"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	adminHTTPPortName string = "admin-http"
)

type diagnosticsOptions struct {
	wait time.Duration
}

func newDiagnosticsOptions() *diagnosticsOptions {
	return &diagnosticsOptions{
		wait: 30 * time.Second,
	}
}

func newCmdDiagnostics() *cobra.Command {
	options := newDiagnosticsOptions()

	cmd := &cobra.Command{
		Use:   "diagnostics",
		Short: "Fetch metrics directly from the Linkerd control plane containers",
		Long: `Fetch metrics directly from Linkerd control plane containers.

  This command initiates port-forward to each control plane process, and
  queries the /metrics endpoint on them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			pods, err := k8sAPI.CoreV1().Pods(controlPlaneNamespace).List(metav1.ListOptions{})
			if err != nil {
				return err
			}

			results := getMetrics(k8sAPI, pods.Items, adminHTTPPortName, options.wait, verbose)

			var buf bytes.Buffer
			for i, result := range results {
				content := fmt.Sprintf("#\n# POD %s (%d of %d)\n", result.pod, i+1, len(results))
				if result.err != nil {
					content += fmt.Sprintf("# ERROR %s\n", result.err)
				} else {
					content += fmt.Sprintf("# CONTAINER %s \n#\n", result.container)
					content += string(result.metrics)
				}
				buf.WriteString(content)
			}
			fmt.Printf("%s", buf.String())

			return nil
		},
	}

	cmd.Flags().DurationVarP(&options.wait, "wait", "w", options.wait, "Time allowed to fetch diagnostics")

	return cmd
}
