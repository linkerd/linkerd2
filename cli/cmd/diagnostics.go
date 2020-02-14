package cmd

import (
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
		Args:  cobra.NoArgs,
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

			for i, result := range results {
				fmt.Printf("#\n# POD %s (%d of %d)\n# CONTAINER %s \n#\n", result.pod, i+1, len(results), result.container)
				if result.err == nil {
					fmt.Printf("%s", result.metrics)
				} else {
					fmt.Printf("# ERROR %s\n", result.err)
				}
			}

			return nil
		},
	}

	cmd.Flags().DurationVarP(&options.wait, "wait", "w", options.wait, "Time allowed to fetch diagnostics")

	return cmd
}
