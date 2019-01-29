package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/linkerd/linkerd2/controller/api/public"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/spf13/cobra"
)

const defaultVersionString = "unavailable"

type versionOptions struct {
	shortVersion      bool
	onlyClientVersion bool
}

func newVersionOptions() *versionOptions {
	return &versionOptions{
		shortVersion:      false,
		onlyClientVersion: false,
	}
}

func newCmdVersion() *cobra.Command {
	options := newVersionOptions()

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the client and server version information",
		Run: func(cmd *cobra.Command, args []string) {
			configureAndRunVersion(options, os.Stdout, os.Stderr, newVersionClient)
		},
	}

	cmd.Args = cobra.NoArgs
	cmd.PersistentFlags().BoolVar(&options.shortVersion, "short", options.shortVersion, "Print the version number(s) only, with no additional output")
	cmd.PersistentFlags().BoolVar(&options.onlyClientVersion, "client", options.onlyClientVersion, "Print the client version only")

	return cmd
}

func configureAndRunVersion(
	options *versionOptions,
	stdout io.Writer,
	stderr io.Writer,
	mkClient func() (pb.ApiClient, error),
) {
	clientVersion := version.Version
	if options.shortVersion {
		fmt.Fprintln(stdout, clientVersion)
	} else {
		fmt.Fprintf(stdout, "Client version: %s\n", clientVersion)
	}

	if !options.onlyClientVersion {
		client, err := mkClient()
		if err != nil {
			fmt.Fprintf(stderr, "Error connecting to server: %s\n", err)
			os.Exit(1)
		}

		serverVersion, err := healthcheck.GetServerVersion(client)
		if err != nil {
			serverVersion = defaultVersionString
		}
		if options.shortVersion {
			fmt.Fprintln(stdout, serverVersion)
		} else {
			fmt.Fprintf(stdout, "Server version: %s\n", serverVersion)
		}
	}
}

// This client does not do any validation
func newVersionClient() (pb.ApiClient, error) {
	if apiAddr != "" {
		return public.NewInternalClient(controlPlaneNamespace, apiAddr)
	}
	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext)
	if err != nil {
		return nil, err
	}
	return public.NewExternalClient(controlPlaneNamespace, kubeAPI)
}
