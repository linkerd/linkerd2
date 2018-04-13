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
	TimeWindow    string
	Namespace     string
	ResourceType  string
	ResourceName  string
	ToNamespace   string
	ToType        string
	ToName        string
	FromNamespace string
	FromType      string
	FromName      string
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

	if p.ToName != "" || p.ToType != "" || p.ToNamespace != "" {
		if p.ToNamespace == "" {
			p.ToNamespace = p.Namespace
		}
		if p.ToType == "" {
			p.ToType = resourceType
		}

		toType, err := k8s.CanonicalKubernetesNameFromFriendlyName(p.ToType)
		if err != nil {
			return nil, err
		}

		toResource := pb.StatSummaryRequest_ToResource{
			ToResource: &pb.Resource{
				Namespace: p.ToNamespace,
				Type:      toType,
				Name:      p.ToName,
			},
		}
		statRequest.Outbound = &toResource
	}

	if p.FromName != "" || p.FromType != "" || p.FromNamespace != "" {
		if p.FromNamespace == "" {
			p.FromNamespace = p.Namespace
		}
		if p.FromType == "" {
			p.FromType = resourceType
		}

		fromType, err := k8s.CanonicalKubernetesNameFromFriendlyName(p.FromType)
		if err != nil {
			return nil, err
		}

		fromResource := pb.StatSummaryRequest_FromResource{
			FromResource: &pb.Resource{
				Namespace: p.FromNamespace,
				Type:      fromType,
				Name:      p.FromName,
			},
		}
		statRequest.Outbound = &fromResource
	}

	return statRequest, nil
}
