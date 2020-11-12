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

// ControlPlaneMetricsOptions holds values for command line flags that apply to the controlplane-metrics
// command.
type ControlPlaneMetricsOptions struct {
	wait time.Duration
}

// newControlPlaneMetricsOptions initializes controlplane-metrics options setting
// the max wait time duration as 30 seconds to fetch controlplane-metrics
//
// This option may be overridden on the CLI at run-time
func newControlPlaneMetricsOptions() *ControlPlaneMetricsOptions {
	return &ControlPlaneMetricsOptions{
		wait: 30 * time.Second,
	}
}

// newCmdDiagnostics creates a new cobra command `diagnostics` which contains commands to fetch Linkerd diagnostics
func newCmdDiagnostics() *cobra.Command {

	diagnosticsCmd := &cobra.Command{
		Use:     "diagnostics [flags]",
		Aliases: []string{"dg"},
		Args:    cobra.NoArgs,
		Short:   "Commands used to diagnose Linkerd components",
		Long: `Commands used to diagnose Linkerd components.

This command provides subcommands to diagnose the functionality of Linkerd.`,
		Example: `  # Get control-plane component metrics
  linkerd diagnostics cp-metrics

  # Get metrics from the web deployment in the emojivoto namespace.
  linkerd diagnostics proxy-metrics -n emojivoto deploy/web
 
  # get the endpoints for authorities in Linkerd's control-plane itself
  linkerd diagnostics endpoints linkerd-controller-api.linkerd.svc.cluster.local:8085
  `,
	}

	diagnosticsCmd.AddCommand(newCmdControlPlaneMetrics())
	diagnosticsCmd.AddCommand(newCmdEndpoints())
	diagnosticsCmd.AddCommand(newCmdMetrics())
	return diagnosticsCmd
}

// newCmdControlPlaneMetrics creates a new cobra command `controlplane-metrics` which contains commands to fetch control plane container's metrics
func newCmdControlPlaneMetrics() *cobra.Command {
	options := newControlPlaneMetricsOptions()

	cmd := &cobra.Command{
		Use:     "controlplane-metrics",
		Aliases: []string{"cp-metrics"},
		Short:   "Fetch metrics directly from the Linkerd control plane containers",
		Long: `Fetch metrics directly from Linkerd control plane containers.

  This command initiates port-forward to each control plane process, and
  queries the /metrics endpoint on them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			pods, err := k8sAPI.CoreV1().Pods(controlPlaneNamespace).List(cmd.Context(), metav1.ListOptions{})
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
