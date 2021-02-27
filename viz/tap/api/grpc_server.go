package api

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/labels"

	httpPb "github.com/linkerd/linkerd2-proxy-api/go/http_types"
	proxy "github.com/linkerd/linkerd2-proxy-api/go/tap"
	apiUtil "github.com/linkerd/linkerd2/controller/api/util"
	netPb "github.com/linkerd/linkerd2/controller/gen/common/net"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/util"
	metricsPb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	vizLabels "github.com/linkerd/linkerd2/viz/pkg/labels"
	tapPb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

const ipIndex = "ip"
const defaultMaxRps = 100.0

// GRPCTapServer describes the gRPC server implementing pb.TapServer
type GRPCTapServer struct {
	tapPort             uint
	k8sAPI              *k8s.API
	controllerNamespace string
	trustDomain         string
}

var (
	tapInterval = 1 * time.Second
)

// Tap is deprecated, use TapByResource.
func (s *GRPCTapServer) Tap(req *tapPb.TapRequest, stream tapPb.Tap_TapServer) error {
	return status.Error(codes.Unimplemented, "Tap is deprecated, use TapByResource")
}

// TapByResource taps all resources matched by the request object.
func (s *GRPCTapServer) TapByResource(req *tapPb.TapByResourceRequest, stream tapPb.Tap_TapByResourceServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "TapByResource received nil TapByResourceRequest")
	}
	if req.GetTarget() == nil {
		return status.Error(codes.InvalidArgument, "TapByResource received nil target ResourceSelection")
	}
	res := req.GetTarget().GetResource()
	labelSelector, err := getLabelSelector(req)
	if err != nil {
		return err
	}
	if res == nil {
		return status.Error(codes.InvalidArgument, "TapByResource received nil target Resource")
	}
	if req.GetMaxRps() == 0.0 {
		req.MaxRps = defaultMaxRps
	}

	objects, err := s.k8sAPI.GetObjects(res.GetNamespace(), res.GetType(), res.GetName(), labelSelector)
	if err != nil {
		return apiUtil.GRPCError(err)
	}

	pods := []*corev1.Pod{}
	tapDisabled := []*corev1.Pod{}
	tapNotEnabled := []*corev1.Pod{}
	for _, object := range objects {
		podsFor, err := s.k8sAPI.GetPodsFor(object, false)
		if err != nil {
			return apiUtil.GRPCError(err)
		}

		for _, pod := range podsFor {
			if pkgK8s.IsMeshed(pod, s.controllerNamespace) {
				if vizLabels.IsTapDisabled(pod) {
					tapDisabled = append(tapDisabled, pod)
				} else if !vizLabels.IsTapEnabled(pod) {
					tapNotEnabled = append(tapNotEnabled, pod)
				} else {
					pods = append(pods, pod)
				}
			}
		}
	}

	if len(pods) == 0 {
		var errs strings.Builder
		fmt.Fprintf(&errs, "no pods to tap for %s/%s\n", res.GetType(), res.GetName())
		if len(tapDisabled) > 0 {
			fmt.Fprintf(&errs, "%d pods found with tap disabled via the %s annotation:\n", len(tapDisabled), vizLabels.VizTapDisabled)
			for _, pod := range tapDisabled {
				fmt.Fprintf(&errs, "\t* %s\n", pod.Name)
			}
			fmt.Fprintln(&errs, "remove this annotation to make these pods valid tap targets")
		}
		if len(tapNotEnabled) > 0 {
			fmt.Fprintf(&errs, "%d pods found with tap not enabled:\n", len(tapNotEnabled))
			for _, pod := range tapNotEnabled {
				fmt.Fprintf(&errs, "\t* %s\n", pod.Name)
			}
			fmt.Fprintln(&errs, "restart these pods to enable tap and make them valid tap targets")
		}
		return status.Errorf(codes.NotFound, errs.String())
	}

	log.Infof("Tapping %d pods for target: %s", len(pods), res.String())

	events := make(chan *tapPb.TapEvent)

	// divide the rps evenly between all pods to tap
	rpsPerPod := req.GetMaxRps() / float32(len(pods))
	if rpsPerPod < 1 {
		rpsPerPod = 1
	}

	match, err := makeByResourceMatch(req.GetMatch())
	if err != nil {
		return apiUtil.GRPCError(err)
	}

	extract := &proxy.ObserveRequest_Extract{}

	// HTTP is the only protocol supported for extracting metadata, so this is
	// the only field checked.
	extractHTTP := req.GetExtract().GetHttp()
	if extractHTTP != nil {
		extract = buildExtractHTTP(extractHTTP)
	}

	for _, pod := range pods {
		// create the expected pod identity from the pod spec
		ns := res.GetNamespace()
		if res.GetType() == pkgK8s.Namespace {
			ns = res.GetName()
		}
		name := fmt.Sprintf("%s.%s.serviceaccount.identity.%s.%s", pod.Spec.ServiceAccountName, ns, s.controllerNamespace, s.trustDomain)
		log.Debugf("initiating tap request to %s with required name %s", pod.Spec.ServiceAccountName, name)

		// pass the header metadata into the request context
		ctx := stream.Context()
		ctx = metadata.AppendToOutgoingContext(ctx, pkgK8s.RequireIDHeader, name)

		// initiate a tap on the pod
		go s.tapProxy(ctx, rpsPerPod, match, extract, pod.Status.PodIP, events)
	}

	// read events from the taps and send them back
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case event := <-events:
			err := stream.Send(event)
			if err != nil {
				return apiUtil.GRPCError(err)
			}
		}
	}
}

