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

var shortVersion bool
var onlyClientVersion bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the client and server version information",
	Run: func(cmd *cobra.Command, args []string) {
		clientVersion := version.Version
		if shortVersion {
			fmt.Println(clientVersion)
		} else {
			fmt.Printf("Client version: %s\n", clientVersion)
		}

		conduitApiClient, err := newPublicAPIClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error connecting to server: %s\n", err)
			os.Exit(1)
		}

		if !onlyClientVersion {
			serverVersion := getServerVersion(conduitApiClient)
			if shortVersion {
				fmt.Println(serverVersion)
			} else {
				fmt.Printf("Server version: %s\n", serverVersion)
			}
		}

		return
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
	versionCmd.Args = cobra.NoArgs
	versionCmd.PersistentFlags().BoolVar(&shortVersion, "short", false, "Print the version number(s) only, with no additional output")
	versionCmd.PersistentFlags().BoolVar(&onlyClientVersion, "client", false, "Print the client version only")
}

func getServerVersion(client pb.ApiClient) string {
	resp, err := client.Version(context.Background(), &pb.Empty{})
	if err != nil {
		return DefaultVersionString
	}

	return resp.GetReleaseVersion()
}
