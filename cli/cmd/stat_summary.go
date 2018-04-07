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
)

var namespace, resourceType, resourceName string
var outToNamespace, outToType, outToName string
var outFromNamespace, outFromType, outFromName string

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
	latencyP50  int64
	latencyP99  int64
}

func writeStatTableToBuffer(resp *pb.StatSummaryResponse, w *tabwriter.Writer) {
	nameHeader := "NAME"
	maxNameLength := len(nameHeader)

	stats := make(map[string]*summaryRow)

	for _, statTable := range resp.GetOk().StatTables {
		table := statTable.GetPodGroup()
		for _, r := range table.Rows {
			name := r.Resource.Name
			if name == "" {
				continue
			}

			if len(name) > maxNameLength {
				maxNameLength = len(name)
			}

			stats[name] = &summaryRow{
				meshed:      fmt.Sprintf("%d/%d", r.MeshedPodCount, r.TotalPodCount),
				requestRate: getRequestRate(*r),
				successRate: getSuccessRate(*r),
			}
		}
	}

	fmt.Fprintln(w, strings.Join([]string{
		nameHeader + strings.Repeat(" ", maxNameLength-len(nameHeader)),
		"MESHED",
		"IN_RPS",
		"IN_SUCCESS",
		"IN_LATENCY_P50",
		"IN_LATENCY_P99\t", // trailing \t is required to format last column
	}, "\t"))

	sortedNames := sortStatSummaryKeys(stats)
	for _, name := range sortedNames {
		fmt.Fprintf(
			w,
			"%s\t%s\t%.1frps\t%.2f%%\t%dms\t%dms\t\n",
			name+strings.Repeat(" ", maxNameLength-len(name)),
			stats[name].meshed,
			stats[name].requestRate,
			stats[name].successRate*100,
			stats[name].latencyP50,
			stats[name].latencyP99,
		)
	}
}

func buildStatSummaryRequest() (*pb.StatSummaryRequest, error) {
	requestParams := util.StatSummaryRequestParams{
		TimeWindow:       timeWindow,
		ResourceName:     resourceName,
		ResourceType:     resourceType,
		Namespace:        namespace,
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
	if r.Stats == nil {
		return 0.0
	}
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
	if r.Stats == nil {
		return 0.0
	}

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
