package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/runconduit/conduit/controller/api/util"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	"github.com/spf13/cobra"
)

const ConduitPaths = "paths"

var target string
var timeWindow string

var statCmd = &cobra.Command{
	Use:   "stat [flags] deployment [TARGET]",
	Short: "Display runtime statistics about mesh resources",
	Long: `Display runtime statistics about mesh resources.

Only deployment resources (aka deployments, deploy) are supported.

The optional [TARGET] argument can be used to target a specific deployment.`,
	Example: `  # get stats for all deployments
  conduit stat deployments

  # get stats for the web deployment in the default namespace
  conduit stat deploy default/web`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var friendlyNameForResourceType string

		switch len(args) {
		case 1:
			friendlyNameForResourceType = args[0]
		case 2:
			friendlyNameForResourceType = args[0]
			target = args[1]
		default:
			return errors.New("please specify a resource type")
		}

		validatedResourceType, err := k8s.CanonicalKubernetesNameFromFriendlyName(friendlyNameForResourceType)
		if err != nil {
			return fmt.Errorf("invalid resource type %s, only %v are allowed as resource types", friendlyNameForResourceType, []string{k8s.KubernetesDeployments})
		} else {
			switch friendlyNameForResourceType {
			case "pods", "pod", "po", "paths", "path", "pa":
				return fmt.Errorf("invalid resource type %s, only %v are allowed as resource types", friendlyNameForResourceType, []string{k8s.KubernetesDeployments})
			default:
			}
		}
		client, err := newPublicAPIClient()
		if err != nil {
			return fmt.Errorf("error creating api client while making stats request: %v", err)
		}

		output, err := requestStatsFromApi(client, validatedResourceType)
		if err != nil {
			return err
		}

		_, err = fmt.Print(output)

		return err
	},
}

func init() {
	RootCmd.AddCommand(statCmd)
	statCmd.PersistentFlags().StringVarP(&timeWindow, "time-window", "t", "1m", "Stat window (one of: \"10s\", \"1m\", \"10m\", \"1h\")")
}

var resourceTypeToAggregationType = map[string]pb.AggregationType{
	k8s.KubernetesDeployments: pb.AggregationType_TARGET_DEPLOY,
}

func requestStatsFromApi(client pb.ApiClient, resourceType string) (string, error) {
	aggType := resourceTypeToAggregationType[resourceType]
	req, err := buildMetricRequest(aggType)
	if err != nil {
		return "", fmt.Errorf("error creating metrics request while making stats request: %v", err)
	}

	resp, err := client.Stat(context.Background(), req)
	if err != nil {
		return "", fmt.Errorf("error calling stat with request: %v", err)
	}

	return renderStats(resp)
}

func renderStats(resp *pb.MetricResponse) (string, error) {
	var buffer bytes.Buffer
	w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', tabwriter.AlignRight)
	writeStatsToBuffer(resp, w)
	w.Flush()

	// strip left padding on the first column
	out := string(buffer.Bytes()[padding:])
	out = strings.Replace(out, "\n"+strings.Repeat(" ", padding), "\n", -1)

	return out, nil
}

const padding = 3

type row struct {
	requestRate float64
	successRate float64
	latencyP50  int64
	latencyP99  int64
}

func writeStatsToBuffer(resp *pb.MetricResponse, w *tabwriter.Writer) {
	nameHeader := "NAME"
	maxNameLength := len(nameHeader)

	stats := make(map[string]*row)
	for _, metric := range resp.Metrics {
		if len(metric.Datapoints) == 0 {
			continue
		}

		metadata := *metric.Metadata
		var name string
		if metadata.TargetDeploy != "" {
			name = metadata.TargetDeploy
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

func sortStatsKeys(stats map[string]*row) []string {
	var sortedKeys []string
	for key, _ := range stats {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)
	return sortedKeys
}
