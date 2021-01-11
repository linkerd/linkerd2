package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	publicPb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/version"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/spf13/cobra"
)

const defaultVersionString = "unavailable"

type versionOptions struct {
	shortVersion      bool
	onlyClientVersion bool
	proxy             bool
	namespace         string
}

func newVersionOptions() *versionOptions {
	return &versionOptions{
		shortVersion:      false,
		onlyClientVersion: false,
		proxy:             false,
		namespace:         "",
	}
}

func newCmdVersion() *cobra.Command {
	options := newVersionOptions()

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the client and server version information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			configureAndRunVersion(cmd.Context(), options, os.Stdout, rawPublicAPIClient, rawVizAPIClient)
		},
	}

	cmd.PersistentFlags().BoolVar(&options.shortVersion, "short", options.shortVersion, "Print the version number(s) only, with no additional output")
	cmd.PersistentFlags().BoolVar(&options.onlyClientVersion, "client", options.onlyClientVersion, "Print the client version only")
	cmd.PersistentFlags().BoolVar(&options.proxy, "proxy", options.proxy, "Print data-plane versions")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace to use for --proxy versions (default: all namespaces)")

	return cmd
}

func configureAndRunVersion(
	ctx context.Context,
	options *versionOptions,
	stdout io.Writer,
	mkPublicClient func(ctx context.Context) (publicPb.ApiClient, error),
	mkVizClient func(ctx context.Context) (pb.ApiClient, error),
) {
	clientVersion := version.Version
	if options.shortVersion {
		fmt.Fprintln(stdout, clientVersion)
	} else {
		fmt.Fprintf(stdout, "Client version: %s\n", clientVersion)
	}

	if !options.onlyClientVersion {
		serverVersion := defaultVersionString
		publicClient, clientErr := mkPublicClient(ctx)
		if clientErr == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var err error
			serverVersion, err = healthcheck.GetServerVersion(ctx, publicClient)
			if err != nil {
				serverVersion = defaultVersionString
			}
		}

		if options.shortVersion {
			fmt.Fprintln(stdout, serverVersion)
		} else {
			fmt.Fprintf(stdout, "Server version: %s\n", serverVersion)
		}

		if options.proxy {
			vizClient, clientErr := mkVizClient(ctx)
			if clientErr != nil {
				fmt.Fprintln(stdout, "Proxy versions: unavailable")
			} else {
				req := &pb.ListPodsRequest{}
				if options.namespace != "" {
					req.Selector = &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: options.namespace,
						},
					}
				}

				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				resp, err := vizClient.ListPods(ctx, req)
				if err != nil {
					fmt.Fprintln(stdout, "Proxy versions: unavailable")
				} else {
					counts := make(map[string]int)
					for _, pod := range resp.GetPods() {
						if pod.ControllerNamespace == controlPlaneNamespace {
							counts[pod.GetProxyVersion()]++
						}
					}
					if len(counts) == 0 {
						fmt.Fprintln(stdout, "Proxy versions: unavailable")
					} else {
						fmt.Fprintln(stdout, "Proxy versions:")
						for version, count := range counts {
							fmt.Fprintf(stdout, "\t%s (%d pods)\n", version, count)
						}
					}
				}
			}
		}
	}
}
