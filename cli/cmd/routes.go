package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/linkerd/linkerd2/controller/api/util"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type routesOptions struct {
	namespace     string
	timeWindow    string
	toNamespace   string
	toResource    string
	fromNamespace string
	fromResource  string
	allNamespaces bool
	outputFormat  string
}

func newRoutesOptions() *routesOptions {
	return &routesOptions{
		namespace:     "default",
		timeWindow:    "1m",
		toNamespace:   "",
		toResource:    "",
		fromNamespace: "",
		fromResource:  "",
		allNamespaces: false,
		outputFormat:  "",
	}
}

func newCmdRoutes() *cobra.Command {
	options := newRoutesOptions()

	cmd := &cobra.Command{
		Use:   "routes [flags] (RESOURCE)",
		Short: "Display route stats about one or many resources",
		Long: `Display route stats about one or many resources.

  The RESOURCE argument specifies the target resource(s) to aggregate stats over:
  (TYPE [NAME] | TYPE/NAME)

  Examples:
  * deploy
  * deploy/my-deploy
  * rc/my-replication-controller
  * ns/my-ns
  * authority
  * au/my-authority
  * all

Valid resource types include:

  * deployments
  * namespaces
  * pods
  * replicationcontrollers
  * authorities (not supported in --from)
  * services (only supported if a --from is also specified, or as a --to)
  * all (all resource types, not supported in --from or --to)

This command will hide resources that have completed, such as pods that are in the Succeeded or Failed phases.
If no resource name is specified, displays stats about all resources of the specified RESOURCETYPE`,
		Example: `  # Get all deployments in the test namespace.
  linkerd routes deployments -n test

  # Get the hello1 replication controller in the test namespace.
  linkerd routes replicationcontrollers hello1 -n test

  # Get all namespaces.
  linkerd routes namespaces

  # Get all inbound stats to the web deployment.
  linkerd routes deploy/web

  # Get all pods in all namespaces that call the hello1 deployment in the test namesapce.
  linkerd routes pods --to deploy/hello1 --to-namespace test --all-namespaces

  # Get all pods in all namespaces that call the hello1 service in the test namesapce.
  linkerd routes pods --to svc/hello1 --to-namespace test --all-namespaces

  # Get all services in all namespaces that receive calls from hello1 deployment in the test namespace.
  linkerd routes services --from deploy/hello1 --from-namespace test --all-namespaces

  # Get all namespaces that receive traffic from the default namespace.
  linkerd routes namespaces --from ns/default

  # Get all inbound stats to the test namespace.
  linkerd routes ns/test`,
		Args:      cobra.RangeArgs(1, 2),
		ValidArgs: util.ValidTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildTopRoutesRequest(args, options)
			if err != nil {
				return fmt.Errorf("error creating metrics request while making routes request: %v", err)
			}

			output, err := requestRouteStatsFromAPI(validatedPublicAPIClient(time.Time{}), req, options)
			if err != nil {
				return err
			}

			_, err = fmt.Print(output)

			return err
		},
	}

	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the specified resource")
	cmd.PersistentFlags().StringVarP(&options.timeWindow, "time-window", "t", options.timeWindow, "Stat window (for example: \"10s\", \"1m\", \"10m\", \"1h\")")
	cmd.PersistentFlags().StringVar(&options.toResource, "to", options.toResource, "If present, restricts outbound stats to the specified resource name")
	cmd.PersistentFlags().StringVar(&options.toNamespace, "to-namespace", options.toNamespace, "Sets the namespace used to lookup the \"--to\" resource; by default the current \"--namespace\" is used")
	cmd.PersistentFlags().StringVar(&options.fromResource, "from", options.fromResource, "If present, restricts outbound stats from the specified resource name")
	cmd.PersistentFlags().StringVar(&options.fromNamespace, "from-namespace", options.fromNamespace, "Sets the namespace used from lookup the \"--from\" resource; by default the current \"--namespace\" is used")
	cmd.PersistentFlags().BoolVar(&options.allNamespaces, "all-namespaces", options.allNamespaces, "If present, returns stats across all namespaces, ignoring the \"--namespace\" flag")
	cmd.PersistentFlags().StringVarP(&options.outputFormat, "output", "o", options.outputFormat, "Output format; currently only \"table\" (default) and \"json\" are supported")

	return cmd
}

