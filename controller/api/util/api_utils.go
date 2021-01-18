package util

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	netPb "github.com/linkerd/linkerd2/controller/gen/common/net"
	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

/*
  Shared utilities for interacting with the controller public api
*/

var (
	// ValidTargets specifies resource types allowed as a target:
	// target resource on an inbound query
	// target resource on an outbound 'to' query
	// destination resource on an outbound 'from' query
	ValidTargets = []string{
		k8s.Authority,
		k8s.CronJob,
		k8s.DaemonSet,
		k8s.Deployment,
		k8s.Job,
		k8s.Namespace,
		k8s.Pod,
		k8s.ReplicaSet,
		k8s.ReplicationController,
		k8s.StatefulSet,
	}

	// ValidTapDestinations specifies resource types allowed as a tap destination:
	// destination resource on an outbound 'to' query
	ValidTapDestinations = []string{
		k8s.CronJob,
		k8s.DaemonSet,
		k8s.Deployment,
		k8s.Job,
		k8s.Namespace,
		k8s.Pod,
		k8s.ReplicaSet,
		k8s.ReplicationController,
		k8s.Service,
		k8s.StatefulSet,
	}
)

// TapRequestParams contains parameters that are used to build a
// TapByResourceRequest.
type TapRequestParams struct {
	Resource      string
	Namespace     string
	ToResource    string
	ToNamespace   string
	MaxRps        float32
	Scheme        string
	Method        string
	Authority     string
	Path          string
	Extract       bool
	LabelSelector string
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

// BuildTapByResourceRequest builds a Public API TapByResourceRequest from a
// TapRequestParams.
func BuildTapByResourceRequest(params TapRequestParams) (*pb.TapByResourceRequest, error) {
	target, err := BuildResource(params.Namespace, params.Resource)
	if err != nil {
		return nil, fmt.Errorf("target resource invalid: %s", err)
	}
	if !contains(ValidTargets, target.Type) {
		return nil, fmt.Errorf("unsupported resource type [%s]", target.Type)
	}

	matches := []*pb.TapByResourceRequest_Match{}

	if params.ToResource != "" {
		destination, err := BuildResource(params.ToNamespace, params.ToResource)
		if err != nil {
			return nil, fmt.Errorf("destination resource invalid: %s", err)
		}
		if !contains(ValidTapDestinations, destination.Type) {
			return nil, fmt.Errorf("unsupported resource type [%s]", destination.Type)
		}

		match := pb.TapByResourceRequest_Match{
			Match: &pb.TapByResourceRequest_Match_Destinations{
				Destinations: &pb.ResourceSelection{
					Resource: destination,
				},
			},
		}
		matches = append(matches, &match)
	}

	if params.Scheme != "" {
		match := buildMatchHTTP(&pb.TapByResourceRequest_Match_Http{
			Match: &pb.TapByResourceRequest_Match_Http_Scheme{Scheme: params.Scheme},
		})
		matches = append(matches, &match)
	}
	if params.Method != "" {
		match := buildMatchHTTP(&pb.TapByResourceRequest_Match_Http{
			Match: &pb.TapByResourceRequest_Match_Http_Method{Method: params.Method},
		})
		matches = append(matches, &match)
	}
	if params.Authority != "" {
		match := buildMatchHTTP(&pb.TapByResourceRequest_Match_Http{
			Match: &pb.TapByResourceRequest_Match_Http_Authority{Authority: params.Authority},
		})
		matches = append(matches, &match)
	}
	if params.Path != "" {
		match := buildMatchHTTP(&pb.TapByResourceRequest_Match_Http{
			Match: &pb.TapByResourceRequest_Match_Http_Path{Path: params.Path},
		})
		matches = append(matches, &match)
	}

	extract := &pb.TapByResourceRequest_Extract{}
	if params.Extract {
		extract = buildExtractHTTP(&pb.TapByResourceRequest_Extract_Http{
			Extract: &pb.TapByResourceRequest_Extract_Http_Headers_{
				Headers: &pb.TapByResourceRequest_Extract_Http_Headers{},
			},
		})
	}

	return &pb.TapByResourceRequest{
		Target: &pb.ResourceSelection{
			Resource:      target,
			LabelSelector: params.LabelSelector,
		},
		MaxRps: params.MaxRps,
		Match: &pb.TapByResourceRequest_Match{
			Match: &pb.TapByResourceRequest_Match_All{
				All: &pb.TapByResourceRequest_Match_Seq{
					Matches: matches,
				},
			},
		},
		Extract: extract,
	}, nil
}

func buildMatchHTTP(match *pb.TapByResourceRequest_Match_Http) pb.TapByResourceRequest_Match {
	return pb.TapByResourceRequest_Match{
		Match: &pb.TapByResourceRequest_Match_Http_{
			Http: match,
		},
	}
}

func buildExtractHTTP(extract *pb.TapByResourceRequest_Extract_Http) *pb.TapByResourceRequest_Extract {
	return &pb.TapByResourceRequest_Extract{
		Extract: &pb.TapByResourceRequest_Extract_Http_{
			Http: extract,
		},
	}
}

func contains(list []string, s string) bool {
	for _, elem := range list {
		if s == elem {
			return true
		}
	}
	return false
}

// CreateTapEvent generates tap events for use in tests
func CreateTapEvent(eventHTTP *pb.TapEvent_Http, dstMeta map[string]string, proxyDirection pb.TapEvent_ProxyDirection) *pb.TapEvent {
	event := &pb.TapEvent{
		ProxyDirection: proxyDirection,
		Source: &netPb.TcpAddress{
			Ip: &netPb.IPAddress{
				Ip: &netPb.IPAddress_Ipv4{
					Ipv4: uint32(1),
				},
			},
		},
		Destination: &netPb.TcpAddress{
			Ip: &netPb.IPAddress{
				Ip: &netPb.IPAddress_Ipv6{
					Ipv6: &netPb.IPv6{
						// All nodes address: https://www.iana.org/assignments/ipv6-multicast-addresses/ipv6-multicast-addresses.xhtml
						First: binary.BigEndian.Uint64([]byte{0xff, 0x01, 0, 0, 0, 0, 0, 0}),
						Last:  binary.BigEndian.Uint64([]byte{0, 0, 0, 0, 0, 0, 0, 0x01}),
					},
				},
			},
		},
		Event: &pb.TapEvent_Http_{
			Http: eventHTTP,
		},
		DestinationMeta: &pb.TapEvent_EndpointMeta{
			Labels: dstMeta,
		},
	}
	return event
}
