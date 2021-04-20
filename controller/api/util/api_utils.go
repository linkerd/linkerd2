package util

import (
	"errors"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

/*
  Shared utilities for interacting with the controller APIs
*/

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

// BuildResource parses input strings, typically from CLI flags, to build a
// Resource object for use in the protobuf API.
// It's the same as BuildResources but only admits one arg and only returns one resource
func BuildResource(namespace, arg string) (*pb.Resource, error) {
	res, err := BuildResources(namespace, []string{arg})
	if err != nil {
		return &pb.Resource{}, err
	}

	return res[0], err
}

// BuildResources parses input strings, typically from CLI flags, to build a
// slice of Resource objects for use in the protobuf API.
// It's the same as BuildResource but it admits any number of args and returns multiple resources
func BuildResources(namespace string, args []string) ([]*pb.Resource, error) {
	switch len(args) {
	case 0:
		return nil, errors.New("No resource arguments provided")
	case 1:
		return parseResources(namespace, "", args)
	default:
		if res, err := k8s.CanonicalResourceNameFromFriendlyName(args[0]); err == nil && res != k8s.All {
			// --namespace my-ns deploy foo1 foo2 ...
			return parseResources(namespace, args[0], args[1:])
		}

		return parseResources(namespace, "", args)
	}
}

func parseResources(namespace string, resType string, args []string) ([]*pb.Resource, error) {
	if err := validateResources(args); err != nil {
		return nil, err
	}
	resources := make([]*pb.Resource, 0)
	for _, arg := range args {
		res, err := parseResource(namespace, resType, arg)
		if err != nil {
			return nil, err
		}
		resources = append(resources, res)
	}
	return resources, nil
}

func validateResources(args []string) error {
	set := make(map[string]bool)
	all := false
	for _, arg := range args {
		set[arg] = true
		if arg == k8s.All {
			all = true
		}
	}
	if len(set) < len(args) {
		return errors.New("cannot supply duplicate resources")
	}
	if all && len(args) > 1 {
		return errors.New("'all' can't be supplied alongside other resources")
	}
	return nil
}

func parseResource(namespace, resType string, arg string) (*pb.Resource, error) {
	if resType != "" {
		return buildResource(namespace, resType, arg)
	}
	elems := strings.Split(arg, "/")
	switch len(elems) {
	case 1:
		// --namespace my-ns deploy
		return buildResource(namespace, elems[0], "")
	case 2:
		// --namespace my-ns deploy/foo
		return buildResource(namespace, elems[0], elems[1])
	default:
		return &pb.Resource{}, errors.New("Invalid resource string: " + arg)
	}
}

func buildResource(namespace string, resType string, name string) (*pb.Resource, error) {
	canonicalType, err := k8s.CanonicalResourceNameFromFriendlyName(resType)
	if err != nil {
		return &pb.Resource{}, err
	}
	if canonicalType == k8s.Namespace {
		// ignore --namespace flags if type is namespace
		namespace = ""
	}

	return &pb.Resource{
		Namespace: namespace,
		Type:      canonicalType,
		Name:      name,
	}, nil
}
