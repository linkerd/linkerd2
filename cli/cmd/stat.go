package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/runconduit/conduit/controller/api/util"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/spf13/cobra"
)

const padding = 3

type row struct {
	requestRate float64
	successRate float64
	latencyP50  int64
	latencyP99  int64
}

var target string
var timeWindow string
var watch bool
var watchOnly bool

var statCmd = &cobra.Command{
	Use:   "stat [flags] RESOURCE [TARGET]",
	Short: "Display runtime statistics about mesh resources",
	Long: `Display runtime statistics about mesh resources.

Valid resource types include:
 * pods (aka pod, po)
 * deployments (aka deployment, deploy)

The optional [TARGET] option can be either a name for a deployment or pod resource`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var resourceType string
		switch len(args) {
		case 1:
			resourceType = args[0]
		case 2:
			resourceType = args[0]
			target = args[1]
		default:
			return errors.New("please specify a resource type: pods or deployments")
		}

		switch resourceType {
		case "pods", "pod", "po":
			return makeStatsRequest(pb.AggregationType_TARGET_POD)
		case "deployments", "deployment", "deploy":
			return makeStatsRequest(pb.AggregationType_TARGET_DEPLOY)
		default:
			return errors.New("invalid resource type")
		}

		return nil
	},
}

func makeStatsRequest(aggType pb.AggregationType) error {
	client, err := newApiClient()
	if err != nil {
		return err
	}
	req, err := buildMetricRequest(aggType)
	if err != nil {
		return err
	}

	resp, err := client.Stat(context.Background(), req)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, padding, ' ', 0)
	displayStats(resp, w)

	w.Flush()
	return nil
}

func sortStatsKeys(stats map[string]*row) []string {
	var sortedKeys []string
	for key, _ := range stats {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)
	return sortedKeys
}

func displayStats(resp *pb.MetricResponse, w *tabwriter.Writer) {
	fmt.Fprintln(w, strings.Join([]string{
		"NAME",
		"REQUEST_RATE",
		"SUCCESS_RATE",
		"P50_LATENCY",
		"P99_LATENCY",
	}, "\t"))

	stats := make(map[string]*row)
	for _, metric := range resp.Metrics {
		if len(metric.Datapoints) == 0 {
			continue
		}

		metadata := *metric.Metadata
		var name string
		if metadata.TargetPod != "" {
			name = metadata.TargetPod
		} else if metadata.TargetDeploy != "" {
			name = metadata.TargetDeploy
		}

		if _, ok := stats[name]; !ok {
			stats[name] = &row{}
		}

		switch metric.Name {
		case pb.MetricName_REQUEST_RATE:
			stats[name].requestRate = metric.Datapoints[0].Value.GetGauge()
		case pb.MetricName_SUCCESS_RATE:
			stats[name].successRate = metric.Datapoints[0].Value.GetGauge()
		case pb.MetricName_LATENCY:
			for _, v := range metric.Datapoints[0].Value.GetHistogram().Values {
				switch v.Label {
				case pb.HistogramLabel_P50:
					stats[name].latencyP50 = v.Value
				case pb.HistogramLabel_P99:
					stats[name].latencyP99 = v.Value
				}
			}
		}
	}

	sortedNames := sortStatsKeys(stats)
	for _, name := range sortedNames {
		fmt.Fprintf(
			w,
			"%s\t%frps\t%f%%\t%dms\t%dms\n",
			name,
			stats[name].requestRate,
			stats[name].successRate*100,
			stats[name].latencyP50,
			stats[name].latencyP99,
		)
	}
}

func buildMetricRequest(aggregationType pb.AggregationType) (*pb.MetricRequest, error) {
	var filterBy pb.MetricMetadata
	window, err := util.GetWindow(timeWindow)
	if err != nil {
		return nil, err
	}

	if target != "all" && aggregationType == pb.AggregationType_TARGET_POD {
		filterBy.TargetPod = target
	}
	if target != "all" && aggregationType == pb.AggregationType_TARGET_DEPLOY {
		filterBy.TargetDeploy = target
	}

	return &pb.MetricRequest{
		Metrics: []pb.MetricName{
			pb.MetricName_REQUEST_RATE,
			pb.MetricName_SUCCESS_RATE,
			pb.MetricName_LATENCY,
		},
		Window:    window,
		FilterBy:  &filterBy,
		GroupBy:   aggregationType,
		Summarize: true,
	}, nil
}

func init() {
	RootCmd.AddCommand(statCmd)
	addControlPlaneNetworkingArgs(statCmd)
	statCmd.PersistentFlags().StringVarP(&timeWindow, "time-window", "t", "1m", "Stat window.  One of: '10s', '1m', '10m', '1h', '6h', '24h'.")
	statCmd.PersistentFlags().BoolVarP(&watch, "watch", "w", false, "After listing/getting the requested object, watch for changes.")
	statCmd.PersistentFlags().BoolVar(&watchOnly, "watch-only", false, "Watch for changes to the requested object(s), without listing/getting first.")
}
