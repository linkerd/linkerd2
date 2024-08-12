package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/table"
	metricsApi "github.com/linkerd/linkerd2/viz/metrics-api"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	pkgUtil "github.com/linkerd/linkerd2/viz/pkg/util"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type outboundLogicalRowKey struct {
	service   string
	route     string
	routeType string
}

type outboundLogicalRow struct {
	outboundLogicalRowKey
	successes  uint64
	failures   uint64
	latencyP50 uint64
	latencyP95 uint64
	latencyP99 uint64
	timeouts   uint64
	retries    uint64
}

// NewCmdStatLogicalOutbound creates a new cobra command `stat-outbound-logical` for stat functionality
func NewCmdStatLogicalOutbound() *cobra.Command {
	options := newStatOptions()

	cmd := &cobra.Command{
		Use:   "stat-outbound-logical [flags] (RESOURCE)",
		Short: "Display outbound traffic stats about a resource",
		Long: `Display outbound traffic stats about a resource.

  The RESOURCE argument specifies the target resource to aggregate stats over:
  TYPE/NAME

  Examples:
  * cronjob/my-cronjob
  * deploy/my-deploy
  * ds/my-daemonset
  * job/my-job
  * ns/my-ns
  * rc/my-replication-controller
  * rs/my-replicaset
  * sts/my-statefulset

  Valid resource types include:
  * cronjobs
  * daemonsets
  * deployments
  * namespaces
  * jobs
  * pods
  * replicasets
  * replicationcontrollers
  * statefulsets`,
		Args: cobra.MinimumNArgs(1),
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

			promApi, err := api.NewExternalPrometheusClient(context.Background(), k8sAPI)
			if err != nil {
				return err
			}

			resource, err := pkgUtil.BuildResource(options.namespace, args[0])
			if err != nil {
				return err
			}

			labels := metricsApi.PromQueryLabels(resource)
			query := fmt.Sprintf("sum(increase(outbound_http_route_request_statuses_total%s[%s])) by (parent_name, route_name, route_kind, http_status, error)",
				labels,
				options.timeWindow,
			)

			res, warn, err := promApi.Query(cmd.Context(), query, time.Time{})
			if err != nil {
				return err
			}
			if warn != nil {
				log.Warnf("%v", warn)
			}
			vector := res.(model.Vector)

			results := make(map[outboundLogicalRowKey]outboundLogicalRow)
			for _, sample := range vector {
				labels := sample.Metric
				key := outboundLogicalRowKey{
					service:   (string)(labels["parent_name"]),
					route:     (string)(labels["route_name"]),
					routeType: (string)(labels["route_kind"]),
				}
				row := results[key]
				row.outboundLogicalRowKey = key
				if strings.HasPrefix((string)(labels["http_status"]), "5") || labels["error"] != "" {
					row.failures += uint64(sample.Value)
				} else {
					row.successes += uint64(sample.Value)
				}
				if labels["error"] == "RESPONSE_HEADERS_TIMEOUT" || labels["error"] == "REQUEST_TIMEOUT" {
					row.timeouts += uint64(sample.Value)
				}
				results[key] = row
			}

			query = fmt.Sprintf("sum(increase(outbound_http_route_retry_requests_total%s[%s])) by (parent_name, route_name, route_kind)",
				labels,
				options.timeWindow,
			)

			res, warn, err = promApi.Query(cmd.Context(), query, time.Time{})
			if err != nil {
				return err
			}
			if warn != nil {
				log.Warnf("%v", warn)
			}
			vector = res.(model.Vector)

			for _, sample := range vector {
				labels := sample.Metric
				key := outboundLogicalRowKey{
					service:   (string)(labels["parent_name"]),
					route:     (string)(labels["route_name"]),
					routeType: (string)(labels["route_kind"]),
				}
				row := results[key]
				row.outboundLogicalRowKey = key
				row.retries += uint64(sample.Value)
				results[key] = row
			}

			for _, quantile := range []string{"0.5", "0.95", "0.99"} {

				query = fmt.Sprintf("histogram_quantile(%s, sum(irate(outbound_http_route_request_duration_seconds_bucket%s[%s])) by (le, parent_name, route_name, route_kind))",
					quantile,
					labels,
					options.timeWindow,
				)
				res, warn, err = promApi.Query(cmd.Context(), query, time.Time{})
				if err != nil {
					return err
				}
				if warn != nil {
					log.Warnf("%v", warn)
				}
				vector = res.(model.Vector)

				for _, sample := range vector {
					labels := sample.Metric
					key := outboundLogicalRowKey{
						service:   (string)(labels["parent_name"]),
						route:     (string)(labels["route_name"]),
						routeType: (string)(labels["route_kind"]),
					}
					row := results[key]
					row.outboundLogicalRowKey = key
					switch quantile {
					case "0.5":
						row.latencyP50 = uint64(sample.Value)
					case "0.95":
						row.latencyP95 = uint64(sample.Value)
					case "0.99":
						row.latencyP99 = uint64(sample.Value)
					}
					results[key] = row
				}

			}

			// GRPC

			query = fmt.Sprintf("sum(increase(outbound_grpc_route_request_statuses_total%s[%s])) by (parent_name, route_name, route_kind, grpc_status, error)",
				labels,
				options.timeWindow,
			)

			res, warn, err = promApi.Query(cmd.Context(), query, time.Time{})
			if err != nil {
				return err
			}
			if warn != nil {
				log.Warnf("%v", warn)
			}
			vector = res.(model.Vector)

			for _, sample := range vector {
				labels := sample.Metric
				key := outboundLogicalRowKey{
					service:   (string)(labels["parent_name"]),
					route:     (string)(labels["route_name"]),
					routeType: (string)(labels["route_kind"]),
				}
				row := results[key]
				row.outboundLogicalRowKey = key
				if labels["grpc_status"] != "OK" || labels["error"] != "" {
					row.failures += uint64(sample.Value)
				} else {
					row.successes += uint64(sample.Value)
				}
				if labels["error"] == "RESPONSE_HEADERS_TIMEOUT" || labels["error"] == "REQUEST_TIMEOUT" {
					row.timeouts += uint64(sample.Value)
				}
				results[key] = row
			}

			query = fmt.Sprintf("sum(increase(outbound_grpc_route_retry_requests_total%s[%s])) by (parent_name, route_name, route_kind)",
				labels,
				options.timeWindow,
			)

			res, warn, err = promApi.Query(cmd.Context(), query, time.Time{})
			if err != nil {
				return err
			}
			if warn != nil {
				log.Warnf("%v", warn)
			}
			vector = res.(model.Vector)

			for _, sample := range vector {
				labels := sample.Metric
				key := outboundLogicalRowKey{
					service:   (string)(labels["parent_name"]),
					route:     (string)(labels["route_name"]),
					routeType: (string)(labels["route_kind"]),
				}
				row := results[key]
				row.outboundLogicalRowKey = key
				row.retries += uint64(sample.Value)
				results[key] = row
			}

			for _, quantile := range []string{"0.5", "0.95", "0.99"} {

				query = fmt.Sprintf("histogram_quantile(%s, sum(irate(outbound_grpc_route_request_duration_seconds_bucket%s[%s])) by (le, parent_name, route_name, route_kind))",
					quantile,
					labels,
					options.timeWindow,
				)
				res, warn, err = promApi.Query(cmd.Context(), query, time.Time{})
				if err != nil {
					return err
				}
				if warn != nil {
					log.Warnf("%v", warn)
				}
				vector = res.(model.Vector)

				for _, sample := range vector {
					labels := sample.Metric
					key := outboundLogicalRowKey{
						service:   (string)(labels["parent_name"]),
						route:     (string)(labels["route_name"]),
						routeType: (string)(labels["route_kind"]),
					}
					row := results[key]
					row.outboundLogicalRowKey = key
					switch quantile {
					case "0.5":
						row.latencyP50 = uint64(sample.Value)
					case "0.95":
						row.latencyP95 = uint64(sample.Value)
					case "0.99":
						row.latencyP99 = uint64(sample.Value)
					}
					results[key] = row
				}

			}

			rows := make([][]string, 0)
			windowLength, err := time.ParseDuration(options.timeWindow)
			for _, row := range results {
				rows = append(rows, []string{
					row.service,
					row.route,
					row.routeType,
					fmt.Sprintf("%.2f%%", (float32)(row.successes)/(float32)(row.successes+row.failures)*100.0),
					fmt.Sprintf("%.2f", (float32)(row.successes+row.failures)/float32(windowLength.Seconds())),
					fmt.Sprintf("%dms", row.latencyP50),
					fmt.Sprintf("%dms", row.latencyP95),
					fmt.Sprintf("%dms", row.latencyP99),
					fmt.Sprintf("%.2f%%", (float32)(row.timeouts)/(float32)(row.successes+row.failures)*100.0),
					fmt.Sprintf("%.2f%%", (float32)(row.retries)/(float32)(row.successes+row.failures+row.retries)*100.0),
				})
			}

			columns := []table.Column{
				table.NewColumn("SERVICE").WithLeftAlign(),
				table.NewColumn("ROUTE").WithLeftAlign(),
				table.NewColumn("TYPE").WithLeftAlign(),
				table.NewColumn("SUCCESS"),
				table.NewColumn("RPS"),
				table.NewColumn("LATENCY_P50"),
				table.NewColumn("LATENCY_P95"),
				table.NewColumn("LATENCY_P99"),
				table.NewColumn("TIMEOUTS"),
				table.NewColumn("RETRIES"),
			}

			table := table.NewTable(columns, rows)
			table.Render(os.Stdout)

			return err
		},
	}

	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", options.outputFormat, "Output format; one of: \"table\" or \"json\" or \"wide\"")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace")

	return cmd
}
