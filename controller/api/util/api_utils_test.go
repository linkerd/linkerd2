package util

import (
	"testing"

	pb "github.com/runconduit/conduit/controller/gen/public"
)

func TestGetWindow(t *testing.T) {
	t.Run("Returns valid windows", func(t *testing.T) {
		expectations := map[string]pb.TimeWindow{
			"10s": pb.TimeWindow_TEN_SEC,
			"1m":  pb.TimeWindow_ONE_MIN,
			"10m": pb.TimeWindow_TEN_MIN,
			"1h":  pb.TimeWindow_ONE_HOUR,
		}

		for windowFriendlyName, expectedTimeWindow := range expectations {
			actualTimeWindow, err := GetWindow(windowFriendlyName)
			if err != nil {
				t.Fatalf("Unexpected error when resolving time window friendly name [%s]: %v",
					windowFriendlyName, err)
			}

			if actualTimeWindow != expectedTimeWindow {
				t.Fatalf("Expected resolving friendly name [%s] to return timw window [%v], but got [%v]",
					windowFriendlyName, expectedTimeWindow, actualTimeWindow)
			}
		}
	})

	t.Run("Returns error and default value if unknown friendly name for TimeWindow", func(t *testing.T) {
		invalidNames := []string{
			"10seconds", "10sec", "9s",
			"10minutes", "10min", "9m",
			"1minute", "1min", "0s", "2s",
			"1hour", "0h", "2h",
			"10", ""}
		defaultTimeWindow := pb.TimeWindow_ONE_MIN

		for _, invalidName := range invalidNames {
			window, err := GetWindow(invalidName)
			if err == nil {
				t.Fatalf("Expected invalid friendly name [%s] to generate error, but got no error and result [%v]",
					invalidName, window)
			}

			if window != defaultTimeWindow {
				t.Fatalf("Expected invalid friendly name resolution to return default window [%v], but got [%v]",
					defaultTimeWindow, window)
			}
		}
	})
}

func TestGetWindowString(t *testing.T) {
	t.Run("Returns names for valid windows", func(t *testing.T) {
		expectations := map[pb.TimeWindow]string{
			pb.TimeWindow_TEN_SEC:  "10s",
			pb.TimeWindow_ONE_MIN:  "1m",
			pb.TimeWindow_TEN_MIN:  "10m",
			pb.TimeWindow_ONE_HOUR: "1h",
		}

		for window, expectedName := range expectations {
			actualName, err := GetWindowString(window)
			if err != nil {
				t.Fatalf("Unexpected error when resolving name for window [%v]: %v", window, err)
			}

			if actualName != expectedName {
				t.Fatalf("Expected window [%v] to resolve to name [%s], but got [%s]", window, expectedName, actualName)
			}
		}
	})
}

func TestGetMetricName(t *testing.T) {
	t.Run("Returns valid metrics from name", func(t *testing.T) {
		expectations := map[string]pb.MetricName{
			"requests":    pb.MetricName_REQUEST_RATE,
			"latency":     pb.MetricName_LATENCY,
			"successRate": pb.MetricName_SUCCESS_RATE,
		}

		for metricFriendlyName, expectedMetricName := range expectations {
			actualMetricName, err := GetMetricName(metricFriendlyName)
			if err != nil {
				t.Fatalf("Unexpected error when resolving metric friendly name [%s]: %v",
					metricFriendlyName, err)
			}

			if actualMetricName != expectedMetricName {
				t.Fatalf("Expected resolving metric friendly name [%s] to return metric [%v], but got [%v]",
					metricFriendlyName, expectedMetricName, actualMetricName)
			}
		}
	})

	t.Run("Returns error and default value if unknown friendly name for TimeWindow", func(t *testing.T) {
		invalidNames := []string{"failureRate", ""}
		defaultMetricName := pb.MetricName_REQUEST_RATE

		for _, invalidName := range invalidNames {
			window, err := GetMetricName(invalidName)
			if err == nil {
				t.Fatalf("Expected invalid friendly name [%s] to generate error, but got no error and result [%v]",
					invalidName, window)
			}

			if window != defaultMetricName {
				t.Fatalf("Expected invalid friendly name resolution to return default name [%v], but got [%v]",
					defaultMetricName, window)
			}
		}
	})
}

func TestGetAggregationType(t *testing.T) {
	t.Run("Returns valid metrics from name", func(t *testing.T) {
		expectations := map[string]pb.AggregationType{
			"target_pod":    pb.AggregationType_TARGET_POD,
			"target_deploy": pb.AggregationType_TARGET_DEPLOY,
			"source_pod":    pb.AggregationType_SOURCE_POD,
			"source_deploy": pb.AggregationType_SOURCE_DEPLOY,
			"mesh":          pb.AggregationType_MESH,
			"path":          pb.AggregationType_PATH,
		}

		for aggregationFriendlyName, expectedAggregation := range expectations {
			actualAggregation, err := GetAggregationType(aggregationFriendlyName)
			if err != nil {
				t.Fatalf("Unexpected error when resolving friendly name [%s]: %v",
					aggregationFriendlyName, err)
			}

			if actualAggregation != expectedAggregation {
				t.Fatalf("Expected resolving friendly name [%s] to return [%v], but got [%v]",
					aggregationFriendlyName, expectedAggregation, actualAggregation)
			}
		}
	})

	t.Run("Returns error and default value if unknown friendly name for TimeWindow", func(t *testing.T) {
		invalidNames := []string{"pod_target", "pod", "service", "target_service", "target_mesh", ""}
		defaultAggregation := pb.AggregationType_TARGET_POD

		for _, invalidName := range invalidNames {
			aggregation, err := GetAggregationType(invalidName)
			if err == nil {
				t.Fatalf("Expected invalid friendly name [%s] to generate error, but got no error and result [%v]",
					invalidName, aggregation)
			}

			if aggregation != defaultAggregation {
				t.Fatalf("Expected invalid friendly name resolution to return default [%v], but got [%v]",
					defaultAggregation, aggregation)
			}
		}
	})
}
