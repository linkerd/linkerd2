package cmd

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/table"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	pkgUtil "github.com/linkerd/linkerd2/viz/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
)

type outboundRowKey struct {
	service   string
	backend   string
	route     string
	routeType string
}

type outboundRow struct {
	outboundRowKey
	successes  uint64
	failures   uint64
	latencyP50 uint64
	latencyP95 uint64
	latencyP99 uint64
	timeouts   uint64
}

// NewCmdStatOutbound creates a new cobra command `stat-outbound` for stat functionality
func NewCmdStatOutbound() *cobra.Command {
	options := newStatOptions()

	cmd := &cobra.Command{
		Use:   "stat-outbound [flags] (RESOURCE)",
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

			var promApi api.MetricsProvider
			if options.prometheus {
				promApi, err = api.NewExternalPrometheusClient(cmd.Context(), k8sAPI)
				if err != nil {
					return err
				}
			} else {
				promApi = api.NewProxyMetrics(k8sAPI)
			}

			resource, err := pkgUtil.BuildResource(options.namespace, args[0])
			if err != nil {
				return err
			}

			if !options.prometheus {
				fmt.Println("Taking initial metrics snapshot...")
			}

			httpResponseChan := make(chan *model.Sample)
			go func() {
				err := promApi.QueryRate(
					cmd.Context(),
					"outbound_http_route_backend_response_statuses_total",
					options.timeWindow,
					model.LabelSet{},
					model.LabelNames{"parent_name", "backend_name", "route_name", "route_kind", "http_status", "error"},
					resource,
					httpResponseChan,
				)
				if err != nil {
					log.Fatal(err)
				}
			}()

			httpQuantiles := map[string]chan *model.Sample{
				"0.5":  make(chan *model.Sample),
				"0.95": make(chan *model.Sample),
				"0.99": make(chan *model.Sample),
			}

			for quantile, resultChan := range httpQuantiles {
				quant, _ := strconv.ParseFloat(quantile, 32)
				go func() {
					err = promApi.QueryQuantile(
						cmd.Context(),
						quant,
						"outbound_http_route_backend_response_duration_seconds_bucket",
						options.timeWindow,
						model.LabelSet{},
						model.LabelNames{"parent_name", "backend_name", "route_name", "route_kind"},
						resource,
						resultChan,
					)
					if err != nil {
						log.Fatal(err)
					}
				}()
			}

			// GRPC

			grpcResponseChan := make(chan *model.Sample)
			go func() {
				err = promApi.QueryRate(
					cmd.Context(),
					"outbound_grpc_route_backend_response_statuses_total",
					options.timeWindow,
					model.LabelSet{},
					model.LabelNames{"parent_name", "backend_name", "route_name", "route_kind", "grpc_status", "error"},
					resource,
					grpcResponseChan,
				)
				if err != nil {
					log.Fatal(err)
				}
			}()

			grpcQuantiles := map[string]chan *model.Sample{
				"0.5":  make(chan *model.Sample),
				"0.95": make(chan *model.Sample),
				"0.99": make(chan *model.Sample),
			}

			for quantile, resultChan := range grpcQuantiles {
				quant, _ := strconv.ParseFloat(quantile, 32)

				go func() {
					err = promApi.QueryQuantile(
						cmd.Context(),
						quant,
						"outbound_grpc_route_backend_response_duration_seconds_bucket",
						options.timeWindow,
						model.LabelSet{},
						model.LabelNames{"parent_name", "backend_name", "route_name", "route_kind"},
						resource,
						resultChan,
					)
					if err != nil {
						log.Fatal(err)
					}
				}()
			}

			windowLength, err := time.ParseDuration(options.timeWindow)
			if err != nil {
				return err
			}

			if !options.prometheus {
				for i := range uint64(windowLength.Seconds()) {
					fmt.Printf("Waiting [%s]\n", windowLength-(time.Duration(i)*time.Second))
					time.Sleep(time.Second)
					fmt.Printf("\033[1A\033[K")
				}
				fmt.Println("Taking final metrics snapshot...")
			}

			results := make(map[outboundRowKey]outboundRow)

			for sample := range httpResponseChan {
				labels := sample.Metric
				backend := (string)(labels["backend_name"])
				if labels["backend_kind"] == "default" {
					backend = (string)(labels["parent_name"])
				}
				key := outboundRowKey{
					service: (string)(labels["parent_name"]),
					backend: backend,

					route:     (string)(labels["route_name"]),
					routeType: (string)(labels["route_kind"]),
				}
				row := results[key]
				row.outboundRowKey = key
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

			for quantile, resultsChan := range httpQuantiles {
				for sample := range resultsChan {
					labels := sample.Metric
					backend := (string)(labels["backend_name"])
					if labels["backend_kind"] == "default" {
						backend = (string)(labels["parent_name"])
					}
					key := outboundRowKey{
						service:   (string)(labels["parent_name"]),
						backend:   backend,
						route:     (string)(labels["route_name"]),
						routeType: (string)(labels["route_kind"]),
					}
					row := results[key]
					row.outboundRowKey = key
					switch quantile {
					case "0.5":
						row.latencyP50 = uint64(sample.Value * 1000)
					case "0.95":
						row.latencyP95 = uint64(sample.Value * 1000)
					case "0.99":
						row.latencyP99 = uint64(sample.Value * 1000)
					}
					results[key] = row
				}
			}

			for sample := range grpcResponseChan {
				labels := sample.Metric
				backend := (string)(labels["backend_name"])
				if labels["backend_kind"] == "default" {
					backend = (string)(labels["parent_name"])
				}
				key := outboundRowKey{
					service: (string)(labels["parent_name"]),
					backend: backend,

					route:     (string)(labels["route_name"]),
					routeType: (string)(labels["route_kind"]),
				}
				row := results[key]
				row.outboundRowKey = key
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

			for quantile, resultsChan := range grpcQuantiles {
				for sample := range resultsChan {
					labels := sample.Metric
					backend := (string)(labels["backend_name"])
					if labels["backend_kind"] == "default" {
						backend = (string)(labels["parent_name"])
					}
					key := outboundRowKey{
						service: (string)(labels["parent_name"]),
						backend: backend,

						route:     (string)(labels["route_name"]),
						routeType: (string)(labels["route_kind"]),
					}
					row := results[key]
					row.outboundRowKey = key
					switch quantile {
					case "0.5":
						row.latencyP50 = uint64(sample.Value * 1000)
					case "0.95":
						row.latencyP95 = uint64(sample.Value * 1000)
					case "0.99":
						row.latencyP99 = uint64(sample.Value * 1000)
					}
					results[key] = row
				}
			}

			if !options.prometheus {
				fmt.Printf("\033[1A\033[K\033[1A\033[K")
			}
			rows := make([][]string, 0)
			for _, row := range results {
				rows = append(rows, []string{
					row.service,
					row.backend,
					row.route,
					row.routeType,
					fmt.Sprintf("%.2f%%", (float32)(row.successes)/(float32)(row.successes+row.failures)*100.0),
					fmt.Sprintf("%.2f", (float32)(row.successes+row.failures)/float32(windowLength.Seconds())),
					fmt.Sprintf("%dms", row.latencyP50),
					fmt.Sprintf("%dms", row.latencyP95),
					fmt.Sprintf("%dms", row.latencyP99),
					fmt.Sprintf("%.2f%%", (float32)(row.timeouts)/(float32)(row.successes+row.failures)*100.0),
				})
			}

			columns := []table.Column{
				table.NewColumn("SERVICE").WithLeftAlign(),
				table.NewColumn("BACKEND").WithLeftAlign(),
				table.NewColumn("ROUTE").WithLeftAlign(),
				table.NewColumn("TYPE").WithLeftAlign(),
				table.NewColumn("SUCCESS"),
				table.NewColumn("RPS"),
				table.NewColumn("LATENCY_P50"),
				table.NewColumn("LATENCY_P95"),
				table.NewColumn("LATENCY_P99"),
				table.NewColumn("TIMEOUTS"),
			}

			table := table.NewTable(columns, rows)
			table.Render(os.Stdout)

			return err
		},
	}

	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", options.outputFormat, "Output format; one of: \"table\" or \"json\" or \"wide\"")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace")
	cmd.PersistentFlags().StringVarP(&options.timeWindow, "time-window", "t", options.timeWindow, "Stat window (for example: \"10s\", \"1m\", \"10m\", \"1h\")")
	cmd.PersistentFlags().BoolVar(&options.prometheus, "prometheus", options.prometheus, "Use prometheus for querying metrics")

	return cmd
}