func makeByResourceMatch(match *tapPb.TapByResourceRequest_Match) (*proxy.ObserveRequest_Match, error) {
	// TODO: for now assume it's always a single, flat `All` match list
	seq := match.GetAll()
	if seq == nil {
		return nil, status.Errorf(codes.Unimplemented, "unexpected match specified: %+v", match)
	}

	matches := []*proxy.ObserveRequest_Match{}

	for _, reqMatch := range seq.Matches {
		switch typed := reqMatch.Match.(type) {
		case *tapPb.TapByResourceRequest_Match_Destinations:

			for k, v := range destinationLabels(typed.Destinations.Resource) {
				matches = append(matches, &proxy.ObserveRequest_Match{
					Match: &proxy.ObserveRequest_Match_DestinationLabel{
						DestinationLabel: &proxy.ObserveRequest_Match_Label{
							Key:   k,
							Value: v,
						},
					},
				})
			}

		case *tapPb.TapByResourceRequest_Match_Http_:

			httpMatch := proxy.ObserveRequest_Match_Http{}

			switch httpTyped := typed.Http.Match.(type) {
			case *tapPb.TapByResourceRequest_Match_Http_Scheme:
				httpMatch = proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Scheme{
						Scheme: util.ParseScheme(httpTyped.Scheme),
					},
				}
			case *tapPb.TapByResourceRequest_Match_Http_Method:
				httpMatch = proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Method{
						Method: util.ParseMethod(httpTyped.Method),
					},
				}
			case *tapPb.TapByResourceRequest_Match_Http_Authority:
				httpMatch = proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Authority{
						Authority: &proxy.ObserveRequest_Match_Http_StringMatch{
							Match: &proxy.ObserveRequest_Match_Http_StringMatch_Exact{
								Exact: httpTyped.Authority,
							},
						},
					},
				}
			case *tapPb.TapByResourceRequest_Match_Http_Path:
				httpMatch = proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Path{
						Path: &proxy.ObserveRequest_Match_Http_StringMatch{
							Match: &proxy.ObserveRequest_Match_Http_StringMatch_Prefix{
								Prefix: httpTyped.Path,
							},
						},
					},
				}
			default:
				return nil, status.Errorf(codes.Unimplemented, "unknown HTTP match type: %v", httpTyped)
			}

			matches = append(matches, &proxy.ObserveRequest_Match{
				Match: &proxy.ObserveRequest_Match_Http_{
					Http: &httpMatch,
				},
			})

		default:
			return nil, status.Errorf(codes.Unimplemented, "unknown match type: %v", typed)
		}
	}

	return &proxy.ObserveRequest_Match{
		Match: &proxy.ObserveRequest_Match_All{
			All: &proxy.ObserveRequest_Match_Seq{
				Matches: matches,
			},
		},
	}, nil
}

