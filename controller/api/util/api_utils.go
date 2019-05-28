package util

import (
	"errors"
	"fmt"
	"strings"
	"time"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
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
		k8s.Authority,
		k8s.DaemonSet,
		k8s.Deployment,
		k8s.Job,
		k8s.Namespace,
		k8s.Pod,
		k8s.ReplicationController,
		k8s.StatefulSet,
	}

	// ValidTapDestinations specifies resource types allowed as a tap destination:
	// destination resource on an outbound 'to' query
	ValidTapDestinations = []string{
		k8s.DaemonSet,
		k8s.Deployment,
		k8s.Job,
		k8s.Namespace,
		k8s.Pod,
		k8s.ReplicationController,
		k8s.Service,
		k8s.StatefulSet,
	}
)

// StatsBaseRequestParams contains parameters that are used to build requests
// for metrics data.  This includes requests to StatSummary and TopRoutes.
type StatsBaseRequestParams struct {
	TimeWindow    string
	Namespace     string
	ResourceType  string
	ResourceName  string
	AllNamespaces bool
}

// StatsSummaryRequestParams contains parameters that are used to build
// StatSummary requests.
type StatsSummaryRequestParams struct {
	StatsBaseRequestParams
	ToNamespace   string
	ToType        string
	ToName        string
	FromNamespace string
	FromType      string
	FromName      string
	SkipStats     bool
	TCPStats      bool
}

// EdgesRequestParams contains parameters that are used to build
// Edges requests.
type EdgesRequestParams struct {
	Namespace    string
	ResourceType string
}

// TopRoutesRequestParams contains parameters that are used to build TopRoutes
// requests.
type TopRoutesRequestParams struct {
	StatsBaseRequestParams
	ToNamespace string
	ToType      string
	ToName      string
}

