package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

type diagProfileOptions struct {
	destinationPod string
	contextToken   string
}

// validate performs all validation on the command-line options.
// It returns the first error encountered, or `nil` if the options are valid.
func (o *diagProfileOptions) validate() error {
	return nil
}

func newDiagProfileOptions() *diagProfileOptions {
	return &diagProfileOptions{}
}

func newCmdDiagnosticsProfile() *cobra.Command {
	options := newDiagProfileOptions()

	example := `  # Get the service profile for the service or endpoint at 10.20.2.4:8080
  linkerd diagnostics profile 10.20.2.4:8080`

	cmd := &cobra.Command{
		Use:     "profile [flags] address",
		Aliases: []string{"ep"},
		Short:   "Introspect Linkerd's service discovery state",
		Long: `Introspect Linkerd's service discovery state.

This command provides debug information about the internal state of the
control-plane's destination controller. It queries the same Destination service
endpoint as the linkerd-proxy's, and returns the profile associated with that
destination.`,
		Example: example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			err := options.validate()
			if err != nil {
				return err
			}

			var client destinationPb.DestinationClient
			var conn *grpc.ClientConn
			if apiAddr != "" {
				client, conn, err = destination.NewClient(apiAddr)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating destination client: %s\n", err)
					os.Exit(1)
				}
			} else {
				k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
				if err != nil {
					return err
				}

				client, conn, err = destination.NewExternalClient(cmd.Context(), controlPlaneNamespace, k8sAPI, options.destinationPod)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating destination client: %s\n", err)
					os.Exit(1)
				}
			}

			defer conn.Close()

			profile, err := requestProfileFromAPI(client, options.contextToken, args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Destination API error: %s\n", err)
				os.Exit(1)
			}

			return writeProfileJSON(os.Stdout, profile)
		},
	}

	cmd.PersistentFlags().StringVar(&options.destinationPod, "destination-pod", "", "Target a specific destination Pod when there are multiple running")
	cmd.PersistentFlags().StringVar(&options.contextToken, "token", "", "The context token to use when making the request to the destination API")

	pkgcmd.ConfigureOutputFlagCompletion(cmd)

	return cmd
}

func requestProfileFromAPI(client destinationPb.DestinationClient, token string, addr string) (*destinationPb.DestinationProfile, error) {
	dest := &destinationPb.GetDestination{
		Path:         addr,
		ContextToken: token,
	}

	rsp, err := client.GetProfile(context.Background(), dest)
	if err != nil {
		return nil, err
	}

	return rsp.Recv()
}

func writeProfileJSON(w io.Writer, profile *destinationPb.DestinationProfile) error {
	b, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(w, string(b))
	return err
}
