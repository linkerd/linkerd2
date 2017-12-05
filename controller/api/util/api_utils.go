package util

import (
	"errors"

	pb "github.com/runconduit/conduit/controller/gen/public"
)

/*
  Shared utilities for interacting with the controller public api
*/

func GetWindow(timeWindow string) (pb.TimeWindow, error) {
	switch timeWindow {
	case "10s":
		return pb.TimeWindow_TEN_SEC, nil
	case "1m":
		return pb.TimeWindow_ONE_MIN, nil
	case "10m":
		return pb.TimeWindow_TEN_MIN, nil
	case "1h":
		return pb.TimeWindow_ONE_HOUR, nil
	}
	return pb.TimeWindow_ONE_MIN, errors.New("invalid time-window " + timeWindow)
}

func GetWindowString(timeWindow pb.TimeWindow) (string, error) {
	switch timeWindow {
	case pb.TimeWindow_TEN_SEC:
		return "10s", nil
	case pb.TimeWindow_ONE_MIN:
		return "1m", nil
	case pb.TimeWindow_TEN_MIN:
		return "10m", nil
	case pb.TimeWindow_ONE_HOUR:
		return "1h", nil
	}
	return "", errors.New("invalid time-window " + timeWindow.String())
}

func GetMetricName(metricName string) (pb.MetricName, error) {
	switch metricName {
	case "requests":
		return pb.MetricName_REQUEST_RATE, nil
	case "latency":
		return pb.MetricName_LATENCY, nil
	case "successRate":
		return pb.MetricName_SUCCESS_RATE, nil
	}
	return pb.MetricName_REQUEST_RATE, errors.New("invalid metric name " + metricName)
}

func GetAggregationType(aggregationType string) (pb.AggregationType, error) {
	switch aggregationType {
	case "target_pod":
		return pb.AggregationType_TARGET_POD, nil
	case "target_deploy":
		return pb.AggregationType_TARGET_DEPLOY, nil
	case "source_pod":
		return pb.AggregationType_SOURCE_POD, nil
	case "source_deploy":
		return pb.AggregationType_SOURCE_DEPLOY, nil
	case "mesh":
		return pb.AggregationType_MESH, nil
	}
	return pb.AggregationType_TARGET_POD, errors.New("invalid aggregation type " + aggregationType)
}
