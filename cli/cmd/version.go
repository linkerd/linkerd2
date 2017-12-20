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

const DefaultVersionString = "unavailable"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the client and server version information",
	Long:  "Print the client and server version information.",
	Args:  cobra.NoArgs,
	Run: exitSilentlyOnError(func(cmd *cobra.Command, args []string) error {

		kubeApi, err := k8s.MakeK8sAPi(shell.MakeUnixShell(), kubeconfigPath, apiAddr)
		if err != nil {
			return err
		}

		client, err := newApiClient(kubeApi)
		if err != nil {
			return err
		}

		versions := getVersions(client)

		fmt.Printf("Client version: %s\n", versions.Client)
		fmt.Printf("Server version: %s\n", versions.Server)

		return err
	}),
}

func init() {
	RootCmd.AddCommand(versionCmd)
	addControlPlaneNetworkingArgs(versionCmd)
}

type versions struct {
	Server string
	Client string
}

func getVersions(client pb.ApiClient) versions {
	resp, err := client.Version(context.Background(), &pb.Empty{})
	if err != nil {
		return versions{
			Client: controller.Version,
			Server: DefaultVersionString,
		}
	}

	versions := versions{
		Server: resp.GetReleaseVersion(),
		Client: controller.Version,
	}

	return versions
}
