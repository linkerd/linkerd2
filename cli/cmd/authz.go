package cmd

import (
	"fmt"
	"os"

	"github.com/linkerd/linkerd2/cli/table"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
)

func newCmdAuthz() *cobra.Command {

	var namespace string

	cmd := &cobra.Command{
		Use:   "authz [flags] resource",
		Short: "List authorizations for a resource",
		Long:  "List authorizations for a resource.",
		Args:  cobra.RangeArgs(1, 2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			if namespace == "" {
				namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}

			cc := k8s.NewCommandCompletion(k8sAPI, namespace)

			results, err := cc.Complete(args, toComplete)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			return results, cobra.ShellCompDirectiveDefault
		},
		RunE: func(cmd *cobra.Command, args []string) error {

			if namespace == "" {
				namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			var resource string
			if len(args) == 1 {
				resource = args[0]
			} else if len(args) == 2 {
				resource = args[0] + "/" + args[1]
			}

			rows := make([]table.Row, 0)

			authzs, err := k8s.AuthorizationsForResource(cmd.Context(), k8sAPI, namespace, resource)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get serverauthorization resources: %s\n", err)
				os.Exit(1)
			}

			for _, authz := range authzs {
				if authz.Route == "" {
					authz.Route = "*"
				}
				rows = append(rows, table.Row{authz.Route, authz.Server, authz.AuthorizationPolicy, authz.ServerAuthorization})
			}

			cols := []table.Column{
				{Header: "ROUTE", Width: 6, Flexible: true, LeftAlign: true},
				{Header: "SERVER", Width: 6, Flexible: true, LeftAlign: true},
				{Header: "AUTHORIZATION_POLICY", Width: 21, Flexible: true, LeftAlign: true},
				{Header: "SERVER_AUTHORIZATION", Width: 21, Flexible: true, LeftAlign: true},
			}

			table := table.NewTable(cols, rows)
			table.Render(os.Stdout)

			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Namespace of resource")

	pkgcmd.ConfigureNamespaceFlagCompletion(cmd, []string{"namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)
	return cmd
}