// TODO: factor out with `promLabels` in public-api
func destinationLabels(resource *metricsPb.Resource) map[string]string {
	dstLabels := map[string]string{}
	if resource.Name != "" {
		l5dLabel := pkgK8s.KindToL5DLabel(resource.Type)
		dstLabels[l5dLabel] = resource.Name
	}
	if resource.Type != pkgK8s.Namespace && resource.Namespace != "" {
		dstLabels["namespace"] = resource.Namespace
	}
	return dstLabels
}

func buildExtractHTTP(extract *tapPb.TapByResourceRequest_Extract_Http) *proxy.ObserveRequest_Extract {
	if extract.GetHeaders() != nil {
		return &proxy.ObserveRequest_Extract{
			Extract: &proxy.ObserveRequest_Extract_Http_{
				Http: &proxy.ObserveRequest_Extract_Http{
					Extract: &proxy.ObserveRequest_Extract_Http_Headers_{
						Headers: &proxy.ObserveRequest_Extract_Http_Headers{},
					},
				},
			},
		}
	}
	return nil
}

// Tap a pod.
// This method will run continuously until an error is encountered or the
// request is cancelled via the context.  Thus it should be called as a
// go-routine.
// To limit the rps to maxRps, this method calls Observe on the pod with a limit
// of maxRps * 1s at most once per 1s window.  If this limit is reached in
// less than 1s, we sleep until the end of the window before calling Observe
// again.
func (s *GRPCTapServer) tapProxy(ctx context.Context, maxRps float32, match *proxy.ObserveRequest_Match, extract *proxy.ObserveRequest_Extract, addr string, events chan *tapPb.TapEvent) {
	tapAddr := fmt.Sprintf("%s:%d", addr, s.tapPort)
	log.Infof("Establishing tap on %s", tapAddr)
	conn, err := grpc.DialContext(ctx, tapAddr, grpc.WithInsecure())
	if err != nil {
		log.Error(err)
		return
	}
	client := proxy.NewTapClient(conn)
	defer conn.Close()

	req := &proxy.ObserveRequest{
		Limit:   uint32(maxRps * float32(tapInterval.Seconds())),
		Match:   match,
		Extract: extract,
	}

	for { // Request loop
		windowStart := time.Now()
		windowEnd := windowStart.Add(tapInterval)
		rsp, err := client.Observe(ctx, req)
		if err != nil {
			log.Error(err)
			return
		}
		for { // Stream loop
			event, err := rsp.Recv()
			if err == io.EOF {
				log.Debugf("[%s] proxy terminated the stream", addr)
				break
			}
			if err != nil {
				log.Errorf("[%s] encountered an error: %s", addr, err)
				return
			}

			translatedEvent := s.translateEvent(ctx, event)

			select {
			case <-ctx.Done():
				log.Debugf("[%s] client terminated the stream", addr)
				return
			default:
				events <- translatedEvent
			}
		}
		if time.Now().Before(windowEnd) {
			time.Sleep(time.Until(windowEnd))
		}
	}
}

