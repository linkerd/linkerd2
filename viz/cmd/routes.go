package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	coreUtil "github.com/linkerd/linkerd2/controller/api/util"
	pkgcmd "github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/metrics-api/util"
	"github.com/linkerd/linkerd2/viz/pkg"
	"github.com/linkerd/linkerd2/viz/pkg/api"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type routesOptions struct {
	namespace string
	statOptionsBase
	toResource    string
	toNamespace   string
	dstIsService  bool
	labelSelector string
}

type routeRowStats struct {
	rowStats
	actualRequestRate float64
	actualSuccessRate float64
	hasRequestData    bool
}

func newRoutesOptions() *routesOptions {
	return &routesOptions{
		statOptionsBase: *newStatOptionsBase(),
		toResource:      "",
		toNamespace:     "",
		labelSelector:   "",
	}
}

// NewCmdRoutes creates a new cobra command `routes` for routes functionality
func NewCmdRoutes() *cobra.Command {
	options := newRoutesOptions()

	cmd := &cobra.Command{
		Use:   "routes [flags] (RESOURCES)",
		Short: "Display route stats",
		Long: `Display route stats.

This command will only display traffic which is sent to a service that has a Service Profile defined.`,
		Example: `  # Routes for the webapp service in the test namespace.
  linkerd viz routes service/webapp -n test

  # Routes for calls from the traffic deployment to the webapp service in the test namespace.
  linkerd viz routes deploy/traffic -n test --to svc/webapp`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: pkg.ValidTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.namespace == "" {
				options.namespace = pkgcmd.GetDefaultNamespace(kubeconfigPath, kubeContext)
			}
			req, err := buildTopRoutesRequest(args[0], options)
			if err != nil {
				return fmt.Errorf("error creating metrics request while making routes request: %v", err)
			}

			output, err := requestRouteStatsFromAPI(
				api.CheckClientOrExit(healthcheck.Options{
					ControlPlaneNamespace: controlPlaneNamespace,
					KubeConfig:            kubeconfigPath,
					Impersonate:           impersonate,
					ImpersonateGroup:      impersonateGroup,
					KubeContext:           kubeContext,
					APIAddr:               apiAddr,
				}),
				req,
				options,
			)
			if err != nil {
				fmt.Fprint(os.Stderr, err.Error())
				os.Exit(1)
			}

			_, err = fmt.Print(output)

			return err
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the specified resource")
	cmd.PersistentFlags().StringVarP(&options.timeWindow, "time-window", "t", options.timeWindow, "Stat window (for example: \"10s\", \"1m\", \"10m\", \"1h\")")
	cmd.PersistentFlags().StringVar(&options.toResource, "to", options.toResource, "If present, shows outbound stats to the specified resource")
	cmd.PersistentFlags().StringVar(&options.toNamespace, "to-namespace", options.toNamespace, "Sets the namespace used to lookup the \"--to\" resource; by default the current \"--namespace\" is used")
	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", options.outputFormat, fmt.Sprintf("Output format; one of: \"%s\", \"%s\", or \"%s\"", tableOutput, wideOutput, jsonOutput))
	cmd.PersistentFlags().StringVarP(&options.labelSelector, "selector", "l", options.labelSelector, "Selector (label query) to filter on, supports '=', '==', and '!='")

	return cmd
}

func requestRouteStatsFromAPI(client pb.ApiClient, req *pb.TopRoutesRequest, options *routesOptions) (string, error) {
	resp, err := client.TopRoutes(context.Background(), req)
	if err != nil {
		return "", fmt.Errorf("TopRoutes API error: %v", err)
	}
	if e := resp.GetError(); e != nil {
		return "", errors.New(e.Error)
	}

	return renderRouteStats(resp, options), nil
}

func renderRouteStats(resp *pb.TopRoutesResponse, options *routesOptions) string {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', tabwriter.AlignRight)
	writeRouteStatsToBuffer(resp, w, options)
	w.Flush()

	return renderStats(buffer, &options.statOptionsBase)
}

