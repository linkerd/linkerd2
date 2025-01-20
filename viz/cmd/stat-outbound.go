package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
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
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
)

type outboundRowKey struct {
	name      string
	service   string
	port      string
	route     string
	routeType string
}

type outboundBackendRow struct {
	successes  uint64
	failures   uint64
	latencyP50 float64
	latencyP95 float64
	latencyP99 float64
	timeouts   uint64
}

type backendKey struct {
	name string
	port string
}

type outboundRouteRow struct {
	outboundBackendRow
	retries  uint64
	backends map[backendKey]outboundBackendRow
}

type outboundJsonRow struct {
	Name        string   `json:"name"`
	Service     string   `json:"service"`
	Port        string   `json:"port"`
	Route       string   `json:"route"`
	RouteType   string   `json:"routeType"`
	Backend     string   `json:"backend,omitempty"`
	BackendPort string   `json:"backendPort,omitempty"`
	SuccessRate float64  `json:"successRate"`
	RPS         float64  `json:"rps"`
	LatencyP50  *float64 `json:"latencyMsP50"`
	LatencyP95  *float64 `json:"latencyMsP95"`
	LatencyP99  *float64 `json:"latencyMsP99"`
	TimeoutRate float64  `json:"timeoutRate"`
	RetryRate   *float64 `json:"retryRate,omitempty"`
}