func (s *GRPCTapServer) translateEvent(ctx context.Context, orig *proxy.TapEvent) *tapPb.TapEvent {
	direction := func(orig proxy.TapEvent_ProxyDirection) tapPb.TapEvent_ProxyDirection {
		switch orig {
		case proxy.TapEvent_INBOUND:
			return tapPb.TapEvent_INBOUND
		case proxy.TapEvent_OUTBOUND:
			return tapPb.TapEvent_OUTBOUND
		default:
			return tapPb.TapEvent_UNKNOWN
		}
	}

	event := func(orig *proxy.TapEvent_Http) *tapPb.TapEvent_Http_ {
		id := func(orig *proxy.TapEvent_Http_StreamId) *tapPb.TapEvent_Http_StreamId {
			return &tapPb.TapEvent_Http_StreamId{
				Base:   orig.GetBase(),
				Stream: orig.GetStream(),
			}
		}

		method := func(orig *httpPb.HttpMethod) *metricsPb.HttpMethod {
			switch m := orig.GetType().(type) {
			case *httpPb.HttpMethod_Registered_:
				return &metricsPb.HttpMethod{
					Type: &metricsPb.HttpMethod_Registered_{
						Registered: metricsPb.HttpMethod_Registered(m.Registered),
					},
				}
			case *httpPb.HttpMethod_Unregistered:
				return &metricsPb.HttpMethod{
					Type: &metricsPb.HttpMethod_Unregistered{
						Unregistered: m.Unregistered,
					},
				}
			default:
				return nil
			}
		}

		scheme := func(orig *httpPb.Scheme) *metricsPb.Scheme {
			switch s := orig.GetType().(type) {
			case *httpPb.Scheme_Registered_:
				return &metricsPb.Scheme{
					Type: &metricsPb.Scheme_Registered_{
						Registered: metricsPb.Scheme_Registered(s.Registered),
					},
				}
			case *httpPb.Scheme_Unregistered:
				return &metricsPb.Scheme{
					Type: &metricsPb.Scheme_Unregistered{
						Unregistered: s.Unregistered,
					},
				}
			default:
				return nil
			}
		}

		headers := func(orig *httpPb.Headers) *metricsPb.Headers {
			if orig == nil {
				return nil
			}
			var headers []*metricsPb.Headers_Header
			for _, header := range orig.GetHeaders() {
				n := header.GetName()
				b := header.GetValue()
				h := metricsPb.Headers_Header{Name: n, Value: &metricsPb.Headers_Header_ValueBin{ValueBin: b}}
				if utf8.Valid(b) {
					h = metricsPb.Headers_Header{Name: n, Value: &metricsPb.Headers_Header_ValueStr{ValueStr: string(b)}}
				}
				headers = append(headers, &h)
			}
			return &metricsPb.Headers{
				Headers: headers,
			}
		}

		switch orig := orig.GetEvent().(type) {
		case *proxy.TapEvent_Http_RequestInit_:
			return &tapPb.TapEvent_Http_{
				Http: &tapPb.TapEvent_Http{
					Event: &tapPb.TapEvent_Http_RequestInit_{
						RequestInit: &tapPb.TapEvent_Http_RequestInit{
							Id:        id(orig.RequestInit.GetId()),
							Method:    method(orig.RequestInit.GetMethod()),
							Scheme:    scheme(orig.RequestInit.GetScheme()),
							Authority: orig.RequestInit.Authority,
							Path:      orig.RequestInit.Path,
							Headers:   headers(orig.RequestInit.GetHeaders()),
						},
					},
				},
			}

		case *proxy.TapEvent_Http_ResponseInit_:
			return &tapPb.TapEvent_Http_{
				Http: &tapPb.TapEvent_Http{
					Event: &tapPb.TapEvent_Http_ResponseInit_{
						ResponseInit: &tapPb.TapEvent_Http_ResponseInit{
							Id:               id(orig.ResponseInit.GetId()),
							SinceRequestInit: orig.ResponseInit.GetSinceRequestInit(),
							HttpStatus:       orig.ResponseInit.GetHttpStatus(),
							Headers:          headers(orig.ResponseInit.GetHeaders()),
						},
					},
				},
			}

		case *proxy.TapEvent_Http_ResponseEnd_:
			eos := func(orig *proxy.Eos) *metricsPb.Eos {
				switch e := orig.GetEnd().(type) {
				case *proxy.Eos_ResetErrorCode:
					return &metricsPb.Eos{
						End: &metricsPb.Eos_ResetErrorCode{
							ResetErrorCode: e.ResetErrorCode,
						},
					}
				case *proxy.Eos_GrpcStatusCode:
					return &metricsPb.Eos{
						End: &metricsPb.Eos_GrpcStatusCode{
							GrpcStatusCode: e.GrpcStatusCode,
						},
					}
				default:
					return nil
				}
			}

			return &tapPb.TapEvent_Http_{
				Http: &tapPb.TapEvent_Http{
					Event: &tapPb.TapEvent_Http_ResponseEnd_{
						ResponseEnd: &tapPb.TapEvent_Http_ResponseEnd{
							Id:                id(orig.ResponseEnd.GetId()),
							SinceRequestInit:  orig.ResponseEnd.GetSinceRequestInit(),
							SinceResponseInit: orig.ResponseEnd.GetSinceResponseInit(),
							ResponseBytes:     orig.ResponseEnd.GetResponseBytes(),
							Eos:               eos(orig.ResponseEnd.GetEos()),
							Trailers:          headers(orig.ResponseEnd.GetTrailers()),
						},
					},
				},
			}

		default:
			return nil
		}
	}

	sourceLabels := orig.GetSourceMeta().GetLabels()
	if sourceLabels == nil {
		sourceLabels = make(map[string]string)
	}
	destinationLabels := orig.GetDestinationMeta().GetLabels()
	if destinationLabels == nil {
		destinationLabels = make(map[string]string)
	}

	ev := &tapPb.TapEvent{
		Source: addr.NetToPublic(orig.GetSource()),
		SourceMeta: &tapPb.TapEvent_EndpointMeta{
			Labels: sourceLabels,
		},
		Destination: addr.NetToPublic(orig.GetDestination()),
		DestinationMeta: &tapPb.TapEvent_EndpointMeta{
			Labels: destinationLabels,
		},
		RouteMeta: &tapPb.TapEvent_RouteMeta{
			Labels: orig.GetRouteMeta().GetLabels(),
		},
		ProxyDirection: direction(orig.GetProxyDirection()),
		Event:          event(orig.GetHttp()),
	}

	s.hydrateEventLabels(ctx, ev)

	return ev
}

