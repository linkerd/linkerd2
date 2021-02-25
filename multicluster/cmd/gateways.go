package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/linkerd/linkerd2/cli/table"
	"github.com/linkerd/linkerd2/pkg/k8s"
	vizCmd "github.com/linkerd/linkerd2/viz/cmd"
	"github.com/linkerd/linkerd2/viz/metrics-api/client"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
)

type (
	gatewaysOptions struct {
		gatewayNamespace string
		clusterName      string
		timeWindow       string
	}
)

func newGatewaysCommand() *cobra.Command {

	opts := gatewaysOptions{}

	cmd := &cobra.Command{
		Use:   "gateways",
		Short: "Display stats information about the gateways in target clusters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := &pb.GatewaysRequest{
				RemoteClusterName: opts.clusterName,
				GatewayNamespace:  opts.gatewayNamespace,
				TimeWindow:        opts.timeWindow,
			}

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			vizNs, err := k8sAPI.GetNamespaceWithExtensionLabel(ctx, vizCmd.ExtensionName)
			if err != nil {
				return fmt.Errorf("make sure the linkerd-viz extension is installed, using 'linkerd viz install' (%s)", err)
			}

			client, err := client.NewExternalClient(ctx, vizNs.Name, k8sAPI)
			if err != nil {
				return err
			}

			resp, err := requestGatewaysFromAPI(client, req)
			if err != nil {
				fmt.Fprint(os.Stderr, err.Error())
				os.Exit(1)
			}

			renderGateways(resp.GetOk().GatewaysTable.Rows, stdout)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.clusterName, "cluster-name", "", "the name of the target cluster")
	cmd.Flags().StringVar(&opts.gatewayNamespace, "gateway-namespace", "", "the namespace in which the gateway resides on the target cluster")
	cmd.Flags().StringVarP(&opts.timeWindow, "time-window", "t", "1m", "Time window (for example: \"15s\", \"1m\", \"10m\", \"1h\"). Needs to be at least 15s.")

	return cmd
}

func requestGatewaysFromAPI(client pb.ApiClient, req *pb.GatewaysRequest) (*pb.GatewaysResponse, error) {
	resp, err := client.Gateways(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("Gateways API error: %v", err)
	}
	if e := resp.GetError(); e != nil {
		return nil, fmt.Errorf("Gateways API response error: %v", e.Error)
	}
	return resp, nil
}

func renderGateways(rows []*pb.GatewaysTable_Row, w io.Writer) {
	t := buildGatewaysTable()
	t.Data = []table.Row{}
	for _, row := range rows {
		row := row // Copy to satisfy golint.
		t.Data = append(t.Data, gatewaysRowToTableRow(row))
	}
	t.Render(w)
}

var (
	clusterNameHeader    = "CLUSTER"
	aliveHeader          = "ALIVE"
	pairedServicesHeader = "NUM_SVC"
	latencyP50Header     = "LATENCY_P50"
	latencyP95Header     = "LATENCY_P95"
	latencyP99Header     = "LATENCY_P99"
)

func buildGatewaysTable() table.Table {
	columns := []table.Column{
		table.Column{
			Header:    clusterNameHeader,
			Width:     7,
			Flexible:  true,
			LeftAlign: true,
		},
		table.Column{
			Header:    aliveHeader,
			Width:     5,
			Flexible:  true,
			LeftAlign: true,
		},
		table.Column{
			Header: pairedServicesHeader,
			Width:  9,
		},
		table.Column{
			Header: latencyP50Header,
			Width:  11,
		},
		table.Column{
			Header: latencyP95Header,
			Width:  11,
		},
		table.Column{
			Header: latencyP99Header,
			Width:  11,
		},
	}
	t := table.NewTable(columns, []table.Row{})
	t.Sort = []int{0, 1} // Sort by namespace, then name.
	return t
}

func gatewaysRowToTableRow(row *pb.GatewaysTable_Row) []string {
	valueOrPlaceholder := func(value string) string {
		if row.Alive {
			return value
		}
		return "-"
	}

	alive := "False"

	if row.Alive {
		alive = "True"
	}
	return []string{
		row.ClusterName,
		alive,
		fmt.Sprint(row.PairedServices),
		valueOrPlaceholder(fmt.Sprintf("%dms", row.LatencyMsP50)),
		valueOrPlaceholder(fmt.Sprintf("%dms", row.LatencyMsP95)),
		valueOrPlaceholder(fmt.Sprintf("%dms", row.LatencyMsP99)),
	}

}

func extractGatewayPort(gateway *corev1.Service) (uint32, error) {
	for _, port := range gateway.Spec.Ports {
		if port.Name == k8s.GatewayPortName {
			return uint32(port.Port), nil
		}
	}
	return 0, fmt.Errorf("gateway service %s has no gateway port named %s", gateway.Name, k8s.GatewayPortName)
}
