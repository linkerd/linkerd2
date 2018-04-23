package util

import (
	"errors"
	"strings"
	"time"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
)

/*
  Shared utilities for interacting with the controller public api
*/

var (
	defaultMetricTimeWindow = "1m"

	// ValidTargets specifies resource types allowed as a target:
	// target resource on an inbound query
	// target resource on an outbound 'to' query
	// destination resource on an outbound 'from' query
	ValidTargets = []string{
		k8s.KubernetesDeployments,
		k8s.KubernetesNamespaces,
		k8s.KubernetesPods,
		k8s.KubernetesReplicationControllers,
	}

	// ValidDestinations specifies resource types allowed as a destination:
	// destination resource on an outbound 'to' query
	// target resource on an outbound 'from' query
	ValidDestinations = []string{
		k8s.KubernetesDeployments,
		k8s.KubernetesNamespaces,
		k8s.KubernetesPods,
		k8s.KubernetesReplicationControllers,
		k8s.KubernetesServices,
	}
)

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
		_, err := time.ParseDuration(p.TimeWindow)
		if err != nil {
			return nil, err
		}
		window = p.TimeWindow
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

// BuildResource parses input strings, typically from CLI flags, to build a
// Resource object for use in the Conduit Public API.
func BuildResource(namespace string, args ...string) (pb.Resource, error) {
	switch len(args) {
	case 0:
		return pb.Resource{}, errors.New("No resource arguments provided")
	case 1:
		elems := strings.Split(args[0], "/")
		switch len(elems) {
		case 1:
			// --namespace my-ns deploy
			return buildResource(namespace, elems[0], "")
		case 2:
			// --namespace my-ns deploy/foo
			return buildResource(namespace, elems[0], elems[1])
		default:
			return pb.Resource{}, errors.New("Invalid resource string: " + args[0])
		}
	case 2:
		// --namespace my-ns deploy foo
		return buildResource(namespace, args[0], args[1])
	default:
		return pb.Resource{}, errors.New("Too many arguments provided for resource: " + strings.Join(args, "/"))
	}
}

func buildResource(namespace string, resType string, name string) (pb.Resource, error) {
	canonicalType, err := k8s.CanonicalKubernetesNameFromFriendlyName(resType)
	if err != nil {
		return pb.Resource{}, err
	}
	if canonicalType == k8s.KubernetesNamespaces {
		// ignore --namespace flags if type is namespace
		namespace = ""
	}

	return pb.Resource{
		Namespace: namespace,
		Type:      canonicalType,
		Name:      name,
	}, nil
}
