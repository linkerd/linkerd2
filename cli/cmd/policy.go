package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	pb "github.com/linkerd/linkerd2-proxy-api/go/inbound"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	policyPort       = 8090
	policyDeployment = "linkerd-destination"
)

func newCmdPolicy() *cobra.Command {
	options := newEndpointsOptions()
	var namespace = "default"

	example := `  # get the inbound policy for pod emoji-6d66d87995-bvrnn on port 8080
  linkerd diagnostics policy -n emojivoto emoji-6d66d87995-bvrnn 8080`

	cmd := &cobra.Command{
		Use:   "policy [flags] pod port",
		Short: "Introspect Linkerd's policy state",
		Long: `Introspect Linkerd's policy state.

This command provides debug information about the internal state of the
control-plane's policy controller. It queries the same control-plane
endpoint as the linkerd-proxy's, and returns the policies associated with that
server.`,
		Example: example,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			err := options.validate()
			if err != nil {
				return err
			}

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			port, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return err
			}

			if apiAddr == "" {
				var portForward *k8s.PortForward
				var err error
				if options.destinationPod == "" {
					portForward, err = k8s.NewPortForward(
						cmd.Context(),
						k8sAPI,
						controlPlaneNamespace,
						policyDeployment,
						"localhost",
						0,
						policyPort,
						false,
					)
				} else {
					portForward, err = k8s.NewPodPortForward(k8sAPI, controlPlaneNamespace, options.destinationPod, "localhost", 0, policyPort, false)
				}
				if err != nil {
					return err
				}

				apiAddr = portForward.AddressAndPort()
				if err = portForward.Init(); err != nil {
					return err
				}
			}

			conn, err := grpc.Dial(apiAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(&ocgrpc.ClientHandler{}))
			if err != nil {
				return err
			}
			defer conn.Close()

			client := pb.NewInboundServerPoliciesClient(conn)

			server, err := client.GetPort(cmd.Context(), &pb.PortSpec{
				Workload: fmt.Sprintf("%s:%s", namespace, args[0]),
				Port:     uint32(port),
			})
			if err != nil {
				return err
			}

			out, err := json.MarshalIndent(server, "", "  ")
			if err != nil {
				fmt.Fprint(os.Stderr, err)
				os.Exit(1)
			}
			_, err = fmt.Print(string(out))

			return err
		},
	}

	cmd.PersistentFlags().StringVar(&options.destinationPod, "destination-pod", "", "Target a specific destination Pod when there are multiple running")
	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", namespace, "Namespace of resource")

	pkgcmd.ConfigureOutputFlagCompletion(cmd)

	return cmd
}