// TapRequestParams contains parameters that are used to build a
// TapByResourceRequest.
type TapRequestParams struct {
	Resource    string
	Namespace   string
	ToResource  string
	ToNamespace string
	MaxRps      float32
	Scheme      string
	Method      string
	Authority   string
	Path        string
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

// BuildStatSummaryRequest builds a Public API StatSummaryRequest from a
// StatsSummaryRequestParams.
func BuildStatSummaryRequest(p StatsSummaryRequestParams) (*pb.StatSummaryRequest, error) {
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
		targetNamespace = corev1.NamespaceDefault
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
		SkipStats:  p.SkipStats,
		TcpStats:   p.TCPStats,
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

// BuildEdgesRequest builds a Public API EdgesRequest from a
// EdgesRequestParams.
func BuildEdgesRequest(p EdgesRequestParams) (*pb.EdgesRequest, error) {

	namespace := p.Namespace
	if p.Namespace == "" {
		namespace = corev1.NamespaceDefault
	}

	resourceType, err := k8s.CanonicalResourceNameFromFriendlyName(p.ResourceType)
	if err != nil {
		return nil, err
	}

	edgesRequest := &pb.EdgesRequest{
		Selector: &pb.ResourceSelection{
			Resource: &pb.Resource{
				Namespace: namespace,
				Type:      resourceType,
			},
		},
	}

	return edgesRequest, nil
}

// BuildTopRoutesRequest builds a Public API TopRoutesRequest from a
// TopRoutesRequestParams.
func BuildTopRoutesRequest(p TopRoutesRequestParams) (*pb.TopRoutesRequest, error) {
	window := defaultMetricTimeWindow
	if p.TimeWindow != "" {
		_, err := time.ParseDuration(p.TimeWindow)
		if err != nil {
			return nil, err
		}
		window = p.TimeWindow
	}

	if p.AllNamespaces && p.ResourceName != "" {
		return nil, errors.New("routes for a resource cannot be retrieved by name across all namespaces")
	}

	targetNamespace := p.Namespace
	if p.AllNamespaces {
		targetNamespace = ""
	} else if p.Namespace == "" {
		targetNamespace = corev1.NamespaceDefault
	}

	resourceType, err := k8s.CanonicalResourceNameFromFriendlyName(p.ResourceType)
	if err != nil {
		return nil, err
	}

	topRoutesRequest := &pb.TopRoutesRequest{
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

		toResource := pb.TopRoutesRequest_ToResource{
			ToResource: &pb.Resource{
				Namespace: p.ToNamespace,
				Type:      toType,
				Name:      p.ToName,
			},
		}
		topRoutesRequest.Outbound = &toResource
	} else {
		topRoutesRequest.Outbound = &pb.TopRoutesRequest_None{
			None: &pb.Empty{},
		}
	}

	return topRoutesRequest, nil
}

// An authority can only receive traffic, not send it, so it can't be a --from
func validateFromResourceType(resourceType string) (string, error) {
	name, err := k8s.CanonicalResourceNameFromFriendlyName(resourceType)
	if err != nil {
		return "", err
	}
	if name == k8s.Authority {
		return "", errors.New("cannot query traffic --from an authority")
	}
	return name, nil
}

// BuildResource parses input strings, typically from CLI flags, to build a
// Resource object for use in the protobuf API.
// It's the same as BuildResources but only admits one arg and only returns one resource
func BuildResource(namespace, arg string) (pb.Resource, error) {
	res, err := BuildResources(namespace, []string{arg})
	if err != nil {
		return pb.Resource{}, err
	}

	return res[0], err
}

// BuildResources parses input strings, typically from CLI flags, to build a
// slice of Resource objects for use in the protobuf API.
// It's the same as BuildResource but it admits any number of args and returns multiple resources
func BuildResources(namespace string, args []string) ([]pb.Resource, error) {
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

func parseResources(namespace string, resType string, args []string) ([]pb.Resource, error) {
	if err := validateResources(args); err != nil {
		return nil, err
	}
	resources := make([]pb.Resource, 0)
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

func parseResource(namespace, resType string, arg string) (pb.Resource, error) {
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
		return pb.Resource{}, errors.New("Invalid resource string: " + arg)
	}
}

func buildResource(namespace string, resType string, name string) (pb.Resource, error) {
	canonicalType, err := k8s.CanonicalResourceNameFromFriendlyName(resType)
	if err != nil {
		return pb.Resource{}, err
	}
	if canonicalType == k8s.Namespace {
		// ignore --namespace flags if type is namespace
		namespace = ""
	}

	return pb.Resource{
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
					Resource: &destination,
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

	return &pb.TapByResourceRequest{
		Target: &pb.ResourceSelection{
			Resource: &target,
		},
		MaxRps: params.MaxRps,
		Match: &pb.TapByResourceRequest_Match{
			Match: &pb.TapByResourceRequest_Match_All{
				All: &pb.TapByResourceRequest_Match_Seq{
					Matches: matches,
				},
			},
		},
	}, nil
}

func buildMatchHTTP(match *pb.TapByResourceRequest_Match_Http) pb.TapByResourceRequest_Match {
	return pb.TapByResourceRequest_Match{
		Match: &pb.TapByResourceRequest_Match_Http_{
			Http: match,
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
func CreateTapEvent(eventHTTP *pb.TapEvent_Http, dstMeta map[string]string, proxyDirection pb.TapEvent_ProxyDirection) pb.TapEvent {
	event := pb.TapEvent{
		ProxyDirection: proxyDirection,
		Source: &pb.TcpAddress{
			Ip: &pb.IPAddress{
				Ip: &pb.IPAddress_Ipv4{
					Ipv4: uint32(1),
				},
			},
		},
		Destination: &pb.TcpAddress{
			Ip: &pb.IPAddress{
				Ip: &pb.IPAddress_Ipv4{
					Ipv4: uint32(9),
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

// K8sPodToPublicPod converts a Kubernetes Pod to a Public API Pod
func K8sPodToPublicPod(pod corev1.Pod, ownerKind string, ownerName string) pb.Pod {
	status := string(pod.Status.Phase)
	if pod.DeletionTimestamp != nil {
		status = "Terminating"
	}
	controllerComponent := pod.Labels[k8s.ControllerComponentLabel]
	controllerNS := pod.Labels[k8s.ControllerNSLabel]

	proxyReady := false
	for _, container := range pod.Status.ContainerStatuses {
		if container.Name == k8s.ProxyContainerName {
			proxyReady = container.Ready
		}
	}

	proxyVersion := ""
	for _, container := range pod.Spec.Containers {
		if container.Name == k8s.ProxyContainerName {
			parts := strings.Split(container.Image, ":")
			proxyVersion = parts[1]
		}
	}

	item := pb.Pod{
		Name:                pod.Namespace + "/" + pod.Name,
		Status:              status,
		PodIP:               pod.Status.PodIP,
		ControllerNamespace: controllerNS,
		ControlPlane:        controllerComponent != "",
		ProxyReady:          proxyReady,
		ProxyVersion:        proxyVersion,
		ResourceVersion:     pod.ResourceVersion,
	}

	namespacedOwnerName := pod.Namespace + "/" + ownerName

	switch ownerKind {
	case k8s.Deployment:
		item.Owner = &pb.Pod_Deployment{Deployment: namespacedOwnerName}
	case k8s.DaemonSet:
		item.Owner = &pb.Pod_DaemonSet{DaemonSet: namespacedOwnerName}
	case k8s.Job:
		item.Owner = &pb.Pod_Job{Job: namespacedOwnerName}
	case k8s.ReplicaSet:
		item.Owner = &pb.Pod_ReplicaSet{ReplicaSet: namespacedOwnerName}
	case k8s.ReplicationController:
		item.Owner = &pb.Pod_ReplicationController{ReplicationController: namespacedOwnerName}
	case k8s.StatefulSet:
		item.Owner = &pb.Pod_StatefulSet{StatefulSet: namespacedOwnerName}
	}

	return item
}
