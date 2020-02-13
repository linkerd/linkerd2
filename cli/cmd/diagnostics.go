package cmd

import (
	"fmt"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
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
		wait: 300 * time.Second,
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

			timeoutSeconds := int64(30)
			deployments, err := k8sAPI.AppsV1().Deployments(controlPlaneNamespace).List(metav1.ListOptions{TimeoutSeconds: &timeoutSeconds})
			if err != nil {
				return err
			}

			// ensure we can connect to the public API before fetching the diagnostics.
			checkPublicAPIClientOrRetryOrExit(time.Now().Add(options.wait), true)

			var pods []corev1.Pod
			for _, d := range deployments.Items {
				p, err := getPodsFor(k8sAPI, controlPlaneNamespace, "deploy/"+d.Name)
				if err != nil {
					continue
				}

				pods = append(pods, p...)
			}

			results := getMetrics(k8sAPI, pods, adminHTTPPortName, options.wait, verbose)

			for i, result := range results {
				fmt.Printf("#\n# POD %s (%d of %d)\n# CONTAINER %s (%d of %d)\n#\n", result.pod, i+1, len(results), result.container, i+1, len(results))
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
