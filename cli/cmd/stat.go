package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/runconduit/conduit/cli/k8s"
	"github.com/runconduit/conduit/cli/shell"

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
 * paths (aka path, pa)

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
			return errors.New("please specify a resource type: pods, deployments or paths")
		}

		switch resourceType {
		case "pods", "pod", "po":
			return makeStatsRequest(pb.AggregationType_TARGET_POD)
		case "deployments", "deployment", "deploy":
			return makeStatsRequest(pb.AggregationType_TARGET_DEPLOY)
		case "paths", "path", "pa":
			return makeStatsRequest(pb.AggregationType_PATH)
		default:
			return errors.New("invalid resource type")
		}

		return nil
	},
}

func makeStatsRequest(aggType pb.AggregationType) error {
	kubeApi, err := k8s.MakeK8sAPi(shell.MakeUnixShell(), kubeconfigPath, apiAddr)
	if err != nil {
		return err
	}

	client, err := newApiClient(kubeApi)
	if err != nil {
		return fmt.Errorf("error creating api client while making stats request: %v", err)
	}
	req, err := buildMetricRequest(aggType)
	if err != nil {
		return fmt.Errorf("error creating metrics request while making stats request: %v", err)
	}

	resp, err := client.Stat(context.Background(), req)
	if err != nil {
		return fmt.Errorf("error calling stat with request: %v", err)
	}

	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', tabwriter.AlignRight)
	displayStats(resp, w)
	w.Flush()

	// strip left padding on the first column
	out := string(buffer.Bytes()[padding:])
	out = strings.Replace(out, "\n"+strings.Repeat(" ", padding), "\n", -1)

	_, err = fmt.Print(out)
	return err
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
	nameHeader := "NAME"
	maxNameLength := len(nameHeader)

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
		} else if metadata.Path != "" {
			name = metadata.Path
		}

		if len(name) > maxNameLength {
			maxNameLength = len(name)
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

	fmt.Fprintln(w, strings.Join([]string{
		nameHeader + strings.Repeat(" ", maxNameLength-len(nameHeader)),
		"REQUEST_RATE",
		"SUCCESS_RATE",
		"P50_LATENCY",
		"P99_LATENCY\t", // trailing \t is required to format last column
	}, "\t"))

	sortedNames := sortStatsKeys(stats)
	for _, name := range sortedNames {
		fmt.Fprintf(
			w,
			"%s\t%.1frps\t%.2f%%\t%dms\t%dms\t\n",
			name+strings.Repeat(" ", maxNameLength-len(name)),
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
	if target != "all" && aggregationType == pb.AggregationType_PATH {
		filterBy.Path = target
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
