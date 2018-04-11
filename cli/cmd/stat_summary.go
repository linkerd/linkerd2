package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/prometheus/common/log"
	"github.com/runconduit/conduit/controller/api/util"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
)

var namespace, resourceType, resourceName string
var outToNamespace, outToType, outToName string
var outFromNamespace, outFromType, outFromName string
var allNamespaces bool

var statSummaryCommand = &cobra.Command{
	Use:   "statsummary [flags] RESOURCETYPE [RESOURCENAME]",
	Short: "Display traffic stats about one or many resources",
	Long: `Display traffic stats about one or many resources.

Valid resource types include:

	* deployment

This command will hide resources that have completed, such as pods that are in the Succeeded or Failed phases.
If no resource name is specified, displays stats about all resources of the specified RESOURCETYPE`,
	Example: `  # Get all deployments in the test namespace.
  conduit statsummary deployments -n test

  # Get the hello1 deployment in the test namespace.
  conduit statsummary deployments hello1 -n test`,
	Args:      cobra.RangeArgs(1, 2),
	ValidArgs: []string{"deployment"},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch len(args) {
		case 1:
			resourceType = args[0]
		case 2:
			resourceType = args[0]
			resourceName = args[1]
		default:
			return errors.New("please specify one resource only")
		}

		client, err := newPublicAPIClient()
		if err != nil {
			return fmt.Errorf("error creating api client while making stats request: %v", err)
		}

		output, err := requestStatSummaryFromAPI(client)
		if err != nil {
			return err
		}

		_, err = fmt.Print(output)

		return err
	},
}

func init() {
	RootCmd.AddCommand(statSummaryCommand)
	statSummaryCommand.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default", "Namespace of the specified resource")
	statSummaryCommand.PersistentFlags().StringVarP(&timeWindow, "time-window", "t", "1m", "Stat window (one of: \"10s\", \"1m\", \"10m\", \"1h\")")
	statSummaryCommand.PersistentFlags().StringVar(&outToName, "out-to", "", "If present, restricts outbound stats to the specified resource name")
	statSummaryCommand.PersistentFlags().StringVar(&outToNamespace, "out-to-namespace", "", "Sets the namespace used to lookup the \"--out-to\" resource; by default the current \"--namespace\" is used")
	statSummaryCommand.PersistentFlags().StringVar(&outToType, "out-to-resource", "", "If present, restricts outbound stats to the specified resource type")
	statSummaryCommand.PersistentFlags().StringVar(&outFromName, "out-from", "", "If present, restricts outbound stats to the specified resource name")
	statSummaryCommand.PersistentFlags().StringVar(&outFromNamespace, "out-from-namespace", "", "Sets the namespace used to lookup the \"--out-from\" resource; by default the current \"--namespace\" is used")
	statSummaryCommand.PersistentFlags().StringVar(&outFromType, "out-from-resource", "", "If present, restricts outbound stats to the specified resource type")
	statSummaryCommand.PersistentFlags().BoolVar(&allNamespaces, "all-namespaces", false, "If present, returns stats across all namespaces, ignoring the \"--namespace\" flag")
}

func requestStatSummaryFromAPI(client pb.ApiClient) (string, error) {
	req, err := buildStatSummaryRequest()

	if err != nil {
		return "", fmt.Errorf("error creating metrics request while making stats request: %v", err)
	}

	resp, err := client.StatSummary(context.Background(), req)
	if err != nil {
		return "", fmt.Errorf("error calling stat with request: %v", err)
	}

	return renderStatSummary(resp), nil
}

func renderStatSummary(resp *pb.StatSummaryResponse) string {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', tabwriter.AlignRight)

	writeStatTableToBuffer(resp, w)
	w.Flush()

	// strip left padding on the first column
	out := string(buffer.Bytes()[padding:])
	out = strings.Replace(out, "\n"+strings.Repeat(" ", padding), "\n", -1)

	return out
}

type summaryRow struct {
	meshed      string
	requestRate float64
	successRate float64
	latencyP50  uint64
	latencyP95  uint64
	latencyP99  uint64
}

func writeStatTableToBuffer(resp *pb.StatSummaryResponse, w *tabwriter.Writer) {
	nameHeader := "NAME"
	maxNameLength := len(nameHeader)
	namespaceHeader := "NAMESPACE"
	maxNamespaceLength := len(namespaceHeader)

	stats := make(map[string]*summaryRow)

	for _, statTable := range resp.GetOk().StatTables {
		table := statTable.GetPodGroup()
		for _, r := range table.Rows {
			name := r.Resource.Name
			namespace := r.Resource.Namespace
			key := fmt.Sprintf("%s/%s", namespace, name)

			if len(name) > maxNameLength {
				maxNameLength = len(name)
			}

			if len(namespace) > maxNamespaceLength {
				maxNamespaceLength = len(namespace)
			}

			stats[key] = &summaryRow{
				meshed: fmt.Sprintf("%d/%d", r.MeshedPodCount, r.TotalPodCount),
			}

			if r.Stats != nil {
				stats[key].requestRate = getRequestRate(*r)
				stats[key].successRate = getSuccessRate(*r)
				stats[key].latencyP50 = r.Stats.LatencyMsP50
				stats[key].latencyP95 = r.Stats.LatencyMsP95
				stats[key].latencyP99 = r.Stats.LatencyMsP99
			}
		}
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

	sortedKeys := sortStatSummaryKeys(stats)
	for _, key := range sortedKeys {
		parts := strings.Split(key, "/")
		namespace := parts[0]
		name := parts[1]
		values := make([]interface{}, 0)
		templateString := "%s\t%s\t%.2f%%\t%.1frps\t%dms\t%dms\t%dms\t\n"

		if allNamespaces {
			values = append(values,
				namespace+strings.Repeat(" ", maxNamespaceLength-len(namespace)))
			templateString = "%s\t" + templateString
		}
		values = append(values, []interface{}{
			name + strings.Repeat(" ", maxNameLength-len(name)),
			stats[key].meshed,
			stats[key].successRate * 100,
			stats[key].requestRate,
			stats[key].latencyP50,
			stats[key].latencyP95,
			stats[key].latencyP99,
		}...)

		fmt.Fprintf(w, templateString, values...)
	}
}

func buildStatSummaryRequest() (*pb.StatSummaryRequest, error) {
	targetNamespace := namespace
	if allNamespaces {
		targetNamespace = ""
	} else if namespace == "" {
		targetNamespace = v1.NamespaceDefault
	}

	requestParams := util.StatSummaryRequestParams{
		TimeWindow:       timeWindow,
		ResourceName:     resourceName,
		ResourceType:     resourceType,
		Namespace:        targetNamespace,
		OutToName:        outToName,
		OutToType:        outToType,
		OutToNamespace:   outToNamespace,
		OutFromName:      outFromName,
		OutFromType:      outFromType,
		OutFromNamespace: outFromNamespace,
	}

	return util.BuildStatSummaryRequest(requestParams)
}

func getRequestRate(r pb.StatTable_PodGroup_Row) float64 {
	success := r.Stats.SuccessCount
	failure := r.Stats.FailureCount
	window, err := util.GetWindowString(r.TimeWindow)
	if err != nil {
		log.Error(err.Error())
		return 0.0
	}

	windowLength, err := time.ParseDuration(window)
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

func sortStatSummaryKeys(stats map[string]*summaryRow) []string {
	var sortedKeys []string
	for key := range stats {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)
	return sortedKeys
}
