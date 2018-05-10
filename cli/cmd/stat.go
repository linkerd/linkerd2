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

var (
	timeWindow                  string
	namespace                   string
	toNamespace, toResource     string
	fromNamespace, fromResource string
	allNamespaces               bool
)

var statCmd = &cobra.Command{
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

Valid resource types include:

  * deployments
  * namespaces
  * pods
  * replicationcontrollers
  * services (only supported if a "--from" is also specified, or as a "--to")

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

		req, err := buildStatSummaryRequest(
			timeWindow, allNamespaces,
			args, namespace,
			toResource, toNamespace,
			fromResource, fromNamespace,
		)
		if err != nil {
			return fmt.Errorf("error creating metrics request while making stats request: %v", err)
		}

		output, err := requestStatsFromAPI(client, req)
		if err != nil {
			return err
		}

		_, err = fmt.Print(output)

		return err
	},
}

func init() {
	RootCmd.AddCommand(statCmd)
	statCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default", "Namespace of the specified resource")
	statCmd.PersistentFlags().StringVarP(&timeWindow, "time-window", "t", "1m", "Stat window (for example: \"10s\", \"1m\", \"10m\", \"1h\")")
	statCmd.PersistentFlags().StringVar(&toResource, "to", "", "If present, restricts outbound stats to the specified resource name")
	statCmd.PersistentFlags().StringVar(&toNamespace, "to-namespace", "", "Sets the namespace used to lookup the \"--to\" resource; by default the current \"--namespace\" is used")
	statCmd.PersistentFlags().StringVar(&fromResource, "from", "", "If present, restricts outbound stats from the specified resource name")
	statCmd.PersistentFlags().StringVar(&fromNamespace, "from-namespace", "", "Sets the namespace used from lookup the \"--from\" resource; by default the current \"--namespace\" is used")
	statCmd.PersistentFlags().BoolVar(&allNamespaces, "all-namespaces", false, "If present, returns stats across all namespaces, ignoring the \"--namespace\" flag")
}

func requestStatsFromAPI(client pb.ApiClient, req *pb.StatSummaryRequest) (string, error) {
	resp, err := client.StatSummary(context.Background(), req)
	if err != nil {
		return "", fmt.Errorf("StatSummary API error: %v", err)
	}
	if e := resp.GetError(); e != nil {
		return "", fmt.Errorf("StatSummary API response error: %v", e.Error)
	}

	return renderStats(resp, req.Selector.Resource.Type), nil
}

func renderStats(resp *pb.StatSummaryResponse, resourceType string) string {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', tabwriter.AlignRight)
	writeStatsToBuffer(resp, resourceType, w)
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

func writeStatsToBuffer(resp *pb.StatSummaryResponse, reqResourceType string, w *tabwriter.Writer) {
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
					latencyP50:  r.Stats.LatencyMsP50,
					latencyP95:  r.Stats.LatencyMsP95,
					latencyP99:  r.Stats.LatencyMsP99,
				}
			}
		}
	}

	for resourceType, stats := range statTables {

		if reqResourceType == k8s.All {
			printStatTable(stats, resourceType, w, maxNameLength, maxNamespaceLength)
		} else {
			printStatTable(stats, "", w, maxNameLength, maxNamespaceLength)
		}
		fmt.Fprint(w, "\n")
	}
}

func printStatTable(stats map[string]*row, resourceType string, w *tabwriter.Writer, maxNameLength int, maxNamespaceLength int) {
	if len(stats) == 0 {
		fmt.Fprintln(os.Stderr, "No traffic found.")
		os.Exit(0)
	}

	headers := make([]string, 0)
	if allNamespaces {
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
		"LATENCY_P99\t", // trailing \t is required to format last column
	}...)

	fmt.Fprintln(w, strings.Join(headers, "\t"))

	namePrefix := getNamePrefix(resourceType)

	sortedKeys := sortStatsKeys(stats)
	for _, key := range sortedKeys {
		parts := strings.Split(key, "/")
		namespace := parts[0]
		name := namePrefix + parts[1]
		values := make([]interface{}, 0)
		templateString := "%s\t%s\t%.2f%%\t%.1frps\t%dms\t%dms\t%dms\t\n"
		templateStringEmpty := "%s\t%s\t-\t-\t-\t-\t-\t\n"

		if allNamespaces {
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

func buildStatSummaryRequest(
	timeWindow string, allNamespaces bool,
	resource []string, namespace string,
	toResource, toNamespace string,
	fromResource, fromNamespace string,
) (*pb.StatSummaryRequest, error) {
	targetNamespace := namespace
	if allNamespaces {
		targetNamespace = ""
	} else if namespace == "" {
		targetNamespace = v1.NamespaceDefault
	}

	target, err := util.BuildResource(targetNamespace, resource...)
	if err != nil {
		return nil, err
	}

	var toRes, fromRes pb.Resource
	if toResource != "" {
		toRes, err = util.BuildResource(toNamespace, toResource)
		if err != nil {
			return nil, err
		}
	}
	if fromResource != "" {
		fromRes, err = util.BuildResource(fromNamespace, fromResource)
		if err != nil {
			return nil, err
		}
	}

	requestParams := util.StatSummaryRequestParams{
		TimeWindow:    timeWindow,
		ResourceName:  target.Name,
		ResourceType:  target.Type,
		Namespace:     targetNamespace,
		ToName:        toRes.Name,
		ToType:        toRes.Type,
		ToNamespace:   toNamespace,
		FromName:      fromRes.Name,
		FromType:      fromRes.Type,
		FromNamespace: fromNamespace,
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

func sortStatsKeys(stats map[string]*row) []string {
	var sortedKeys []string
	for key := range stats {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)
	return sortedKeys
}
