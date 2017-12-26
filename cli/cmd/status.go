package cmd

import (
	"io"
	"log"
	"os"

	"github.com/runconduit/conduit/cli/k8s"
	"github.com/runconduit/conduit/cli/shell"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check your Conduit installation for potential problems.",
	Long: `Check your Conduit installation for potential problems. The status command will perform various checks of your
local system, the Conduit control plane, and connectivity between those. The process will exit with non-zero status if
problems were found.`,
	Args: cobra.NoArgs,
	Run: exitSilentlyOnError(func(cmd *cobra.Command, args []string) error {

		kubeApi, err := k8s.MakeK8sAPi(shell.MakeUnixShell(), kubeconfigPath, apiAddr)
		if err != nil {
			return err
		}

		client, err := newApiClient(kubeApi)
		if err != nil {
			return err
		}

		kubectl, err := k8s.MakeKubectl(shell.MakeUnixShell())
		if err != nil {
			log.Fatalf("Failed to start kubectl: %v", err)
		}

		return checkStatus(os.Stdout, kubeApi, client, kubectl)
	}),
}

func checkStatus(w io.Writer, api k8s.KubernetesApi, client pb.ApiClient, kubectl k8s.Kubectl) error {
	return nil
}

func init() {
	RootCmd.AddCommand(statusCmd)
	addControlPlaneNetworkingArgs(statusCmd)
}