func writeRouteStatsToBuffer(resp *pb.TopRoutesResponse, w *tabwriter.Writer, options *routesOptions) {

	tables := make(map[string][]*routeRowStats)

	for _, resourceTable := range resp.GetOk().GetRoutes() {

		table := make([]*routeRowStats, 0)

		for _, r := range resourceTable.GetRows() {
			if r.Stats != nil {
				route := r.GetRoute()
				table = append(table, &routeRowStats{
					rowStats: rowStats{
						route:       route,
						dst:         r.GetAuthority(),
						requestRate: getRequestRate(r.Stats.GetSuccessCount(), r.Stats.GetFailureCount(), r.TimeWindow),
						successRate: getSuccessRate(r.Stats.GetSuccessCount(), r.Stats.GetFailureCount()),
						latencyP50:  r.Stats.LatencyMsP50,
						latencyP95:  r.Stats.LatencyMsP95,
						latencyP99:  r.Stats.LatencyMsP99,
					},
					actualRequestRate: getRequestRate(r.Stats.GetActualSuccessCount(), r.Stats.GetActualFailureCount(), r.TimeWindow),
					actualSuccessRate: getSuccessRate(r.Stats.GetActualSuccessCount(), r.Stats.GetActualFailureCount()),
					hasRequestData:    statHasRequestData(r.Stats),
				})
			}
		}

		sort.Slice(table, func(i, j int) bool {
			return table[i].dst+table[i].route < table[j].dst+table[j].route
		})

		tables[resourceTable.GetResource()] = table
	}

	resources := make([]string, 0)
	for resource := range tables {
		resources = append(resources, resource)
	}
	sort.Strings(resources)

	switch options.outputFormat {
	case tableOutput, wideOutput:
		for _, resource := range resources {
			if len(tables) > 1 {
				fmt.Fprintf(w, "==> %s <==\t\f", resource)
			}
			printRouteTable(tables[resource], w, options)
			fmt.Fprintln(w)
		}
	case jsonOutput:
		printRouteJSON(tables, w, options)
	}
}

func printRouteTable(stats []*routeRowStats, w *tabwriter.Writer, options *routesOptions) {
	// template for left-aligning the route column
	routeTemplate := fmt.Sprintf("%%-%ds", routeWidth(stats))

	authorityColumn := "AUTHORITY"
	if options.dstIsService {
		authorityColumn = "SERVICE"
	}

	headers := []string{
		fmt.Sprintf(routeTemplate, "ROUTE"),
		authorityColumn,
	}
	outputActual := options.toResource != "" && options.outputFormat == wideOutput
	if outputActual {
		headers = append(headers, []string{
			"EFFECTIVE_SUCCESS",
			"EFFECTIVE_RPS",
			"ACTUAL_SUCCESS",
			"ACTUAL_RPS",
		}...)
	} else {
		headers = append(headers, []string{
			"SUCCESS",
			"RPS",
		}...)
	}

	headers = append(headers, []string{
		"LATENCY_P50",
		"LATENCY_P95",
		"LATENCY_P99\t", // trailing \t is required to format last column
	}...)

	fmt.Fprintln(w, strings.Join(headers, "\t"))

	// route, success rate, rps
	templateString := routeTemplate + "\t%s\t%.2f%%\t%.1frps\t"
	if outputActual {
		// actual success rate, actual rps
		templateString = templateString + "%.2f%%\t%.1frps\t"
	}
	// p50, p95, p99
	templateString = templateString + "%dms\t%dms\t%dms\t\n"

	var emptyTemplateString string
	if outputActual {
		emptyTemplateString = routeTemplate + "\t%s\t-\t-\t-\t-\t-\t-\t-\t\n"
	} else {
		emptyTemplateString = routeTemplate + "\t%s\t-\t-\t-\t-\t-\t\n"
	}

	for _, row := range stats {

		values := []interface{}{
			row.route,
			row.dst,
		}

		if row.hasRequestData {
			values = append(values, []interface{}{
				row.successRate * 100,
				row.requestRate,
			}...)

			if outputActual {
				values = append(values, []interface{}{
					row.actualSuccessRate * 100,
					row.actualRequestRate,
				}...)
			}
			values = append(values, []interface{}{
				row.latencyP50,
				row.latencyP95,
				row.latencyP99,
			}...)

			fmt.Fprintf(w, templateString, values...)
		} else {
			fmt.Fprintf(w, emptyTemplateString, values...)
		}
	}
}

