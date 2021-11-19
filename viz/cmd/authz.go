package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/linkerd/linkerd2/cli/table"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/viz/metrics-api/util"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	"github.com/spf13/cobra"
)

// NewCmdAuthz creates a new cobra command `authz`
func NewCmdAuthz() *cobra.Command {
	options := newStatOptions()

	cmd := &cobra.Command{
		Use:   "authz [flags] resource",
		Short: "Display stats for server authorizations for a resource",
		Long:  "Display stats for server authorizations for a resource.",
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

			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}
			// The gRPC client is concurrency-safe, so we can reuse it in all the following goroutines
			// https://github.com/grpc/grpc-go/issues/682
			client := api.CheckClientOrExit(healthcheck.Options{
				ControlPlaneNamespace: controlPlaneNamespace,
				KubeConfig:            kubeconfigPath,
				Impersonate:           impersonate,
				ImpersonateGroup:      impersonateGroup,
				KubeContext:           kubeContext,
				APIAddr:               apiAddr,
			})

			var resource string
			if len(args) == 1 {
				resource = args[0]
			} else if len(args) == 2 {
				resource = args[0] + "/" + args[1]
			}

			cols := []table.Column{
				table.NewColumn("SERVER").WithLeftAlign(),
				table.NewColumn("AUTHZ").WithLeftAlign(),
				table.NewColumn("SUCCESS"),
				table.NewColumn("RPS"),
				table.NewColumn("LATENCY_P50"),
				table.NewColumn("LATENCY_P95"),
				table.NewColumn("LATENCY_P99"),
			}
			rows := []table.Row{}

			servers, err := k8s.ServersForResource(cmd.Context(), k8sAPI, options.namespace, resource, options.labelSelector)
			if err != nil {
				fmt.Fprint(os.Stderr, err.Error())
				os.Exit(1)
			}
			for _, server := range servers {
				sazs, err := k8s.ServerAuthorizationsForServer(cmd.Context(), k8sAPI, options.namespace, server)
				if err != nil {
					fmt.Fprint(os.Stderr, err.Error())
					os.Exit(1)
				}
				for _, saz := range sazs {
					requestParams := util.StatsSummaryRequestParams{
						StatsBaseRequestParams: util.StatsBaseRequestParams{
							TimeWindow:    options.timeWindow,
							ResourceName:  saz,
							ResourceType:  k8s.ServerAuthorization,
							Namespace:     options.namespace,
							AllNamespaces: false,
						},
						ToNamespace: options.namespace,
					}
					requestParams.ToName = server
					requestParams.ToType = k8s.Server

					req, err := util.BuildStatSummaryRequest(requestParams)
					if err != nil {
						return err
					}
					resp, err := requestStatsFromAPI(client, req)
					if err != nil {
						fmt.Fprint(os.Stderr, err.Error())
						os.Exit(1)
					}

					for _, row := range respToRows(resp) {
						if row.Stats == nil {
							rows = append(rows, table.Row{
								server,
								saz,
								"-",
								"-",
								"-",
								"-",
								"-",
							})
						} else {
							rows = append(rows, table.Row{
								server,
								saz,
								fmt.Sprintf("%.2f%%", getSuccessRate(row.Stats.GetSuccessCount(), row.Stats.GetFailureCount())*100),
								fmt.Sprintf("%.1frps", getRequestRate(row.Stats.GetSuccessCount(), row.Stats.GetFailureCount(), row.TimeWindow)),
								fmt.Sprintf("%dms", row.Stats.LatencyMsP50),
								fmt.Sprintf("%dms", row.Stats.LatencyMsP95),
								fmt.Sprintf("%dms", row.Stats.LatencyMsP99),
							})
						}
					}
				}

				// Unauthorized
				requestParams := util.StatsSummaryRequestParams{
					StatsBaseRequestParams: util.StatsBaseRequestParams{
						TimeWindow:    options.timeWindow,
						ResourceName:  server,
						ResourceType:  k8s.Server,
						Namespace:     options.namespace,
						AllNamespaces: false,
					},
					ToNamespace:   options.namespace,
					LabelSelector: options.labelSelector,
				}

				req, err := util.BuildStatSummaryRequest(requestParams)
				if err != nil {
					return err
				}
				resp, err := requestStatsFromAPI(client, req)
				if err != nil {
					fmt.Fprint(os.Stderr, err.Error())
					os.Exit(1)
				}
				for _, row := range respToRows(resp) {
					if row.SrvStats != nil && row.SrvStats.DeniedCount > 0 {
						rows = append(rows, table.Row{
							server,
							"[UNAUTHORIZED]",
							"-",
							fmt.Sprintf("%.1frps", getRequestRate(row.SrvStats.DeniedCount, 0, row.TimeWindow)),
							"-",
							"-",
							"-",
						})
					}
				}
			}

			data := table.NewTable(cols, rows)
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
			} else if field == "rps" {
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