func requestRouteStatsFromAPI(client pb.ApiClient, req *pb.TopRoutesRequest, options *routesOptions) (string, error) {
	resp, err := client.TopRoutes(context.Background(), req)
	if err != nil {
		return "", fmt.Errorf("TopRoutes API error: %v", err)
	}
	if e := resp.GetError(); e != nil {
		return "", fmt.Errorf("TopRoutes API response error: %v", e.Error)
	}

	return renderRouteStats(resp, req.Selector.Resource.Type, options), nil
}

func renderRouteStats(resp *pb.TopRoutesResponse, resourceType string, options *routesOptions) string {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', tabwriter.AlignRight)
	writeRouteStatsToBuffer(resp, resourceType, w, options)
	w.Flush()

	var out string
	switch options.outputFormat {
	case "table", "":
		// strip left padding on the first column
		out = string(buffer.Bytes()[padding:])
		out = strings.Replace(out, "\n"+strings.Repeat(" ", padding), "\n", -1)
	case "json":
		out = string(buffer.Bytes())
	}

	return out
}

type routeRow struct {
	requestRate float64
	successRate float64
	tlsPercent  float64
	latencyP50  uint64
	latencyP95  uint64
	latencyP99  uint64
}

func writeRouteStatsToBuffer(resp *pb.TopRoutesResponse, reqResourceType string, w *tabwriter.Writer, options *routesOptions) {
	table := make(map[string]*routeRow)

	for _, r := range resp.GetRoutes().Rows {
		if r.Stats != nil {
			table[r.Route] = &routeRow{
				requestRate: getRouteRequestRate(*r),
				successRate: getRouteSuccessRate(*r),
				tlsPercent:  getRoutePercentTls(*r),
				latencyP50:  r.Stats.LatencyMsP50,
				latencyP95:  r.Stats.LatencyMsP95,
				latencyP99:  r.Stats.LatencyMsP99,
			}
		}
	}

	switch options.outputFormat {
	case "table", "":
		if len(table) == 0 {
			fmt.Fprintln(os.Stderr, "No traffic found.")
			os.Exit(0)
		}
		printRouteTable(table, w, options)
	case "json":
		printRouteJson(table, w)
	}
}

func printRouteTable(stats map[string]*routeRow, w *tabwriter.Writer, options *routesOptions) {
	headers := []string{
		"ROUTE",
		"SUCCESS",
		"RPS",
		"LATENCY_P50",
		"LATENCY_P95",
		"LATENCY_P99",
		"TLS\t", // trailing \t is required to format last column
	}

	fmt.Fprintln(w, strings.Join(headers, "\t"))

	templateString := "%s\t%.2f%%\t%.1frps\t%dms\t%dms\t%dms\t%.f%%\t\n"
	templateStringEmpty := "%s\t-\t-\t-\t-\t-\t-\t\n"

	sortedRoutes := sortRoutesByRps(stats)
	for _, route := range sortedRoutes {

		if row, ok := stats[route]; ok {
			fmt.Fprintf(w, templateString, route,
				row.successRate*100,
				row.requestRate,
				row.latencyP50,
				row.latencyP95,
				row.latencyP99,
				row.tlsPercent*100,
			)
		} else {
			fmt.Fprintf(w, templateStringEmpty, route)
		}
	}
}

// Using pointers there where the value is NA and the corresponding json is null
type jsonRouteStats struct {
	Route        string   `json:"route"`
	Success      *float64 `json:"success"`
	Rps          *float64 `json:"rps"`
	LatencyMSp50 *uint64  `json:"latency_ms_p50"`
	LatencyMSp95 *uint64  `json:"latency_ms_p95"`
	LatencyMSp99 *uint64  `json:"latency_ms_p99"`
	Tls          *float64 `json:"tls"`
}

func printRouteJson(stats map[string]*routeRow, w *tabwriter.Writer) {
	// avoid nil initialization so that if there are not stats it gets marshalled as an empty array vs null
	entries := []*jsonRouteStats{}
	sortedRoutes := sortRoutesByRps(stats)
	for _, route := range sortedRoutes {
		entry := &jsonRouteStats{
			Route: route,
		}
		if row, ok := stats[route]; ok {
			entry.Success = &row.successRate
			entry.Rps = &row.requestRate
			entry.LatencyMSp50 = &row.latencyP50
			entry.LatencyMSp95 = &row.latencyP95
			entry.LatencyMSp99 = &row.latencyP99
			entry.Tls = &stats[route].tlsPercent
		}
		entries = append(entries, entry)
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		log.Error(err.Error())
		return
	}
	fmt.Fprintf(w, "%s\n", b)
}