// NewCmdStatOutbound creates a new cobra command `stat-outbound` for outbound stat functionality
func NewCmdStatOutbound() *cobra.Command {
	options := newStatInboundOptions()

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

			if resource.Type == k8s.Authority {
				return fmt.Errorf("Resource type is not supported: %s", resource.Type)
			}

			// Issue Prometheus queries for HTTP.

			httpBackendChan := queryRate(
				cmd.Context(),
				promApi,
				"outbound_http_route_backend_response_statuses_total",
				options.timeWindow,
				model.LabelSet{},
				model.LabelNames{"parent_name", "parent_port", "backend_name", "backend_port", "route_name", "route_kind", "http_status", "error"},
				resource,
			)

			httpBackendQuantiles := queryQuantiles(
				cmd.Context(),
				promApi,
				"outbound_http_route_backend_response_duration_seconds_bucket",
				options.timeWindow,
				model.LabelSet{},
				model.LabelNames{"parent_name", "parent_port", "backend_name", "backend_port", "route_name", "route_kind"},
				resource,
			)

			httpRouteChan := queryRate(
				cmd.Context(),
				promApi,
				"outbound_http_route_request_statuses_total",
				options.timeWindow,
				model.LabelSet{},
				model.LabelNames{"parent_name", "parent_port", "route_name", "route_kind", "http_status", "error"},
				resource,
			)

			httpRetiesChan := queryRate(
				cmd.Context(),
				promApi,
				"outbound_http_route_retry_requests_total",
				options.timeWindow,
				model.LabelSet{},
				model.LabelNames{"parent_name", "parent_port", "route_name", "route_kind"},
				resource,
			)

			httpRouteQuantiles := queryQuantiles(
				cmd.Context(),
				promApi,
				"outbound_http_route_request_duration_seconds_bucket",
				options.timeWindow,
				model.LabelSet{},
				model.LabelNames{"parent_name", "parent_port", "route_name", "route_kind"},
				resource,
			)

			// Issue Prometheus queries for gRPC.

			grpcBackendChan := queryRate(
				cmd.Context(),
				promApi,
				"outbound_grpc_route_backend_response_statuses_total",
				options.timeWindow,
				model.LabelSet{},
				model.LabelNames{"parent_name", "parent_port", "backend_name", "backend_port", "route_name", "route_kind", "grpc_status", "error"},
				resource,
			)

			grpcBackendQuantiles := queryQuantiles(
				cmd.Context(),
				promApi,
				"outbound_grpc_route_backend_response_duration_seconds_bucket",
				options.timeWindow,
				model.LabelSet{},
				model.LabelNames{"parent_name", "parent_port", "backend_name", "backend_port", "route_name", "route_kind"},
				resource,
			)

			grpcRouteChan := queryRate(
				cmd.Context(),
				promApi,
				"outbound_grpc_route_request_statuses_total",
				options.timeWindow,
				model.LabelSet{},
				model.LabelNames{"parent_name", "parent_port", "route_name", "route_kind", "grpc_status", "error"},
				resource,
			)

			grpcRetiesChan := queryRate(
				cmd.Context(),
				promApi,
				"outbound_grpc_route_retry_requests_total",
				options.timeWindow,
				model.LabelSet{},
				model.LabelNames{"parent_name", "parent_port", "route_name", "route_kind"},
				resource,
			)

			grpcRouteQuantiles := queryQuantiles(
				cmd.Context(),
				promApi,
				"outbound_grpc_route_request_duration_seconds_bucket",
				options.timeWindow,
				model.LabelSet{},
				model.LabelNames{"parent_name", "parent_port", "route_name", "route_kind"},
				resource,
			)

			// Collect Prometheus results for HTTP.

			results := make(map[outboundRowKey]outboundRouteRow)

			for sample := range httpBackendChan {
				key := outboundKeyForSample(sample, resource)
				row := results[key]
				row.populateBackendHTTPCounts(sample)
				results[key] = row
			}

			for quantile, resultsChan := range httpBackendQuantiles {
				for sample := range resultsChan {
					key := outboundKeyForSample(sample, resource)
					row := results[key]
					row.populateBackendLatency(quantile, sample)
					results[key] = row
				}
			}

			for sample := range httpRouteChan {
				key := outboundKeyForSample(sample, resource)
				row := results[key]
				row.populateHTTPCounts(sample)
				results[key] = row
			}
			for sample := range httpRetiesChan {
				key := outboundKeyForSample(sample, resource)
				row := results[key]
				row.retries += uint64(sample.Value)
				results[key] = row
			}
			for quantile, resultsChan := range httpRouteQuantiles {
				for sample := range resultsChan {
					key := outboundKeyForSample(sample, resource)
					row := results[key]
					row.populateLatency(quantile, sample)
					results[key] = row
				}
			}

			// Collect Prometheus results for gRPC.

			for sample := range grpcBackendChan {
				key := outboundKeyForSample(sample, resource)
				row := results[key]
				row.populateBackendGRPCCounts(sample)
				results[key] = row
			}

			for quantile, resultsChan := range grpcBackendQuantiles {
				for sample := range resultsChan {
					key := outboundKeyForSample(sample, resource)
					row := results[key]
					row.populateBackendLatency(quantile, sample)
					results[key] = row
				}
			}

			for sample := range grpcRouteChan {
				key := outboundKeyForSample(sample, resource)
				row := results[key]
				row.populateGRPCCounts(sample)
				results[key] = row
			}
			for sample := range grpcRetiesChan {
				key := outboundKeyForSample(sample, resource)
				row := results[key]
				row.retries += uint64(sample.Value)
				results[key] = row
			}
			for quantile, resultsChan := range grpcRouteQuantiles {
				for sample := range resultsChan {
					key := outboundKeyForSample(sample, resource)
					row := results[key]
					row.populateLatency(quantile, sample)
					results[key] = row
				}
			}

			// Render output.

			windowLength, err := time.ParseDuration(options.timeWindow)
			if err != nil {
				return err
			}

			if options.outputFormat == "json" {
				return renderStatOutboundJSON(results, windowLength)
			} else if options.outputFormat == "table" || options.outputFormat == "" {
				renderStatOutboundTable(results, windowLength)
				return nil
			} else {
				return fmt.Errorf("Invalid output format: %s", options.outputFormat)
			}
		},
	}

	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", options.outputFormat, "Output format; one of: \"table\" or \"json\"")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace")
	cmd.PersistentFlags().StringVarP(&options.timeWindow, "time-window", "t", options.timeWindow, "Stat window (for example: \"10s\", \"1m\", \"10m\", \"1h\")")
	cmd.PersistentFlags().StringVar(&options.prometheusURL, "prometheusURL", options.prometheusURL, "Address of Prometheus instance to query")

	return cmd
}

