package cmd

import (
	"context"
	"fmt"

	"github.com/runconduit/conduit/cli/k8s"
	"github.com/runconduit/conduit/cli/shell"

	"github.com/runconduit/conduit/controller"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the client and server version information",
	Long:  "Print the client and server version information.",
	Args:  cobra.NoArgs,
	Run: exitSilentlyOnError(func(cmd *cobra.Command, args []string) error {
		fmt.Println("Client version: " + controller.Version)

		serverVersion, err := getVersion()
		if err != nil {
			serverVersion = "unavailable"
		}
		fmt.Println("Server version: " + serverVersion)

		return err
	}),
}

func init() {
	RootCmd.AddCommand(versionCmd)
	addControlPlaneNetworkingArgs(versionCmd)
}

func getVersion() (string, error) {
	kubeApi, err := k8s.MakeK8sAPi(shell.MakeUnixShell(), kubeconfigPath, apiAddr)
	if err != nil {
		return "", err
	}

	client, err := newApiClient(kubeApi)
	if err != nil {
		return "", err
	}
	resp, err := client.Version(context.Background(), &pb.Empty{})
	if err != nil {
		return "", err
	}
	return resp.GetReleaseVersion(), nil
}