func buildTopRoutesRequest(resource []string, options *routesOptions) (*pb.TopRoutesRequest, error) {
	target, err := util.BuildResource(options.namespace, resource...)
	if err != nil {
		return nil, err
	}

	err = options.validate(target.Type)
	if err != nil {
		return nil, err
	}

	var toRes, fromRes pb.Resource
	if options.toResource != "" {
		toRes, err = util.BuildResource(options.toNamespace, options.toResource)
		if err != nil {
			return nil, err
		}
	}
	if options.fromResource != "" {
		fromRes, err = util.BuildResource(options.fromNamespace, options.fromResource)
		if err != nil {
			return nil, err
		}
	}

	requestParams := util.StatSummaryRequestParams{
		TimeWindow:    options.timeWindow,
		ResourceName:  target.Name,
		ResourceType:  target.Type,
		Namespace:     options.namespace,
		ToName:        toRes.Name,
		ToType:        toRes.Type,
		ToNamespace:   options.toNamespace,
		FromName:      fromRes.Name,
		FromType:      fromRes.Type,
		FromNamespace: options.fromNamespace,
		AllNamespaces: options.allNamespaces,
	}

	return util.BuildTopRoutesRequest(requestParams)
}

func getRouteRequestRate(r pb.RouteTable_Row) float64 {
	success := r.Stats.SuccessCount
	failure := r.Stats.FailureCount
	windowLength, err := time.ParseDuration(r.TimeWindow)
	if err != nil {
		log.Error(err.Error())
		return 0.0
	}
	return float64(success+failure) / windowLength.Seconds()
}

func getRouteSuccessRate(r pb.RouteTable_Row) float64 {
	success := r.Stats.SuccessCount
	failure := r.Stats.FailureCount

	if success+failure == 0 {
		return 0.0
	}
	return float64(success) / float64(success+failure)
}

func getRoutePercentTls(r pb.RouteTable_Row) float64 {
	reqTotal := r.Stats.SuccessCount + r.Stats.FailureCount
	if reqTotal == 0 {
		return 0.0
	}
	return float64(r.Stats.TlsRequestCount) / float64(reqTotal)
}

func sortRoutesByRps(stats map[string]*routeRow) []string {
	var sortedRoutes []string
	for key := range stats {
		sortedRoutes = append(sortedRoutes, key)
	}
	sort.Slice(sortedRoutes, func(i, j int) bool {
		return stats[sortedRoutes[i]].requestRate > stats[sortedRoutes[j]].requestRate
	})
	return sortedRoutes
}

// validate performs all validation on the command-line options.
// It returns the first error encountered, or `nil` if the options are valid.
func (o *routesOptions) validate(resourceType string) error {
	err := o.validateConflictingFlags()
	if err != nil {
		return err
	}

	if resourceType == k8s.Namespace {
		err := o.validateNamespaceFlags()
		if err != nil {
			return err
		}
	}

	if err := o.validateOutputFormat(); err != nil {
		return err
	}

	return nil
}

// validateConflictingFlags validates that the options do not contain mutually
// exclusive flags.
func (o *routesOptions) validateConflictingFlags() error {
	if o.toResource != "" && o.fromResource != "" {
		return fmt.Errorf("--to and --from flags are mutually exclusive")
	}

	if o.toNamespace != "" && o.fromNamespace != "" {
		return fmt.Errorf("--to-namespace and --from-namespace flags are mutually exclusive")
	}

	return nil
}

// validateNamespaceFlags performs additional validation for options when the target
// resource type is a namespace.
func (o *routesOptions) validateNamespaceFlags() error {
	if o.toNamespace != "" {
		return fmt.Errorf("--to-namespace flag is incompatible with namespace resource type")
	}

	if o.fromNamespace != "" {
		return fmt.Errorf("--from-namespace flag is incompatible with namespace resource type")
	}

	// Note: technically, this allows you to say `stat ns --namespace default`, but that
	// seems like an edge case.
	if o.namespace != "default" {
		return fmt.Errorf("--namespace flag is incompatible with namespace resource type")
	}

	return nil
}

func (o *routesOptions) validateOutputFormat() error {
	switch o.outputFormat {
	case "table", "json", "":
		return nil
	default:
		return fmt.Errorf("--output currently only supports table and json")
	}
}
