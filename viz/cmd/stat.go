package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/table"
	metricsApi "github.com/linkerd/linkerd2/viz/metrics-api"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	pkgUtil "github.com/linkerd/linkerd2/viz/pkg/util"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type statOptions struct {
	statOptionsBase
}

type statOptionsBase struct {
	// namespace is only referenced from the outer struct statOptions (e.g.
	// options.namespace where options's type is _not_ this struct).
	// structcheck issues a false positive for this field as it does not think
	// it's used.
	//nolint:structcheck
	namespace    string
	timeWindow   string
	outputFormat string
}

type rowKey struct {
	name      string
	server    string
	route     string
	routeType string
}

type row struct {
	rowKey
	successes  uint64
	failures   uint64
	latencyP50 uint64
	latencyP95 uint64
	latencyP99 uint64
}

func newStatOptionsBase() *statOptionsBase {
	return &statOptionsBase{
		timeWindow:   "1m",
		outputFormat: tableOutput,
	}
}

func (o *statOptionsBase) validateOutputFormat() error {
	switch o.outputFormat {
	case tableOutput, jsonOutput, wideOutput:
		return nil
	default:
		return fmt.Errorf("--output currently only supports %s, %s and %s", tableOutput, jsonOutput, wideOutput)
	}
}

type indexedResults struct {
	ix   int
	rows []*pb.StatTable_PodGroup_Row
	err  error
}

func newStatOptions() *statOptions {
	return &statOptions{
		statOptionsBase: *newStatOptionsBase(),
	}
}

// NewCmdStat creates a new cobra command `stat` for stat functionality
func NewCmdStat() *cobra.Command {
	options := newStatOptions()

	cmd := &cobra.Command{
		Use:   "stat [flags] (RESOURCE)",
		Short: "Display traffic stats about a resource",
		Long: `Display traffic stats about a resource.

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
			labels["direction"] = "inbound"
			query := fmt.Sprintf("sum(increase(response_total%s[%s])) by (%s, srv_name, srv_kind, route_name, route_kind, classification)",
				labels,
				options.timeWindow,
				metricsApi.PromGroupByLabelNames(resource),
			)

			res, warn, err := promApi.Query(cmd.Context(), query, time.Time{})
			if err != nil {
				return err
			}
			if warn != nil {
				log.Warnf("%v", warn)
			}
			vector := res.(model.Vector)

			results := make(map[rowKey]row)
			for _, sample := range vector {
				labels := sample.Metric
				server := (string)(labels["srv_name"])
				if labels["srv_kind"] == "default" {
					server = "[default]"
				}
				key := rowKey{
					name:      (string)(labels[metricsApi.PromResourceType(resource)]),
					server:    server,
					route:     (string)(labels["route_name"]),
					routeType: (string)(labels["route_kind"]),
				}
				row := results[key]
				row.rowKey = key
				if labels["classification"] == "success" {
					row.successes = uint64(sample.Value)
				} else {
					row.failures = uint64(sample.Value)
				}
				results[key] = row
			}

			for _, quantile := range []string{"0.5", "0.95", "0.99"} {

				query = fmt.Sprintf("histogram_quantile(%s, sum(irate(response_latency_ms_bucket%s[%s])) by (le, srv_name, srv_kind, route_name, route_kind, %s))", quantile, labels, options.timeWindow, metricsApi.PromGroupByLabelNames(resource))
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
					server := (string)(labels["srv_name"])
					if labels["srv_kind"] == "default" {
						server = "[default]"
					}
					key := rowKey{
						name:      (string)(labels[metricsApi.PromResourceType(resource)]),
						server:    server,
						route:     (string)(labels["route_name"]),
						routeType: (string)(labels["route_kind"]),
					}
					row := results[key]
					row.rowKey = key
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
					row.name,
					row.server,
					row.route,
					row.routeType,
					fmt.Sprintf("%.2f%%", (float32)(row.successes)/(float32)(row.successes+row.failures)*100.0),
					fmt.Sprintf("%.2f", (float32)(row.successes+row.failures)/float32(windowLength.Seconds())),
					fmt.Sprintf("%dms", row.latencyP50),
					fmt.Sprintf("%dms", row.latencyP95),
					fmt.Sprintf("%dms", row.latencyP99),
				})
			}

			columns := []table.Column{
				table.NewColumn("NAME").WithLeftAlign(),
				table.NewColumn("SERVER").WithLeftAlign(),
				table.NewColumn("ROUTE").WithLeftAlign(),
				table.NewColumn("TYPE").WithLeftAlign(),
				table.NewColumn("SUCCESS"),
				table.NewColumn("RPS"),
				table.NewColumn("LATENCY_P50"),
				table.NewColumn("LATENCY_P95"),
				table.NewColumn("LATENCY_P99"),
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
