package util

import (
	"errors"
	"fmt"
	"strings"
	"time"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/addr"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
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
		k8s.Authority,
		k8s.Deployment,
		k8s.Namespace,
		k8s.Pod,
		k8s.ReplicationController,
	}

	// ValidTapDestinations specifies resource types allowed as a tap destination:
	// destination resource on an outbound 'to' query
	ValidTapDestinations = []string{
		k8s.Deployment,
		k8s.Job,
		k8s.Namespace,
		k8s.Pod,
		k8s.ReplicationController,
		k8s.Service,
	}
)

// Parameters that are used to build requests for metrics data.  This includes
// requests to StatSummary and TopRoutes
type StatsBaseRequestParams struct {
	TimeWindow    string
	Namespace     string
	ResourceType  string
	ResourceName  string
	AllNamespaces bool
}

type StatsSummaryRequestParams struct {
	StatsBaseRequestParams
	ToNamespace   string
	ToType        string
	ToName        string
	FromNamespace string
	FromType      string
	FromName      string
	SkipStats     bool
}

type TopRoutesRequestParams struct {
	StatsBaseRequestParams
	To    string
	ToAll bool
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
		SkipStats:  p.SkipStats,
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
		targetNamespace = v1.NamespaceDefault
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

	if p.To != "" && p.ToAll {
		return nil, errors.New("ToService and ToAll are mutually exclusive")
	}

	if p.To != "" {
		topRoutesRequest.Outbound = &pb.TopRoutesRequest_ToAuthority{
			ToAuthority: p.To,
		}
	}

	if p.ToAll {
		topRoutesRequest.Outbound = &pb.TopRoutesRequest_ToAll{
			ToAll: &pb.Empty{},
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
	if err := validateResources(resType, args); err != nil {
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

func validateResources(resType string, args []string) error {
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

// formatAddr formats the peer's TCP address for the `src` or `dst` element in
// the tap output corresponding to this peer.
func (p *peer) formatAddr() string {
	return fmt.Sprintf(
		"%s=%s",
		p.direction,
		addr.PublicAddressToString(p.address),
	)
}

// formatResource returns a label describing what Kubernetes resources the peer
// belongs to. If the peer belongs to a resource of kind `resourceKind`, it will
// return a label for that resource; otherwise, it will fall back to the peer's
// pod name. Additionally, if the resource is not of type `namespace`, it will
// also add a label describing the peer's resource.
func (p *peer) formatResource(resourceKind string) string {
	var s string
	if resourceName, exists := p.labels[resourceKind]; exists {
		kind := resourceKind
		if short := k8s.ShortNameFromCanonicalResourceName(resourceKind); short != "" {
			kind = short
		}
		s = fmt.Sprintf(
			" %s_res=%s/%s",
			p.direction,
			kind,
			resourceName,
		)
	} else if pod, hasPod := p.labels[k8s.Pod]; hasPod {
		s = fmt.Sprintf(" %s_pod=%s", p.direction, pod)
	}
	if resourceKind != k8s.Namespace {
		if ns, hasNs := p.labels[k8s.Namespace]; hasNs {
			s += fmt.Sprintf(" %s_ns=%s", p.direction, ns)
		}
	}
	return s
}

func (p *peer) tlsStatus() string {
	return p.labels["tls"]
}

func routeLabels(event *pb.TapEvent) string {
	out := ""
	for key, val := range event.GetRouteMeta().GetLabels() {
		out = fmt.Sprintf("%s rt_%s=%s", out, key, val)
	}

	return out
}

func RenderTapEvent(event *pb.TapEvent, resource string) string {
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
		src.formatAddr(),
		dst.formatAddr(),
		tls,
	)

	// If `resource` is non-empty, then
	resources := ""
	if resource != "" {
		resources = fmt.Sprintf(
			"%s%s%s",
			src.formatResource(resource),
			dst.formatResource(resource),
			routeLabels(event),
		)
	}

	switch ev := event.GetHttp().GetEvent().(type) {
	case *pb.TapEvent_Http_RequestInit_:
		return fmt.Sprintf("req id=%d:%d %s :method=%s :authority=%s :path=%s%s",
			ev.RequestInit.GetId().GetBase(),
			ev.RequestInit.GetId().GetStream(),
			flow,
			ev.RequestInit.GetMethod().GetRegistered().String(),
			ev.RequestInit.GetAuthority(),
			ev.RequestInit.GetPath(),
			resources,
		)

	case *pb.TapEvent_Http_ResponseInit_:
		return fmt.Sprintf("rsp id=%d:%d %s :status=%d latency=%dµs%s",
			ev.ResponseInit.GetId().GetBase(),
			ev.ResponseInit.GetId().GetStream(),
			flow,
			ev.ResponseInit.GetHttpStatus(),
			ev.ResponseInit.GetSinceRequestInit().GetNanos()/1000,
			resources,
		)

	case *pb.TapEvent_Http_ResponseEnd_:
		switch eos := ev.ResponseEnd.GetEos().GetEnd().(type) {
		case *pb.Eos_GrpcStatusCode:
			return fmt.Sprintf(
				"end id=%d:%d %s grpc-status=%s duration=%dµs response-length=%dB%s",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				codes.Code(eos.GrpcStatusCode),
				ev.ResponseEnd.GetSinceResponseInit().GetNanos()/1000,
				ev.ResponseEnd.GetResponseBytes(),
				resources,
			)

		case *pb.Eos_ResetErrorCode:
			return fmt.Sprintf(
				"end id=%d:%d %s reset-error=%+v duration=%dµs response-length=%dB%s",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				eos.ResetErrorCode,
				ev.ResponseEnd.GetSinceResponseInit().GetNanos()/1000,
				ev.ResponseEnd.GetResponseBytes(),
				resources,
			)

		default:
			return fmt.Sprintf("end id=%d:%d %s duration=%dµs response-length=%dB%s",
				ev.ResponseEnd.GetId().GetBase(),
				ev.ResponseEnd.GetId().GetStream(),
				flow,
				ev.ResponseEnd.GetSinceResponseInit().GetNanos()/1000,
				ev.ResponseEnd.GetResponseBytes(),
				resources,
			)
		}

	default:
		return fmt.Sprintf("unknown %s", flow)
	}
}

func GetRequestRate(stats *pb.BasicStats, timeWindow string) float64 {
	success := stats.SuccessCount
	failure := stats.FailureCount
	windowLength, err := time.ParseDuration(timeWindow)
	if err != nil {
		log.Error(err.Error())
		return 0.0
	}
	return float64(success+failure) / windowLength.Seconds()
}

func GetSuccessRate(stats *pb.BasicStats) float64 {
	success := stats.SuccessCount
	failure := stats.FailureCount

	if success+failure == 0 {
		return 0.0
	}
	return float64(success) / float64(success+failure)
}

func GetPercentTLS(stats *pb.BasicStats) float64 {
	reqTotal := stats.SuccessCount + stats.FailureCount
	if reqTotal == 0 {
		return 0.0
	}
	return float64(stats.TlsRequestCount) / float64(reqTotal)
}
