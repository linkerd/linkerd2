package cmd

import (
	"context"
	"fmt"
	"os"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/version"
	"github.com/spf13/cobra"
)

const DefaultVersionString = "unavailable"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the client and server version information",
	Long:  "Print the client and server version information.",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Client version: %s\n", version.Version)

		client, err := newPublicAPIClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error connecting to server: %s\n", err)
			return
		}

		fmt.Printf("Server version: %s\n", getServerVersion(client))

		return
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
	addControlPlaneNetworkingArgs(versionCmd)
}

func getServerVersion(client pb.ApiClient) string {
	resp, err := client.Version(context.Background(), &pb.Empty{})
	if err != nil {
		return DefaultVersionString
	}

	return resp.GetReleaseVersion()
}
