package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/runconduit/conduit/controller/api/util"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
)

type statOptions struct {
	namespace     string
	timeWindow    string
	toNamespace   string
	toResource    string
	fromNamespace string
	fromResource  string
	allNamespaces bool
}

func newStatOptions() *statOptions {
	return &statOptions{
		namespace:     "default",
		timeWindow:    "1m",
		toNamespace:   "",
		toResource:    "",
		fromNamespace: "",
		fromResource:  "",
		allNamespaces: false,
	}
}

func newCmdStat() *cobra.Command {
	options := newStatOptions()

	cmd := &cobra.Command{
		Use:   "stat [flags] (RESOURCE)",
		Short: "Display traffic stats about one or many resources",
		Long: `Display traffic stats about one or many resources.

  The RESOURCE argument specifies the target resource(s) to aggregate stats over:
  (TYPE [NAME] | TYPE/NAME)

  Examples:
  * deploy
  * deploy/my-deploy
  * deploy my-deploy
  * ns/my-ns
  * all

Valid resource types include:

  * deployments
  * namespaces
  * pods
  * replicationcontrollers
  * services (only supported if a "--from" is also specified, or as a "--to")
  * all (all resource types, not supported in --from or --to)

This command will hide resources that have completed, such as pods that are in the Succeeded or Failed phases.
If no resource name is specified, displays stats about all resources of the specified RESOURCETYPE`,
		Example: `  # Get all deployments in the test namespace.
  conduit stat deployments -n test

  # Get the hello1 replication controller in the test namespace.
  conduit stat replicationcontrollers hello1 -n test

  # Get all namespaces.
  conduit stat namespaces

  # Get all inbound stats to the web deployment.
  conduit stat deploy/web

  # Get all pods in all namespaces that call the hello1 deployment in the test namesapce.
  conduit stat pods --to deploy/hello1 --to-namespace test --all-namespaces

  # Get all pods in all namespaces that call the hello1 service in the test namesapce.
  conduit stat pods --to svc/hello1 --to-namespace test --all-namespaces

  # Get all services in all namespaces that receive calls from hello1 deployment in the test namesapce.
  conduit stat services --from deploy/hello1 --from-namespace test --all-namespaces`,
		Args:      cobra.RangeArgs(1, 2),
		ValidArgs: util.ValidTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newPublicAPIClient()
			if err != nil {
				return fmt.Errorf("error creating api client while making stats request: %v", err)
			}

			req, err := buildStatSummaryRequest(args, options)
			if err != nil {
				return fmt.Errorf("error creating metrics request while making stats request: %v", err)
			}

			output, err := requestStatsFromAPI(client, req, options)
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

	return cmd
}

func requestStatsFromAPI(client pb.ApiClient, req *pb.StatSummaryRequest, options *statOptions) (string, error) {
	resp, err := client.StatSummary(context.Background(), req)
	if err != nil {
		return "", fmt.Errorf("StatSummary API error: %v", err)
	}
	if e := resp.GetError(); e != nil {
		return "", fmt.Errorf("StatSummary API response error: %v", e.Error)
	}

	return renderStats(resp, req.Selector.Resource.Type, options), nil
}

func renderStats(resp *pb.StatSummaryResponse, resourceType string, options *statOptions) string {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', tabwriter.AlignRight)
	writeStatsToBuffer(resp, resourceType, w, options)
	w.Flush()

	// strip left padding on the first column
	out := string(buffer.Bytes()[padding:])
	out = strings.Replace(out, "\n"+strings.Repeat(" ", padding), "\n", -1)

	return out
}

const padding = 3

type rowStats struct {
	requestRate float64
	successRate float64
	secured     float64
	latencyP50  uint64
	latencyP95  uint64
	latencyP99  uint64
}

type row struct {
	meshed string
	*rowStats
}

var (
	nameHeader      = "NAME"
	namespaceHeader = "NAMESPACE"
)

func writeStatsToBuffer(resp *pb.StatSummaryResponse, reqResourceType string, w *tabwriter.Writer, options *statOptions) {
	maxNameLength := len(nameHeader)
	maxNamespaceLength := len(namespaceHeader)
	statTables := make(map[string]map[string]*row)

	for _, statTable := range resp.GetOk().StatTables {
		table := statTable.GetPodGroup()

		for _, r := range table.Rows {
			name := r.Resource.Name
			nameWithPrefix := name
			if reqResourceType == k8s.All {
				nameWithPrefix = getNamePrefix(r.Resource.Type) + nameWithPrefix
			}

			namespace := r.Resource.Namespace
			key := fmt.Sprintf("%s/%s", namespace, name)
			resourceKey := r.Resource.Type

			if _, ok := statTables[resourceKey]; !ok {
				statTables[resourceKey] = make(map[string]*row)
			}

			if len(nameWithPrefix) > maxNameLength {
				maxNameLength = len(nameWithPrefix)
			}

			if len(namespace) > maxNamespaceLength {
				maxNamespaceLength = len(namespace)
			}

			statTables[resourceKey][key] = &row{
				meshed: fmt.Sprintf("%d/%d", r.MeshedPodCount, r.RunningPodCount),
			}

			if r.Stats != nil {
				statTables[resourceKey][key].rowStats = &rowStats{
					requestRate: getRequestRate(*r),
					successRate: getSuccessRate(*r),
					secured:     getPercentSecured(*r),
					latencyP50:  r.Stats.LatencyMsP50,
					latencyP95:  r.Stats.LatencyMsP95,
					latencyP99:  r.Stats.LatencyMsP99,
				}
			}
		}
	}

	if len(statTables) == 0 {
		fmt.Fprintln(os.Stderr, "No traffic found.")
		os.Exit(0)
	}

	lastDisplayedStat := true // don't print a newline after the final stat
	for resourceType, stats := range statTables {
		if !lastDisplayedStat {
			fmt.Fprint(w, "\n")
		}
		lastDisplayedStat = false
		if reqResourceType == k8s.All {
			printStatTable(stats, resourceType, w, maxNameLength, maxNamespaceLength, options)
		} else {
			printStatTable(stats, "", w, maxNameLength, maxNamespaceLength, options)
		}
	}
}

func printStatTable(stats map[string]*row, resourceType string, w *tabwriter.Writer, maxNameLength int, maxNamespaceLength int, options *statOptions) {
	headers := make([]string, 0)
	if options.allNamespaces {
		headers = append(headers,
			namespaceHeader+strings.Repeat(" ", maxNamespaceLength-len(namespaceHeader)))
	}
	headers = append(headers, []string{
		nameHeader + strings.Repeat(" ", maxNameLength-len(nameHeader)),
		"MESHED",
		"SUCCESS",
		"RPS",
		"LATENCY_P50",
		"LATENCY_P95",
		"LATENCY_P99",
		"SECURED\t", // trailing \t is required to format last column
	}...)

	fmt.Fprintln(w, strings.Join(headers, "\t"))

	namePrefix := getNamePrefix(resourceType)

	sortedKeys := sortStatsKeys(stats)
	for _, key := range sortedKeys {
		parts := strings.Split(key, "/")
		namespace := parts[0]
		name := namePrefix + parts[1]
		values := make([]interface{}, 0)
		templateString := "%s\t%s\t%.2f%%\t%.1frps\t%dms\t%dms\t%dms\t%.f%%\t\n"
		templateStringEmpty := "%s\t%s\t-\t-\t-\t-\t-\t-\t\n"

		if options.allNamespaces {
			values = append(values,
				namespace+strings.Repeat(" ", maxNamespaceLength-len(namespace)))
			templateString = "%s\t" + templateString
			templateStringEmpty = "%s\t" + templateStringEmpty
		}
		values = append(values, []interface{}{
			name + strings.Repeat(" ", maxNameLength-len(name)),
			stats[key].meshed,
		}...)

		if stats[key].rowStats != nil {
			values = append(values, []interface{}{
				stats[key].successRate * 100,
				stats[key].requestRate,
				stats[key].latencyP50,
				stats[key].latencyP95,
				stats[key].latencyP99,
				stats[key].secured * 100,
			}...)

			fmt.Fprintf(w, templateString, values...)
		} else {
			fmt.Fprintf(w, templateStringEmpty, values...)
		}
	}
}

func getNamePrefix(resourceType string) string {
	if resourceType == "" {
		return ""
	} else {
		return k8s.ShortNameFromCanonicalKubernetesName(resourceType) + "/"
	}
}

func buildStatSummaryRequest(resource []string, options *statOptions) (*pb.StatSummaryRequest, error) {
	targetNamespace := options.namespace
	if options.allNamespaces {
		targetNamespace = ""
	} else if options.namespace == "" {
		targetNamespace = v1.NamespaceDefault
	}

	target, err := util.BuildResource(targetNamespace, resource...)
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
		Namespace:     targetNamespace,
		ToName:        toRes.Name,
		ToType:        toRes.Type,
		ToNamespace:   options.toNamespace,
		FromName:      fromRes.Name,
		FromType:      fromRes.Type,
		FromNamespace: options.fromNamespace,
	}

	return util.BuildStatSummaryRequest(requestParams)
}

func getRequestRate(r pb.StatTable_PodGroup_Row) float64 {
	success := r.Stats.SuccessCount
	failure := r.Stats.FailureCount
	windowLength, err := time.ParseDuration(r.TimeWindow)
	if err != nil {
		log.Error(err.Error())
		return 0.0
	}
	return float64(success+failure) / windowLength.Seconds()
}

func getSuccessRate(r pb.StatTable_PodGroup_Row) float64 {
	success := r.Stats.SuccessCount
	failure := r.Stats.FailureCount

	if success+failure == 0 {
		return 0.0
	}
	return float64(success) / float64(success+failure)
}

func getPercentSecured(r pb.StatTable_PodGroup_Row) float64 {
	reqTotal := r.Stats.SuccessCount + r.Stats.FailureCount
	if reqTotal == 0 {
		return 0.0
	}
	return float64(r.Stats.TlsRequestCount) / float64(reqTotal)
}

func sortStatsKeys(stats map[string]*row) []string {
	var sortedKeys []string
	for key := range stats {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)
	return sortedKeys
}
