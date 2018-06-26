package util

import (
	"errors"
	"strings"
	"time"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		k8s.Deployments,
		k8s.Namespaces,
		k8s.Pods,
		k8s.ReplicationControllers,
		k8s.Authorities,
	}

	// ValidDestinations specifies resource types allowed as a destination:
	// destination resource on an outbound 'to' query
	// target resource on an outbound 'from' query
	ValidDestinations = []string{
		k8s.Deployments,
		k8s.Namespaces,
		k8s.Pods,
		k8s.ReplicationControllers,
		k8s.Services,
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
	AllNamespaces bool
}

// GRPCError generates a gRPC error code, as defined in
// google.golang.org/grpc/status.
// If the error is nil or already a gRPC error, return the error.
// If the error is of type k8s.io/apimachinery/pkg/apis/meta/v1#StatusReason,
// attempt to map the reason to a gRPC error.
func GRPCError(err error) error {
	if err != nil && status.Code(err) == codes.Unknown {
		code := codes.Internal

		switch k8sErrors.ReasonForError(err) {
		case metav1.StatusReasonUnknown:
			code = codes.Unknown
		case metav1.StatusReasonUnauthorized, metav1.StatusReasonForbidden:
			code = codes.PermissionDenied
		case metav1.StatusReasonNotFound:
			code = codes.NotFound
		case metav1.StatusReasonAlreadyExists:
			code = codes.AlreadyExists
		case metav1.StatusReasonInvalid:
			code = codes.InvalidArgument
		case metav1.StatusReasonExpired:
			code = codes.DeadlineExceeded
		case metav1.StatusReasonServiceUnavailable:
			code = codes.Unavailable
		}

		err = status.Error(code, err.Error())
	}

	return err
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

	if p.AllNamespaces && p.ResourceName != "" {
		return nil, errors.New("stats for a resource cannot be retrieved by name across all namespaces")
	}

	targetNamespace := p.Namespace
	if p.AllNamespaces {
		targetNamespace = ""
	} else if p.Namespace == "" {
		targetNamespace = v1.NamespaceDefault
	}

	resourceType, err := k8s.CanonicalResourceNameFromFriendlyName(p.ResourceType)
	if err != nil {
		return nil, err
	}

	statRequest := &pb.StatSummaryRequest{
		Selector: &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: targetNamespace,
				Name:      p.ResourceName,
				Type:      resourceType,
			},
		},
		TimeWindow: window,
	}

	if p.ToName != "" || p.ToType != "" || p.ToNamespace != "" {
		if p.ToNamespace == "" {
			p.ToNamespace = targetNamespace
		}
		if p.ToType == "" {
			p.ToType = resourceType
		}

		toType, err := k8s.CanonicalResourceNameFromFriendlyName(p.ToType)
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
			p.FromNamespace = targetNamespace
		}
		if p.FromType == "" {
			p.FromType = resourceType
		}

		fromType, err := validateFromResourceType(p.FromType)
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

// An authority can only receive traffic, not send it, so it can't be a --from
func validateFromResourceType(resourceType string) (string, error) {
	name, err := k8s.CanonicalResourceNameFromFriendlyName(resourceType)
	if err != nil {
		return "", err
	}
	if name == k8s.Authorities {
		return "", errors.New("cannot query traffic --from an authority")
	}
	return name, nil
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
	canonicalType, err := k8s.CanonicalResourceNameFromFriendlyName(resType)
	if err != nil {
		return pb.Resource{}, err
	}
	if canonicalType == k8s.Namespaces {
		// ignore --namespace flags if type is namespace
		namespace = ""
	}

	return pb.Resource{
		Namespace: namespace,
		Type:      canonicalType,
		Name:      name,
	}, nil
}
