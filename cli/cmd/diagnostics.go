package cmd

import (
	"github.com/spf13/cobra"
)

const (
	adminHTTPPortName string = "admin-http"
)

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

	diagnosticsCmd.AddCommand(newCmdControllerMetrics())
	diagnosticsCmd.AddCommand(newCmdEndpoints())
	diagnosticsCmd.AddCommand(newCmdMetrics())
	return diagnosticsCmd
}