// getRequestRate calculates request rate from Public API BasicStats.
func getRequestRate(success, failure uint64, timeWindow string) float64 {
	windowLength, err := time.ParseDuration(timeWindow)
	if err != nil {
		log.Error(err.Error())
		return 0.0
	}
	return float64(success+failure) / windowLength.Seconds()
}

// getSuccessRate calculates success rate from Public API BasicStats.
func getSuccessRate(success, failure uint64) float64 {
	if success+failure == 0 {
		return 0.0
	}
	return float64(success) / float64(success+failure)
}

// JSONRouteStats represents the JSON output of the routes command
// Using pointers there where the value is NA and the corresponding json is null
type JSONRouteStats struct {
	Route            string   `json:"route"`
	Authority        string   `json:"authority"`
	Success          *float64 `json:"success,omitempty"`
	Rps              *float64 `json:"rps,omitempty"`
	EffectiveSuccess *float64 `json:"effective_success,omitempty"`
	EffectiveRps     *float64 `json:"effective_rps,omitempty"`
	ActualSuccess    *float64 `json:"actual_success,omitempty"`
	ActualRps        *float64 `json:"actual_rps,omitempty"`
	LatencyMSp50     *uint64  `json:"latency_ms_p50"`
	LatencyMSp95     *uint64  `json:"latency_ms_p95"`
	LatencyMSp99     *uint64  `json:"latency_ms_p99"`
}

func printRouteJSON(tables map[string][]*routeRowStats, w *tabwriter.Writer, options *routesOptions) {
	// avoid nil initialization so that if there are not stats it gets marshalled as an empty array vs null
	entries := map[string][]*JSONRouteStats{}
	for resource, table := range tables {
		for _, row := range table {
			route := row.route
			entry := &JSONRouteStats{
				Route: route,
			}

			entry.Authority = row.dst
			if options.toResource != "" {
				entry.EffectiveSuccess = &row.successRate
				entry.EffectiveRps = &row.requestRate
				entry.ActualSuccess = &row.actualSuccessRate
				entry.ActualRps = &row.actualRequestRate
			} else {
				entry.Success = &row.successRate
				entry.Rps = &row.requestRate
			}
			entry.LatencyMSp50 = &row.latencyP50
			entry.LatencyMSp95 = &row.latencyP95
			entry.LatencyMSp99 = &row.latencyP99

			entries[resource] = append(entries[resource], entry)
		}
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		log.Error(err.Error())
		return
	}
	fmt.Fprintf(w, "%s\n", b)
}

func (o *routesOptions) validateOutputFormat() error {
	switch o.outputFormat {
	case tableOutput, jsonOutput:
		return nil
	case wideOutput:
		if o.toResource == "" {
			return fmt.Errorf("%s output is only available when --to is specified", wideOutput)
		}
		return nil
	default:
		return fmt.Errorf("--output currently only supports %s, %s, and %s", tableOutput, wideOutput, jsonOutput)
	}
}

func buildTopRoutesRequest(resource string, options *routesOptions) (*pb.TopRoutesRequest, error) {
	err := options.validateOutputFormat()
	if err != nil {
		return nil, err
	}

	target, err := coreUtil.BuildResource(options.namespace, resource)
	if err != nil {
		return nil, err
	}

	requestParams := util.TopRoutesRequestParams{
		StatsBaseRequestParams: util.StatsBaseRequestParams{
			TimeWindow:   options.timeWindow,
			ResourceName: target.Name,
			ResourceType: target.Type,
			Namespace:    options.namespace,
		},
		LabelSelector: options.labelSelector,
	}

	options.dstIsService = target.GetType() != k8s.Authority

	if options.toResource != "" {
		if options.toNamespace == "" {
			options.toNamespace = options.namespace
		}
		toRes, err := coreUtil.BuildResource(options.toNamespace, options.toResource)
		if err != nil {
			return nil, err
		}

		options.dstIsService = toRes.GetType() != k8s.Authority

		requestParams.ToName = toRes.Name
		requestParams.ToNamespace = toRes.Namespace
		requestParams.ToType = toRes.Type
	}

	return util.BuildTopRoutesRequest(requestParams)
}

// returns the length of the longest route name
func routeWidth(stats []*routeRowStats) int {
	maxLength := 0
	for _, row := range stats {
		if len(row.route) > maxLength {
			maxLength = len(row.route)
		}
	}
	return maxLength
}
