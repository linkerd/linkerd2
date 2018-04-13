package util

import (
	"errors"
	"fmt"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
)

/*
  Shared utilities for interacting with the controller public api
*/

func GetWindow(timeWindowFriendlyName string) (pb.TimeWindow, error) {
	switch timeWindowFriendlyName {
	case "10s":
		return pb.TimeWindow_TEN_SEC, nil
	case "1m":
		return pb.TimeWindow_ONE_MIN, nil
	case "10m":
		return pb.TimeWindow_TEN_MIN, nil
	case "1h":
		return pb.TimeWindow_ONE_HOUR, nil
	default:
		return pb.TimeWindow_ONE_MIN, errors.New("invalid time-window " + timeWindowFriendlyName)
	}
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
	default:
		return "", fmt.Errorf("invalid time-window %v", timeWindow)
	}
}

var defaultMetricTimeWindow = pb.TimeWindow_ONE_MIN

type StatSummaryRequestParams struct {
	TimeWindow       string
	Namespace        string
	ResourceType     string
	ResourceName     string
	OutToNamespace   string
	OutToType        string
	OutToName        string
	OutFromNamespace string
	OutFromType      string
	OutFromName      string
}

func BuildStatSummaryRequest(p StatSummaryRequestParams) (*pb.StatSummaryRequest, error) {
	window := defaultMetricTimeWindow
	if p.TimeWindow != "" {
		var err error
		window, err = GetWindow(p.TimeWindow)
		if err != nil {
			return nil, err
		}
	}

	resourceType, err := k8s.CanonicalKubernetesNameFromFriendlyName(p.ResourceType)
	if err != nil {
		return nil, err
	}

	statRequest := &pb.StatSummaryRequest{
		Selector: &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: p.Namespace,
				Name:      p.ResourceName,
				Type:      resourceType,
			},
		},
		TimeWindow: window,
	}

	if p.OutToName != "" || p.OutToType != "" || p.OutToNamespace != "" {
		if p.OutToNamespace == "" {
			p.OutToNamespace = p.Namespace
		}

		outToResource := pb.StatSummaryRequest_OutToResource{
			OutToResource: &pb.Resource{
				Namespace: p.OutToNamespace,
				Type:      p.OutToType,
				Name:      p.OutToName,
			},
		}
		statRequest.Outbound = &outToResource
	}

	if p.OutFromName != "" || p.OutFromType != "" || p.OutFromNamespace != "" {
		if p.OutFromNamespace == "" {
			p.OutFromNamespace = p.Namespace
		}

		outFromResource := pb.StatSummaryRequest_OutFromResource{
			OutFromResource: &pb.Resource{
				Namespace: p.OutFromNamespace,
				Type:      p.OutFromType,
				Name:      p.OutFromName,
			},
		}
		statRequest.Outbound = &outFromResource
	}

	return statRequest, nil
}