func outboundKeyForSample(sample *model.Sample, resource *pb.Resource) outboundRowKey {
	labels := sample.Metric
	route := (string)(labels["route_name"])
	routeType := (string)(labels["route_kind"])
	if labels["route_name"] == "default" {
		route = "[default]"
	}
	if labels["route_kind"] == "default" {
		routeType = ""
		route = "[default]"
	}

	return outboundRowKey{
		name:      (string)(labels[prometheus.ResourceType(resource)]),
		service:   (string)(labels["parent_name"]),
		port:      (string)(labels["parent_port"]),
		route:     route,
		routeType: routeType,
	}
}

func (r *outboundBackendRow) populateLatency(quantile string, sample *model.Sample) {
	switch quantile {
	case "0.5":
		r.latencyP50 = float64(sample.Value * 1000)
	case "0.95":
		r.latencyP95 = float64(sample.Value * 1000)
	case "0.99":
		r.latencyP99 = float64(sample.Value * 1000)
	}
}

func (r *outboundRouteRow) populateBackendLatency(quantile string, sample *model.Sample) {
	key := backendKey{
		name: (string)(sample.Metric["backend_name"]),
		port: (string)(sample.Metric["backend_port"]),
	}
	backend := r.backends[key]
	backend.populateLatency(quantile, sample)
	if r.backends == nil {
		r.backends = make(map[backendKey]outboundBackendRow)
	}
	r.backends[key] = backend
}

func (r *outboundBackendRow) populateHTTPCounts(sample *model.Sample) {
	labels := sample.Metric
	if strings.HasPrefix((string)(labels["http_status"]), "5") || labels["error"] != "" {
		r.failures += uint64(sample.Value)
	} else {
		r.successes += uint64(sample.Value)
	}
	if labels["error"] == "RESPONSE_HEADERS_TIMEOUT" || labels["error"] == "REQUEST_TIMEOUT" {
		r.timeouts += uint64(sample.Value)
	}
}

func (r *outboundRouteRow) populateBackendHTTPCounts(sample *model.Sample) {
	key := backendKey{
		name: (string)(sample.Metric["backend_name"]),
		port: (string)(sample.Metric["backend_port"]),
	}
	backend := r.backends[key]
	backend.populateHTTPCounts(sample)
	if r.backends == nil {
		r.backends = make(map[backendKey]outboundBackendRow)
	}
	r.backends[key] = backend
}

func (r *outboundBackendRow) populateGRPCCounts(sample *model.Sample) {
	labels := sample.Metric
	if labels["grpc_status"] != "OK" || labels["error"] != "" {
		r.failures += uint64(sample.Value)
	} else {
		r.successes += uint64(sample.Value)
	}
	if labels["error"] == "RESPONSE_HEADERS_TIMEOUT" || labels["error"] == "REQUEST_TIMEOUT" {
		r.timeouts += uint64(sample.Value)
	}
}

func (r *outboundRouteRow) populateBackendGRPCCounts(sample *model.Sample) {
	key := backendKey{
		name: (string)(sample.Metric["backend_name"]),
		port: (string)(sample.Metric["backend_port"]),
	}
	backend := r.backends[key]
	backend.populateGRPCCounts(sample)
	if r.backends == nil {
		r.backends = make(map[backendKey]outboundBackendRow)
	}
	r.backends[key] = backend
}

