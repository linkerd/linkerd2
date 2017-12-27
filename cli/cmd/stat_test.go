package cmd

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"strings"
	"testing"

	"github.com/runconduit/conduit/cli/k8s"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/stretchr/testify/assert"
)

func TestRequestStatsFromApi(t *testing.T) {
	t.Run("Returns string output containing the data returned by the API", func(t *testing.T) {
		mockClient := &mockApiClient{}

		podName := "pod-1"
		metricDatapoints := []*pb.MetricDatapoint{
			{
				Value: &pb.MetricValue{
					Value: &pb.MetricValue_Counter{
						Counter: 666,
					},
				},
			},
		}
		series := []*pb.MetricSeries{
			{
				Name: pb.MetricName_SUCCESS_RATE,
				Metadata: &pb.MetricMetadata{
					TargetPod: podName,
				},
				Datapoints: metricDatapoints,
			},
		}
		mockClient.metricResponseToReturn = &pb.MetricResponse{
			Metrics: series,
		}

		stats, err := requestStatsFromApi(mockClient, k8s.KubernetesPods)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !strings.Contains(stats, podName) {
			t.Fatalf("Expected response to contain [%s], but was [%s]", podName, stats)
		}
	})

	t.Run("Returns error if API call failed", func(t *testing.T) {
		mockClient := &mockApiClient{}
		mockClient.errorToReturn = errors.New("Expected")
		output, err := requestStatsFromApi(mockClient, k8s.KubernetesPods)

		if err == nil {
			t.Fatalf("Expected error, got nothing but the output [%s]", output)
		}
	})
}

func TestRenderStats(t *testing.T) {
	t.Run("Prints stats correctly for example with one entry", func(t *testing.T) {
		allSeries := make([]*pb.MetricSeries, 0)
		seriesForPodX := generateMetricSeriesFor(fmt.Sprintf("deployment-%d", 66), int64(10))
		allSeries = append(allSeries, seriesForPodX...)

		//shuffles
		for i := range allSeries {
			j := rand.Intn(i + 1)
			allSeries[i], allSeries[j] = allSeries[j], allSeries[i]
		}

		response := &pb.MetricResponse{
			Metrics: allSeries,
		}

		renderedStats, err := renderStats(response)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		goldenFileBytes, err := ioutil.ReadFile("testdata/stat_one_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedContent := string(goldenFileBytes)

		if expectedContent != renderedStats {
			t.Fatalf("Expected function to render:\n%s\bbut got:\n%s", expectedContent, renderedStats)
		}
	})

	t.Run("Prints stats correctly for busy example", func(t *testing.T) {
		allSeries := make([]*pb.MetricSeries, 0)
		for i := 0; i < 10; i++ {
			seriesForPodX := generateMetricSeriesFor(fmt.Sprintf("pod-%d", i), int64(i))
			allSeries = append(allSeries, seriesForPodX...)
		}

		//shuffles
		for i := range allSeries {
			j := rand.Intn(i + 1)
			allSeries[i], allSeries[j] = allSeries[j], allSeries[i]
		}

		response := &pb.MetricResponse{
			Metrics: allSeries,
		}

		renderedStats, err := renderStats(response)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		goldenFileBytes, err := ioutil.ReadFile("testdata/stat_busy_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedContent := string(goldenFileBytes)

		if expectedContent != renderedStats {
			t.Fatalf("Expected function to render:\n%s\bbut got:\n%s", expectedContent, renderedStats)
		}
	})

	t.Run("Prints stats correctly for empty example", func(t *testing.T) {
		response := &pb.MetricResponse{
			Metrics: make([]*pb.MetricSeries, 0),
		}

		renderedStats, err := renderStats(response)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		goldenFileBytes, err := ioutil.ReadFile("testdata/stat_empty_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedContent := string(goldenFileBytes)

		if expectedContent != renderedStats {
			t.Fatalf("Expected function to render:\n%s\bbut got:\n%s", expectedContent, renderedStats)
		}
	})
}

func TestSortStatsKeys(t *testing.T) {
	t.Run("Sorts the keys alphabetically", func(t *testing.T) {
		unsorted := map[string]*row{
			"kube-system/heapster-v1.4.3":      {0.008091, 24.137931, 516666, 990333},
			"test/backend4":                    {0.066121, 38.818565, 494553, 989891},
			"test/hello10":                     {0.000000, 0.000000, 0, 0},
			"test/world-deploy1":               {0.051893, 33.870968, 510526, 990210},
			"test/world-deploy2":               {2.504800, 33.749165, 497249, 989944},
			"kube-system/kubernetes-dashboard": {0.017856, 39.062500, 520000, 990400},
			"other/grafana":                    {0.060557, 35.944212, 518960, 990379},
			"kube-system/l7-default-backend":   {0.020371, 31.508049, 516923, 990338},
		}

		expected := []string{"kube-system/heapster-v1.4.3", "kube-system/kubernetes-dashboard", "kube-system/l7-default-backend",
			"other/grafana", "test/backend4", "test/hello10", "test/world-deploy1", "test/world-deploy2"}

		sorted := sortStatsKeys(unsorted)
		assert.Equal(t, expected, sorted, "Not Sorted!")
	})
}

func generateMetricSeriesFor(podName string, seed int64) []*pb.MetricSeries {
	metricDatapoints := []*pb.MetricDatapoint{
		{
			Value: &pb.MetricValue{
				Value: &pb.MetricValue_Gauge{
					Gauge: float64(seed) / 10,
				},
			},
		},
	}
	latencyHistogram := []*pb.MetricDatapoint{
		{
			Value: &pb.MetricValue{
				Value: &pb.MetricValue_Histogram{
					Histogram: &pb.Histogram{
						Values: []*pb.HistogramValue{
							{
								Label: pb.HistogramLabel_P50,
								Value: 1 + seed,
							},
							{
								Label: pb.HistogramLabel_P99,
								Value: 9 + seed,
							},
							{
								Label: pb.HistogramLabel_P95,
								Value: 5 + seed,
							},
						},
					},
				},
			},
		},
	}
	series := []*pb.MetricSeries{
		{
			Name: pb.MetricName_REQUEST_RATE,
			Metadata: &pb.MetricMetadata{
				TargetPod: podName,
			},
			Datapoints: metricDatapoints,
		},
		{
			Name: pb.MetricName_SUCCESS_RATE,
			Metadata: &pb.MetricMetadata{
				TargetPod: podName,
			},
			Datapoints: metricDatapoints,
		},
		{
			Name: pb.MetricName_LATENCY,
			Metadata: &pb.MetricMetadata{
				TargetPod: podName,
			},
			Datapoints: latencyHistogram,
		},
	}
	return series
}
