package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/linkerd/linkerd2-proxy-api/go/inbound"
	"github.com/linkerd/linkerd2-proxy-api/go/outbound"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	policyPort       = 8090
	policyDeployment = "linkerd-destination"
)

func newCmdPolicy() *cobra.Command {
	options := newEndpointsOptions()
	var (
		namespace = "default"
		output    = "yaml"
	)

	example := `  # get the inbound policy for pod emoji-6d66d87995-bvrnn on port 8080
  linkerd diagnostics policy -n emojivoto po/emoji-6d66d87995-bvrnn 8080

  # get the outbound policy for Service emoji-svc on port 8080
  linkerd diagnostics policy -n emojivoto svc/emoji-svc 8080`

	cmd := &cobra.Command{
		Use:   "policy [flags] resource port",
		Short: "Introspect Linkerd's policy state",
		Long: `Introspect Linkerd's policy state.

This command provides debug information about the internal state of the
control-plane's policy controller. It queries the same control-plane
endpoint as the linkerd-proxy's, and returns the policies associated with the
given resource. If the resource is a Pod, inbound policy for that Pod is
displayed. If the resource is a Service, outbound policy for that Service is
displayed.`,
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

			conn, err := grpc.NewClient(apiAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
			if err != nil {
				return err
			}
			defer conn.Close()

			elems := strings.Split(args[0], "/")

			if len(elems) == 1 {
				return errors.New("resource type and name are required")
			}

			if len(elems) != 2 {
				return fmt.Errorf("invalid resource string: %s", args[0])
			}
			typ, err := k8s.CanonicalResourceNameFromFriendlyName(elems[0])
			if err != nil {
				return err
			}
			name := elems[1]

			port, err := strconv.ParseUint(args[1], 10, 32)
			if err != nil {
				return err
			}

			var result interface{}

			if typ == k8s.Pod {
				client := inbound.NewInboundServerPoliciesClient(conn)

				result, err = client.GetPort(cmd.Context(), &inbound.PortSpec{
					Workload: fmt.Sprintf("%s:%s", namespace, name),
					Port:     uint32(port),
				})
				if err != nil {
					return err
				}

			} else if typ == k8s.Service {
				client := outbound.NewOutboundPoliciesClient(conn)

				result, err = client.Get(cmd.Context(), &outbound.TrafficSpec{
					SourceWorkload: options.contextToken,
					Target:         &outbound.TrafficSpec_Authority{Authority: fmt.Sprintf("%s.%s.svc:%d", name, namespace, port)},
				})
				if err != nil {
					return err
				}
			} else {
				return fmt.Errorf("invalid resource type %s; must be one of Pod or Service", args[0])
			}

			var out []byte
			switch output {
			case "json":
				out, err = json.MarshalIndent(result, "", "  ")
				if err != nil {
					fmt.Fprint(os.Stderr, err)
					os.Exit(1)
				}
			case "yaml":
				out, err = yaml.Marshal(result)
				if err != nil {
					fmt.Fprint(os.Stderr, err)
					os.Exit(1)
				}
			default:
				return errors.New("output must be one of: yaml, json")
			}

			_, err = fmt.Print(string(out))
			return err
		},
	}

	cmd.PersistentFlags().StringVar(&options.destinationPod, "destination-pod", "", "Target a specific destination Pod when there are multiple running")
	cmd.PersistentFlags().StringVar(&options.contextToken, "token", "default:diagnostics", "Token to use when querying the policy service")
	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", namespace, "Namespace of resource")
	cmd.PersistentFlags().StringVarP(&output, "output", "o", output, "Output format. One of: yaml, json")

	pkgcmd.ConfigureOutputFlagCompletion(cmd)

	return cmd
}
