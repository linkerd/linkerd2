package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/cli/table"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/metrics-api/util"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	hc "github.com/linkerd/linkerd2/viz/pkg/healthcheck"
	pkgUtil "github.com/linkerd/linkerd2/viz/pkg/util"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
)

// NewCmdAuthz creates a new cobra command `authz`
func NewCmdAuthz() *cobra.Command {
	options := newStatOptions()

	cmd := &cobra.Command{
		Use:   "authz [flags] resource",
		Short: "Display stats for authorizations for a resource",
		Long:  "Display stats for authorizations for a resource.",
		Args:  cobra.MinimumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}

			cc := k8s.NewCommandCompletion(k8sAPI, options.namespace)

			results, err := cc.Complete(args, toComplete)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			return results, cobra.ShellCompDirectiveDefault
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}

			// The gRPC client is concurrency-safe, so we can reuse it in all the following goroutines
			// https://github.com/grpc/grpc-go/issues/682
			client := api.CheckClientOrExit(hc.VizOptions{
				Options: &healthcheck.Options{
					ControlPlaneNamespace: controlPlaneNamespace,
					KubeConfig:            kubeconfigPath,
					Impersonate:           impersonate,
					ImpersonateGroup:      impersonateGroup,
					KubeContext:           kubeContext,
					APIAddr:               apiAddr,
				},
				VizNamespaceOverride: vizNamespace,
			})

			var resource string
			if len(args) == 1 {
				resource = args[0]
			} else if len(args) == 2 {
				resource = args[0] + "/" + args[1]
			}

			cols := []table.Column{
				table.NewColumn("ROUTE").WithLeftAlign(),
				table.NewColumn("SERVER").WithLeftAlign(),
				table.NewColumn("AUTHORIZATION").WithLeftAlign(),
				table.NewColumn("UNAUTHORIZED"),
				table.NewColumn("SUCCESS"),
				table.NewColumn("RPS"),
				table.NewColumn("LATENCY_P50"),
				table.NewColumn("LATENCY_P95"),
				table.NewColumn("LATENCY_P99"),
			}
			rows := []table.Row{}

			req := pb.AuthzRequest{}
			window, err := util.ValidateTimeWindow(options.timeWindow)
			if err != nil {
				return err
			}
			req.TimeWindow = window

			target, err := pkgUtil.BuildResource(options.namespace, resource)
			if err != nil {
				return err
			}

			if options.allNamespaces && target.Name != "" {
				return errors.New("stats for a resource cannot be retrieved by name across all namespaces")
			}

			if options.allNamespaces {
				target.Namespace = ""
			} else if target.Namespace == "" {
				target.Namespace = corev1.NamespaceDefault
			}

			req.Resource = target

			resp, err := client.Authz(cmd.Context(), &req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Authz API error: %s", err)
				os.Exit(1)
			}
			if e := resp.GetError(); e != nil {
				fmt.Fprintf(os.Stderr, "Authz API error: %s", e.Error)
				os.Exit(1)
			}

			for _, row := range resp.GetOk().GetStatTable().GetPodGroup().GetRows() {
				server := row.GetSrvStats().GetSrv().GetName()
				if row.GetSrvStats().GetSrv().GetType() == "default" {
					server = fmt.Sprintf("%s:%s", row.GetSrvStats().GetSrv().GetType(), row.GetSrvStats().GetSrv().GetName())
				}
				authz := fmt.Sprintf("%s/%s", row.GetSrvStats().GetAuthz().GetType(), row.GetSrvStats().GetAuthz().GetName())
				if row.GetSrvStats().GetAuthz().GetType() == "" {
					authz = ""
				}
				if row.GetStats().GetSuccessCount()+row.GetStats().GetFailureCount()+row.GetSrvStats().GetDeniedCount() > 0 {
					rows = append(rows, table.Row{
						row.GetSrvStats().GetRoute().GetName(),
						server,
						authz,
						fmt.Sprintf("%.1frps", getRequestRate(row.GetSrvStats().GetDeniedCount(), 0, window)),
						fmt.Sprintf("%.2f%%", getSuccessRate(row.Stats.GetSuccessCount(), row.Stats.GetFailureCount())*100),
						fmt.Sprintf("%.1frps", getRequestRate(row.Stats.GetSuccessCount(), row.Stats.GetFailureCount(), window)),
						fmt.Sprintf("%dms", row.Stats.LatencyMsP50),
						fmt.Sprintf("%dms", row.Stats.LatencyMsP95),
						fmt.Sprintf("%dms", row.Stats.LatencyMsP99),
					})
				} else {
					if row.GetSrvStats().GetAuthz().GetType() == "" || row.GetSrvStats().GetAuthz().GetType() == "default" {
						// Skip showing the default or unauthorized entries if there are no requests for them.
						continue
					}
					rows = append(rows, table.Row{
						row.GetSrvStats().GetRoute().GetName(),
						server,
						authz,
						"-",
						"-",
						"-",
						"-",
						"-",
						"-",
					})
				}
			}

			data := table.NewTable(cols, rows)
			data.Sort = []int{1, 0, 2} // Sort by Server, then Route, then Authorization
			if options.outputFormat == "json" {
				err = renderJSON(data, os.Stdout)
				if err != nil {
					fmt.Fprint(os.Stderr, err.Error())
					os.Exit(1)
				}
			} else {
				data.Render(os.Stdout)
			}

			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the specified resource")
	cmd.PersistentFlags().StringVarP(&options.timeWindow, "time-window", "t", options.timeWindow, "Stat window (for example: \"15s\", \"1m\", \"10m\", \"1h\"). Needs to be at least 15s.")
	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", options.outputFormat, "Output format; one of: \"table\" or \"json\" or \"wide\"")
	cmd.PersistentFlags().StringVarP(&options.labelSelector, "selector", "l", options.labelSelector, "Selector (label query) to filter on, supports '=', '==', and '!='")

	pkgcmd.ConfigureNamespaceFlagCompletion(
		cmd, []string{"namespace"},
		kubeconfigPath, impersonate, impersonateGroup, kubeContext)
	return cmd
}

func renderJSON(t table.Table, w io.Writer) error {
	rows := make([]map[string]interface{}, len(t.Data))
	for i, data := range t.Data {
		rows[i] = make(map[string]interface{})
		for j, col := range t.Columns {
			if data[j] == "-" {
				continue
			}
			field := strings.ToLower(col.Header)
			var percentile string

			if n, _ := fmt.Sscanf(field, "latency_%s", &percentile); n == 1 {
				var latency int
				n, _ := fmt.Sscanf(data[j], "%dms", &latency)
				if n == 1 {
					rows[i]["latency_ms_"+percentile] = latency
				} else {
					rows[i]["latency_ms_"+percentile] = data[j]
				}
			} else if field == "rps" || field == "unauthorized" {
				var rps float32
				if n, _ := fmt.Sscanf(data[j], "%frps", &rps); n == 1 {
					rows[i][field] = rps
				} else {
					rows[i][field] = data[j]
				}
			} else if field == "success" {
				var success float32
				if n, _ := fmt.Sscanf(data[j], "%f%%", &success); n == 1 {
					rows[i][field] = success / 100.0
				} else {
					rows[i][field] = data[j]
				}
			} else {
				rows[i][field] = data[j]
			}
		}
	}
	out, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}