// NewGrpcTapServer creates a new gRPC Tap server
func NewGrpcTapServer(
	tapPort uint,
	controllerNamespace string,
	trustDomain string,
	k8sAPI *k8s.API,
) *GRPCTapServer {
	k8sAPI.Pod().Informer().AddIndexers(cache.Indexers{ipIndex: indexByIP})
	k8sAPI.Node().Informer().AddIndexers(cache.Indexers{ipIndex: indexByIP})

	return newGRPCTapServer(tapPort, controllerNamespace, trustDomain, k8sAPI)
}

func newGRPCTapServer(
	tapPort uint,
	controllerNamespace string,
	trustDomain string,
	k8sAPI *k8s.API,
) *GRPCTapServer {
	srv := &GRPCTapServer{
		tapPort:             tapPort,
		k8sAPI:              k8sAPI,
		controllerNamespace: controllerNamespace,
		trustDomain:         trustDomain,
	}

	s := prometheus.NewGrpcServer()
	tapPb.RegisterTapServer(s, srv)

	return srv
}

func indexByIP(obj interface{}) ([]string, error) {
	switch v := obj.(type) {
	case *corev1.Pod:
		return []string{v.Status.PodIP}, nil
	case *corev1.Node:
		addresses := make([]string, 0)
		for _, address := range v.Status.Addresses {
			if address.Type == corev1.NodeInternalIP {
				log.Debugf("Indexing node address: %s", address.Address)
				addresses = append(addresses, address.Address)
			}
		}
		return addresses, nil
	}
	return []string{""}, fmt.Errorf("object is not a pod nor a node")
}