func renderStatOutboundTable(results map[outboundRowKey]outboundRouteRow, windowLength time.Duration) {
	rows := make([][]string, 0)

	for key, row := range results {
		if row.failures+row.successes == 0 {
			continue
		}
		rows = append(rows, []string{
			key.name,
			fmt.Sprintf("%s:%s", key.service, key.port),
			key.route,
			key.routeType,
			"", // backend
			fmt.Sprintf("%.2f%%", (float32)(row.successes)/(float32)(row.successes+row.failures)*100.0),
			fmt.Sprintf("%.2f", (float32)(row.successes+row.failures)/float32(windowLength.Seconds())),
			formatLatencyMs(row.latencyP50),
			formatLatencyMs(row.latencyP95),
			formatLatencyMs(row.latencyP99),
			fmt.Sprintf("%.2f%%", (float32)(row.timeouts)/(float32)(row.successes+row.failures)*100.0),
			fmt.Sprintf("%.2f%%", (float32)(row.retries)/(float32)(row.successes+row.failures+row.retries)*100.0),
		})
		for backend, backendRow := range row.backends {
			rows = append(rows, []string{
				"",
				"",
				"├─",
				"─►",
				fmt.Sprintf("%s:%s", backend.name, backend.port),
				fmt.Sprintf("%.2f%%", (float32)(backendRow.successes)/(float32)(backendRow.successes+backendRow.failures)*100.0),
				fmt.Sprintf("%.2f", (float32)(backendRow.successes+backendRow.failures)/float32(windowLength.Seconds())),
				formatLatencyMs(backendRow.latencyP50),
				formatLatencyMs(backendRow.latencyP95),
				formatLatencyMs(backendRow.latencyP99),
				fmt.Sprintf("%.2f%%", (float32)(backendRow.timeouts)/(float32)(backendRow.successes+backendRow.failures)*100.0),
				"", // retries
			})
		}
		lastBackendLine := rows[len(rows)-1][2]
		rows[len(rows)-1][2] = strings.Replace(lastBackendLine, "├", "└", 1)
	}

	columns := []table.Column{
		table.NewColumn("NAME").WithLeftAlign(),
		table.NewColumn("SERVICE").WithLeftAlign(),
		table.NewColumn("ROUTE").WithLeftAlign(),
		table.NewColumn("TYPE").WithLeftAlign(),
		table.NewColumn("BACKEND").WithLeftAlign(),
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
}

func renderStatOutboundJSON(results map[outboundRowKey]outboundRouteRow, windowLength time.Duration) error {
	rows := make([]outboundJsonRow, 0)

	for key, result := range results {
		result := result // To avoid golangci-lint complaining about memory aliasing.
		if result.failures+result.successes == 0 {
			continue
		}
		retryRate := (float64)(result.retries) / (float64)(result.successes+result.failures+result.retries)
		row := outboundJsonRow{
			Name:        key.name,
			Service:     key.service,
			Port:        key.port,
			Route:       key.route,
			RouteType:   key.routeType,
			SuccessRate: (float64)(result.successes) / (float64)(result.successes+result.failures),
			RPS:         (float64)(result.successes+result.failures) / windowLength.Seconds(),
			LatencyP50:  &result.latencyP50,
			LatencyP95:  &result.latencyP95,
			LatencyP99:  &result.latencyP99,
			TimeoutRate: (float64)(result.timeouts) / (float64)(result.successes+result.failures),
			RetryRate:   &retryRate,
		}
		if math.IsNaN(result.latencyP50) {
			row.LatencyP50 = nil
		}
		if math.IsNaN(result.latencyP95) {
			row.LatencyP95 = nil
		}
		if math.IsNaN(result.latencyP99) {
			row.LatencyP99 = nil
		}

		rows = append(rows, row)

		for backend, result := range result.backends {
			result := result // To avoid golangci-lint complaining about memory aliasing.
			if result.failures+result.successes == 0 {
				continue
			}
			row := outboundJsonRow{
				Name:        key.name,
				Service:     key.service,
				Port:        key.port,
				Route:       key.route,
				RouteType:   key.routeType,
				Backend:     backend.name,
				BackendPort: backend.port,
				SuccessRate: (float64)(result.successes) / (float64)(result.successes+result.failures),
				RPS:         (float64)(result.successes+result.failures) / windowLength.Seconds(),
				LatencyP50:  &result.latencyP50,
				LatencyP95:  &result.latencyP95,
				LatencyP99:  &result.latencyP99,
				TimeoutRate: (float64)(result.timeouts) / (float64)(result.successes+result.failures),
			}
			if math.IsNaN(result.latencyP50) {
				row.LatencyP50 = nil
			}
			if math.IsNaN(result.latencyP95) {
				row.LatencyP95 = nil
			}
			if math.IsNaN(result.latencyP99) {
				row.LatencyP99 = nil
			}
			rows = append(rows, row)
		}
	}

	out, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
