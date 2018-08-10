package util

import (
	"errors"
	"fmt"
	"strings"
	"time"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/addr"
	"github.com/linkerd/linkerd2/pkg/k8s"
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
		k8s.Deployment,
		k8s.Namespace,
		k8s.Pod,
		k8s.ReplicationController,
		k8s.Authority,
	}

	// ValidDestinations specifies resource types allowed as a destination:
	// destination resource on an outbound 'to' query
	// target resource on an outbound 'from' query
	ValidDestinations = []string{
		k8s.Deployment,
		k8s.Namespace,
		k8s.Pod,
		k8s.ReplicationController,
		k8s.Service,
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
	if name == k8s.Authority {
		return "", errors.New("cannot query traffic --from an authority")
	}
	return name, nil
}

// BuildResource parses input strings, typically from CLI flags, to build a
// Resource object for use in the protobuf API.
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
		if !contains(ValidDestinations, destination.Type) {
			return nil, fmt.Errorf("unsupported resource type [%s]", target.Type)
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

type peer struct {
	address   *pb.TcpAddress
	labels    map[string]string
	direction string
}

// src returns the source peer of a `TapEvent`.
func src(event *pb.TapEvent) peer {
	return peer{
		address:   event.GetSource(),
		labels:    event.GetSourceMeta().GetLabels(),
		direction: "src",
	}
}

// dst returns the destination peer of a `TapEvent`.
func dst(event *pb.TapEvent) peer {
	return peer{
		address:   event.GetDestination(),
		labels:    event.GetDestinationMeta().GetLabels(),
		direction: "dst",
	}
}

// format formats the `src` or `dst` element in the tap output corresponding to
// this peer. The returned label consists of the the peer's direction ("src" or
// "dst"), pod name if it was labeled with one or IP address if not, and port.
func (p *peer) format() string {
	if pod, exists := p.labels["pod"]; exists {
		return fmt.Sprintf("%s=%s:%d", p.direction, pod, p.address.GetPort())
	} else {
		return fmt.Sprintf(
			"%s=%s",
			p.direction,
			addr.PublicAddressToString(p.address),
		)
	}
}

// A list of Kubernetes resource types iterated over by `formatResource`.
var resources = [...]string{
	k8s.Deployment,
	k8s.DaemonSet,
	k8s.ReplicaSet,
	k8s.ReplicationController,
	k8s.Service,
	k8s.StatefulSet,
	// The ordering here actually matters somewhat: Since we already show the
	// pod name of a `src`/`dst` instead of its IP address if it has a pod
	// label, we should only use the pod name as the `src_resource` or
	// `dst_resource` IFF it is not labeled as being part of any other
	// resource. Therefore, the pod key should be the last key we try to look
	// up in the labels map. To ensure that's the case, `k8s.Pod` should always
	// be the last item in this array.
	k8s.Pod,
}

// formatResource formats what Kubernetes resource a peer is part of, if there
// is a label in the peer's metadata that identifies it as being part of a
// resource. Otherwise, it falls back to the authority of the peer, if one
// was passed in as `orAuthority`, or finally to returning an empty string.
func (p *peer) formatResource(orAuthority string) string {
	for _, resourceKind := range resources {
		if resourceName, exists := p.labels[resourceKind]; exists {
			var kind string
			if short := k8s.ShortNameFromCanonicalResourceName(resourceKind); short != "" {
				kind = short
			} else {
				kind = resourceKind
			}
			s := fmt.Sprintf(
				" %s_resource=%s/%s",
				p.direction,
				kind,
				resourceName,
			)
			return s
		}
	}
	if orAuthority != "" {
		return fmt.Sprintf(" %s_resource=au/%s", p.direction, orAuthority)
	} else {
		return ""
	}
}

func (p *peer) tlsStatus() string {
	return p.labels["tls"]
}

func RenderTapEvent(event *pb.TapEvent) string {
	dst := dst(event)
	src := src(event)

	proxy := "???"
	tls := ""
	switch event.GetProxyDirection() {
	case pb.TapEvent_INBOUND:
		proxy = "in " // A space is added so it aligns with `out`.
		tls = src.tlsStatus()
	case pb.TapEvent_OUTBOUND:
		proxy = "out"
		tls = dst.tlsStatus()
	default:
		// Too old for TLS.
	}

	flow := fmt.Sprintf("proxy=%s %s %s tls=%s",
		proxy,
		src.format(),
		dst.format(),
		tls,
	)

	switch ev := event.GetHttp().GetEvent().(type) {
	case *pb.TapEvent_Http_RequestInit_:
		return fmt.Sprintf("req id=%d:%d %s :method=%s :authority=%s :path=%s%s%s",
			ev.RequestInit.GetId().GetBase(),
			ev.RequestInit.GetId().GetStream(),
			flow,
			ev.RequestInit.GetMethod().GetRegistered().String(),
			ev.RequestInit.GetAuthority(),
			ev.RequestInit.GetPath(),
			src.formatResource(""),
			dst.formatResource(ev.RequestInit.GetAuthority()),
		)

	case *pb.TapEvent_Http_ResponseInit_:
		return fmt.Sprintf("rsp id=%d:%d %s :status=%d latency=%dµs%s%s",
			ev.ResponseInit.GetId().GetBase(),
			ev.ResponseInit.GetId().GetStream(),
			flow,
			ev.ResponseInit.GetHttpStatus(),
			ev.ResponseInit.GetSinceRequestInit().GetNanos()/1000,
			src.formatResource(""),
			dst.formatResource(""),
		)

	case *pb.TapEvent_Http_ResponseEnd_:
		switch eos := ev.ResponseEnd.GetEos().GetEnd().(type) {
		case *pb.Eos_GrpcStatusCode:
			return fmt.Sprintf(
				"end id=%d:%d %s grpc-status=%s duration=%dµs response-length=%dB%s%s",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				codes.Code(eos.GrpcStatusCode),
				ev.ResponseEnd.GetSinceResponseInit().GetNanos()/1000,
				ev.ResponseEnd.GetResponseBytes(),
				src.formatResource(""),
				dst.formatResource(""),
			)

		case *pb.Eos_ResetErrorCode:
			return fmt.Sprintf(
				"end id=%d:%d %s reset-error=%+v duration=%dµs response-length=%dB%s%s",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				eos.ResetErrorCode,
				ev.ResponseEnd.GetSinceResponseInit().GetNanos()/1000,
				ev.ResponseEnd.GetResponseBytes(),
				src.formatResource(""),
				dst.formatResource(""),
			)

		default:
			return fmt.Sprintf("end id=%d:%d %s duration=%dµs response-length=%dB%s%s",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				ev.ResponseEnd.GetSinceResponseInit().GetNanos()/1000,
				ev.ResponseEnd.GetResponseBytes(),
				src.formatResource(""),
				dst.formatResource(""),
			)
		}

	default:
		return fmt.Sprintf("unknown %s", flow)
	}
}