// hydrateEventLabels attempts to hydrate the metadata labels for an event's
// source and (if the event was reported by an inbound proxy) destination,
// and adds them to the event's `SourceMeta` and `DestinationMeta` fields.
//
// Since errors encountered while hydrating metadata are non-fatal and result
// only in missing labels, any errors are logged at the WARN level.
func (s *GRPCTapServer) hydrateEventLabels(ctx context.Context, ev *tapPb.TapEvent) {
	err := s.hydrateIPLabels(ctx, ev.GetSource().GetIp(), ev.GetSourceMeta().GetLabels())
	if err != nil {
		log.Warnf("error hydrating source labels: %s", err)
	}

	if ev.ProxyDirection == tapPb.TapEvent_INBOUND {
		// Events emitted by an inbound proxies don't have destination labels,
		// since the inbound proxy _is_ the destination, and proxies don't know
		// their own labels.
		err = s.hydrateIPLabels(ctx, ev.GetDestination().GetIp(), ev.GetDestinationMeta().GetLabels())
		if err != nil {
			log.Warnf("error hydrating destination labels: %s", err)
		}
	}

}

// hydrateIPMeta attempts to determine the metadata labels for `ip` and, if
// successful, adds them to `labels`.
func (s *GRPCTapServer) hydrateIPLabels(ctx context.Context, ip *netPb.IPAddress, labels map[string]string) error {
	res, err := s.resourceForIP(ip)
	if err != nil {
		return err
	}

	switch v := res.(type) {
	case *corev1.Pod:
		if v == nil {
			log.Debugf("no pod found for IP %s", addr.PublicIPToString(ip))
			return nil
		}
		ownerKind, ownerName := s.k8sAPI.GetOwnerKindAndName(ctx, v, false)
		podLabels := pkgK8s.GetPodLabels(ownerKind, ownerName, v)
		for key, value := range podLabels {
			labels[key] = value
		}
		labels[pkgK8s.Namespace] = v.Namespace
	case *corev1.Node:
		labels[pkgK8s.Node] = v.Name
	}
	return nil
}

// resourceForIP returns the node or pod corresponding to a given IP address.
//
// First it checks if the IP corresponds to a Node's internal IP and returns the
// node if that's the case. Otherwise it checks the running pods that match the
// IP. If exactly one is found, it's returned. Otherwise it returns nil. Errors
// are returned only in the event of an error searching the indices.
func (s *GRPCTapServer) resourceForIP(ip *netPb.IPAddress) (runtime.Object, error) {
	ipStr := addr.PublicIPToString(ip)

	nodes, err := s.k8sAPI.Node().Informer().GetIndexer().ByIndex(ipIndex, ipStr)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 1 {
		log.Debugf("found one node at IP %s", ipStr)
		return nodes[0].(*corev1.Node), nil
	}

	pods, err := s.k8sAPI.Pod().Informer().GetIndexer().ByIndex(ipIndex, ipStr)
	if err != nil {
		return nil, err
	}

	if len(pods) == 1 {
		log.Debugf("found one pod at IP %s", ipStr)
		return pods[0].(*corev1.Pod), nil
	}

	var singleRunningPod *corev1.Pod
	for _, obj := range pods {
		pod := obj.(*corev1.Pod)
		if pod.Status.Phase == corev1.PodRunning {
			if singleRunningPod != nil {
				log.Warnf(
					"could not uniquely identify pod at %s (found %d pods)",
					ipStr,
					len(pods),
				)
				return nil, nil
			}
			singleRunningPod = pod
		}
	}

	return singleRunningPod, nil
}

func getLabelSelector(req *tapPb.TapByResourceRequest) (labels.Selector, error) {
	labelSelector := labels.Everything()
	if s := req.GetTarget().GetLabelSelector(); s != "" {
		var err error
		labelSelector, err = labels.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector \"%s\": %s", s, err)
		}
	}
	return labelSelector, nil
}
