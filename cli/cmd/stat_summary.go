package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/runconduit/conduit/controller/api/util"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/spf13/cobra"
)

var namespace, resourceType, resourceName string
var outToNamespace, outToType, outToName string
var outFromNamespace, outFromType, outFromName string

var statSummaryCommand = &cobra.Command{
	Use:   "statsummary [flags] deployment [RESOURCE]",
	Short: "Display traffic stats about one or many resources",
	Long: `Display traffic stats about one or many resources.

Valid resource types include:

	* deployment

This command will hide resources that have completed, such as pods that are in the Succeeded or Failed phases.`,
	Example: `  conduit statsummary deployments hello1 -a test `,
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
	addControlPlaneNetworkingArgs(statSummaryCommand)
	// TODO: the -n flag is taken up by conduit-namespace :( we should move it to something else so this can have -n
	statSummaryCommand.PersistentFlags().StringVarP(&namespace, "namespace", "a", "default", "namespace of the specified resource")
	statSummaryCommand.PersistentFlags().StringVarP(&timeWindow, "time-window", "t", "1m", "Stat window.  One of: '10s', '1m', '10m', '1h'.")

	statSummaryCommand.PersistentFlags().StringVarP(&outToName, "out-to", "", "", "If present, restricts outbound stats to the specified resource name")
	statSummaryCommand.PersistentFlags().StringVarP(&outToNamespace, "out-to-namespace", "", "", "Sets the namespace used to lookup the '--out-to' resource. By default the current '--namespace' is used")
	statSummaryCommand.PersistentFlags().StringVarP(&outToType, "out-to-resource", "", "", "If present, restricts outbound stats to the specified resource type")

	statSummaryCommand.PersistentFlags().StringVarP(&outFromName, "out-from", "", "", "If present, restricts outbound stats to the specified resource name")
	statSummaryCommand.PersistentFlags().StringVarP(&outFromNamespace, "out-from-namespace", "", "", "Sets the namespace used to lookup the '--out-to' resource. By default the current '--namespace' is used")
	statSummaryCommand.PersistentFlags().StringVarP(&outFromType, "out-from-resource", "", "", "If present, restricts outbound stats to the specified resource type")
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

	return renderStatSummary(resp)
}

func renderStatSummary(resp *pb.StatSummaryResponse) (string, error) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', tabwriter.AlignRight)

	writeStatTableToBuffer(resp, w)
	w.Flush()

	// strip left padding on the first column
	out := string(buffer.Bytes()[padding:])
	out = strings.Replace(out, "\n"+strings.Repeat(" ", padding), "\n", -1)

	return out, nil
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
			name := r.Spec.Name
			if name == "" {
				continue
			}

			if len(name) > maxNameLength {
				maxNameLength = len(name)
			}

			if _, ok := stats[name]; !ok {
				stats[name] = &summaryRow{}
			}

			stats[name].meshed = strconv.FormatUint(r.MeshedPodCount, 10) + "/" + strconv.FormatUint(r.TotalPodCount, 10)
			stats[name].requestRate = getRequestRate(*r)
			stats[name].successRate = getSuccessRate(*r)
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
	window, err := util.GetWindow(timeWindow)
	if err != nil {
		return nil, err
	}

	request := pb.StatSummaryRequest{
		Resource: &pb.ResourceSelection{
			Spec: &pb.Resource{
				Namespace: namespace,
				Type:      resourceType,
				Name:      resourceName,
			},
		},
		TimeWindow: window,
	}

	if outToName != "" || outToType != "" || outToNamespace != "" {
		if outToNamespace == "" {
			outToNamespace = namespace
		}

		outToResource := pb.StatSummaryRequest_OutToResource{
			OutToResource: &pb.Resource{
				Namespace: outToNamespace,
				Type:      outToType,
				Name:      outToName,
			},
		}
		request.Outbound = &outToResource
	}
	if outFromName != "" || outFromType != "" || outFromNamespace != "" {
		if outFromNamespace == "" {
			outFromNamespace = namespace
		}

		outFromResource := pb.StatSummaryRequest_OutFromResource{
			OutFromResource: &pb.Resource{
				Namespace: outFromNamespace,
				Type:      outFromType,
				Name:      outFromName,
			},
		}
		request.Outbound = &outFromResource
	}

	return &request, nil
}

func getRequestRate(r pb.StatTable_PodGroup_Row) float64 {
	success := r.Stats.SuccessCount
	failure := r.Stats.FailureCount
	window, err := util.GetWindowString(r.TimeWindow)
	if err != nil {
		fmt.Println(err.Error())
	}

	windowLength, err := time.ParseDuration(window)
	if err != nil {
		fmt.Println(err.Error())
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
