package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/linkerd/linkerd2/cli/table"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	hc "github.com/linkerd/linkerd2/viz/pkg/healthcheck"
	"github.com/linkerd/linkerd2/viz/pkg/prometheus"
	pkgUtil "github.com/linkerd/linkerd2/viz/pkg/util"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type statInboundOptions struct {
	statOptionsBase
	prometheusURL string
}

type inboundRowKey struct {
	name      string
	server    string
	port      string
	route     string
	routeType string
}

type inboundRow struct {
	successes  uint64
	failures   uint64
	latencyP50 string
	latencyP95 string
	latencyP99 string
}

func newStatInboundOptions() *statInboundOptions {
	return &statInboundOptions{
		statOptionsBase: *newStatOptionsBase(),
	}
}

// NewCmdStatInbound creates a new cobra command `stat-inbound` for inbound stats functionality
func NewCmdStatInbound() *cobra.Command {
	options := newStatInboundOptions()

	cmd := &cobra.Command{
		Use:   "stat-inbound [flags] (RESOURCE)",
		Short: "Display inbound traffic stats about a resource",
		Long: `Display inbound traffic stats about a resource.

  The RESOURCE argument specifies the target resource to display stats from:
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

			promApi, err := api.NewPrometheusClient(cmd.Context(), hc.VizOptions{
				Options: &healthcheck.Options{
					ControlPlaneNamespace: controlPlaneNamespace,
					KubeConfig:            kubeconfigPath,
					Impersonate:           impersonate,
					ImpersonateGroup:      impersonateGroup,
					KubeContext:           kubeContext,
					APIAddr:               apiAddr,
				},
				VizNamespaceOverride: vizNamespace,
			}, options.prometheusURL)
			if err != nil {
				return err
			}

			resource, err := pkgUtil.BuildResource(options.namespace, args[0])
			if err != nil {
				return err
			}

			// Issue Prometheus queries.

			responseChan := queryRate(
				cmd.Context(),
				promApi,
				"response_total",
				options.timeWindow,
				model.LabelSet{"direction": "inbound"},
				model.LabelNames{"srv_name", "srv_kind", "route_name", "route_kind", "classification", "target_port"},
				resource,
			)

			quantiles := queryQuantiles(
				cmd.Context(),
				promApi,
				"response_latency_ms_bucket",
				options.timeWindow,
				model.LabelSet{"direction": "inbound"},
				model.LabelNames{"srv_name", "srv_kind", "route_name", "route_kind", "target_port"},
				resource,
			)

			// Collect Prometheus results.

			results := make(map[inboundRowKey]inboundRow)

			for sample := range responseChan {
				labels := sample.Metric
				key := inboundKeyForSample(sample, resource)
				row := results[key]
				if labels["classification"] == "success" {
					row.successes += uint64(sample.Value)
				} else {
					row.failures += uint64(sample.Value)
				}
				results[key] = row
			}

			for quantile, resultsChan := range quantiles {
				for sample := range resultsChan {
					key := inboundKeyForSample(sample, resource)
					row := results[key]
					row.populateLatency(quantile, sample)
					results[key] = row
				}
			}

			// Render output.

			rows := make([][]string, 0)

			windowLength, err := time.ParseDuration(options.timeWindow)
			if err != nil {
				return err
			}

			for key, row := range results {
				if row.failures+row.successes == 0 {
					continue
				}
				rows = append(rows, []string{
					key.name,
					fmt.Sprintf("%s:%s", key.server, key.port),
					key.route,
					key.routeType,
					fmt.Sprintf("%.2f%%", (float32)(row.successes)/(float32)(row.successes+row.failures)*100.0),
					fmt.Sprintf("%.2f", (float32)(row.successes+row.failures)/float32(windowLength.Seconds())),
					row.latencyP50,
					row.latencyP95,
					row.latencyP99,
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
			table.Sort = []int{0, 1, 3} // Name, Server, Route
			table.Render(os.Stdout)

			return err
		},
	}

	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", options.outputFormat, "Output format; one of: \"table\" or \"json\" or \"wide\"")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace")
	cmd.PersistentFlags().StringVarP(&options.timeWindow, "time-window", "t", options.timeWindow, "Stat window (for example: \"10s\", \"1m\", \"10m\", \"1h\")")
	cmd.PersistentFlags().StringVar(&options.prometheusURL, "prometheusURL", options.prometheusURL, "Address of Prometheus instance to query")

	return cmd
}

func queryRate(
	ctx context.Context,
	promAPI promv1.API,
	metric string,
	timeWindow string,
	labels model.LabelSet,
	groupBy model.LabelNames,
	resource *pb.Resource,
) <-chan *model.Sample {
	results := make(chan *model.Sample)
	go func() {
		defer close(results)
		query := fmt.Sprintf("sum(increase(%s%s[%s])) by (%s)",
			metric,
			labels.Merge(prometheus.PromQueryLabels(resource)),
			timeWindow,
			append(groupBy, prometheus.PromGroupByLabelNames(resource)...),
		)
		log.Debug(query)
		val, warn, err := promAPI.Query(ctx, query, time.Time{})
		if warn != nil {
			log.Warnf("%v", warn)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to query Prometheus: ", err.Error())
			os.Exit(1)
		}
		for _, sample := range val.(model.Vector) {
			results <- sample
		}
	}()
	return results
}

func queryQuantiles(
	ctx context.Context,
	promAPI promv1.API,
	metric string,
	timeWindow string,
	labels model.LabelSet,
	groupBy model.LabelNames,
	resource *pb.Resource,
) map[string]chan *model.Sample {
	results := map[string]chan *model.Sample{
		"0.5":  make(chan *model.Sample),
		"0.95": make(chan *model.Sample),
		"0.99": make(chan *model.Sample),
	}
	for quantile, resultsChan := range results {
		go func(quantile string) {
			defer close(resultsChan)
			query := fmt.Sprintf("histogram_quantile(%s, sum(irate(%s%s[%s])) by (le, %s))",
				quantile,
				metric,
				labels.Merge(prometheus.PromQueryLabels(resource)),
				timeWindow,
				append(groupBy, prometheus.PromGroupByLabelNames(resource)...),
			)
			log.Debug(query)
			val, warn, err := promAPI.Query(ctx, query, time.Time{})
			if warn != nil {
				log.Warnf("%v", warn)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "failed to query Prometheus: ", err.Error())
				os.Exit(1)
			}
			for _, sample := range val.(model.Vector) {
				resultsChan <- sample
			}
		}(quantile)
	}
	return results
}

func inboundKeyForSample(sample *model.Sample, resource *pb.Resource) inboundRowKey {
	labels := sample.Metric
	server := (string)(labels["srv_name"])
	route := (string)(labels["route_name"])
	routeType := (string)(labels["route_kind"])
	if labels["srv_kind"] == "default" {
		server = "[default]"
	}
	if labels["route_name"] == "default" {
		route = "[default]"
	}
	if labels["route_kind"] == "default" {
		routeType = ""
	}

	return inboundRowKey{
		name:      (string)(labels[prometheus.PromResourceType(resource)]),
		server:    server,
		route:     route,
		port:      (string)(labels["target_port"]),
		routeType: routeType,
	}
}

func formatLatencyMs(value float64) string {
	latency := "-"
	if !math.IsNaN(value) {
		latency = fmt.Sprintf("%.0fms", value)
	}
	return latency
}

func (r *inboundRow) populateLatency(quantile string, sample *model.Sample) {
	latency := formatLatencyMs(float64(sample.Value))
	switch quantile {
	case "0.5":
		r.latencyP50 = latency
	case "0.95":
		r.latencyP95 = latency
	case "0.99":
		r.latencyP99 = latency
	}
}
