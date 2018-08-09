package tap

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	netpb "github.com/linkerd/linkerd2-proxy-api/go/net"
	proxy "github.com/linkerd/linkerd2-proxy-api/go/tap"
	apiUtil "github.com/linkerd/linkerd2/controller/api/util"
	pb "github.com/linkerd/linkerd2/controller/gen/controller/tap"
	public "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

const podIPIndex = "ip"

type (
	server struct {
		tapPort uint
		k8sAPI  *k8s.API
	}
)

var (
	tapInterval = 10 * time.Second
)

func (s *server) Tap(req *public.TapRequest, stream pb.Tap_TapServer) error {
	return status.Error(codes.Unimplemented, "Tap is deprecated, use TapByResource")
}

func (s *server) TapByResource(req *public.TapByResourceRequest, stream pb.Tap_TapByResourceServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "TapByResource received nil TapByResourceRequest")
	}
	if req.Target == nil {
		return status.Errorf(codes.InvalidArgument, "TapByResource received nil target ResourceSelection: %+v", *req)
	}

	objects, err := s.k8sAPI.GetObjects(req.Target.Resource.Namespace, req.Target.Resource.Type, req.Target.Resource.Name)
	if err != nil {
		return apiUtil.GRPCError(err)
	}

	pods := []*apiv1.Pod{}
	for _, object := range objects {
		podsFor, err := s.k8sAPI.GetPodsFor(object, false)
		if err != nil {
			return apiUtil.GRPCError(err)
		}

		pods = append(pods, podsFor...)
	}

	if len(pods) == 0 {
		return status.Errorf(codes.NotFound, "no pods found for ResourceSelection: %+v", *req.Target)
	}

	log.Infof("Tapping %d pods for target: %+v", len(pods), *req.Target.Resource)

	events := make(chan *public.TapEvent)

	go func() { // Stop sending back events if the request is cancelled
		<-stream.Context().Done()
		close(events)
	}()

	// divide the rps evenly between all pods to tap
	rpsPerPod := req.MaxRps / float32(len(pods))
	if rpsPerPod < 1 {
		rpsPerPod = 1
	}

	match, err := makeByResourceMatch(req.Match)
	if err != nil {
		return apiUtil.GRPCError(err)
	}

	for _, pod := range pods {
		// initiate a tap on the pod
		go s.tapProxy(stream.Context(), rpsPerPod, match, pod.Status.PodIP, events)
	}

	// read events from the taps and send them back
	for event := range events {
		err := stream.Send(event)
		if err != nil {
			return apiUtil.GRPCError(err)
		}
	}
	return nil
}

// TODO: validate scheme
func parseScheme(scheme string) *proxy.Scheme {
	value, ok := proxy.Scheme_Registered_value[strings.ToUpper(scheme)]
	if ok {
		return &proxy.Scheme{
			Type: &proxy.Scheme_Registered_{
				Registered: proxy.Scheme_Registered(value),
			},
		}
	}
	return &proxy.Scheme{
		Type: &proxy.Scheme_Unregistered{
			Unregistered: strings.ToUpper(scheme),
		},
	}
}

// TODO: validate method
func parseMethod(method string) *proxy.HttpMethod {
	value, ok := proxy.HttpMethod_Registered_value[strings.ToUpper(method)]
	if ok {
		return &proxy.HttpMethod{
			Type: &proxy.HttpMethod_Registered_{
				Registered: proxy.HttpMethod_Registered(value),
			},
		}
	}
	return &proxy.HttpMethod{
		Type: &proxy.HttpMethod_Unregistered{
			Unregistered: strings.ToUpper(method),
		},
	}
}

func makeByResourceMatch(match *public.TapByResourceRequest_Match) (*proxy.ObserveRequest_Match, error) {
	// TODO: for now assume it's always a single, flat `All` match list
	seq := match.GetAll()
	if seq == nil {
		return nil, status.Errorf(codes.Unimplemented, "unexpected match specified: %+v", match)
	}

	matches := []*proxy.ObserveRequest_Match{}

	for _, reqMatch := range seq.Matches {
		switch typed := reqMatch.Match.(type) {
		case *public.TapByResourceRequest_Match_Destinations:

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

		case *public.TapByResourceRequest_Match_Http_:

			httpMatch := proxy.ObserveRequest_Match_Http{}

			switch httpTyped := typed.Http.Match.(type) {
			case *public.TapByResourceRequest_Match_Http_Scheme:
				httpMatch = proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Scheme{
						Scheme: parseScheme(httpTyped.Scheme),
					},
				}
			case *public.TapByResourceRequest_Match_Http_Method:
				httpMatch = proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Method{
						Method: parseMethod(httpTyped.Method),
					},
				}
			case *public.TapByResourceRequest_Match_Http_Authority:
				httpMatch = proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Authority{
						Authority: &proxy.ObserveRequest_Match_Http_StringMatch{
							Match: &proxy.ObserveRequest_Match_Http_StringMatch_Exact{
								Exact: httpTyped.Authority,
							},
						},
					},
				}
			case *public.TapByResourceRequest_Match_Http_Path:
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
func destinationLabels(resource *public.Resource) map[string]string {
	dstLabels := map[string]string{}
	if resource.Name != "" {
		dstLabels[resource.Type] = resource.Name
	}
	if resource.Type != pkgK8s.Namespace && resource.Namespace != "" {
		dstLabels["namespace"] = resource.Namespace
	}
	return dstLabels
}

// Tap a pod.
// This method will run continuously until an error is encountered or the
// request is cancelled via the context.  Thus it should be called as a
// go-routine.
// To limit the rps to maxRps, this method calls Observe on the pod with a limit
// of maxRps * 10s at most once per 10s window.  If this limit is reached in
// less than 10s, we sleep until the end of the window before calling Observe
// again.
func (s *server) tapProxy(ctx context.Context, maxRps float32, match *proxy.ObserveRequest_Match, addr string, events chan *public.TapEvent) {
	tapAddr := fmt.Sprintf("%s:%d", addr, s.tapPort)
	log.Infof("Establishing tap on %s", tapAddr)
	conn, err := grpc.DialContext(ctx, tapAddr, grpc.WithInsecure())
	if err != nil {
		log.Error(err)
		return
	}
	client := proxy.NewTapClient(conn)

	req := &proxy.ObserveRequest{
		Limit: uint32(maxRps * float32(tapInterval.Seconds())),
		Match: match,
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
				break
			}
			if err != nil {
				log.Error(err)
				return
			}
			events <- s.translateEvent(event)
		}
		if time.Now().Before(windowEnd) {
			time.Sleep(time.Until(windowEnd))
		}
	}
}

func (s *server) translateEvent(orig *proxy.TapEvent) *public.TapEvent {
	direction := func(orig proxy.TapEvent_ProxyDirection) public.TapEvent_ProxyDirection {
		switch orig {
		case proxy.TapEvent_INBOUND:
			return public.TapEvent_INBOUND
		case proxy.TapEvent_OUTBOUND:
			return public.TapEvent_OUTBOUND
		default:
			return public.TapEvent_UNKNOWN
		}
	}

	tcp := func(orig *netpb.TcpAddress) *public.TcpAddress {
		ip := func(orig *netpb.IPAddress) *public.IPAddress {
			switch i := orig.GetIp().(type) {
			case *netpb.IPAddress_Ipv6:
				return &public.IPAddress{
					Ip: &public.IPAddress_Ipv6{
						Ipv6: &public.IPv6{
							First: i.Ipv6.First,
							Last:  i.Ipv6.Last,
						},
					},
				}
			case *netpb.IPAddress_Ipv4:
				return &public.IPAddress{
					Ip: &public.IPAddress_Ipv4{
						Ipv4: i.Ipv4,
					},
				}
			default:
				return nil
			}
		}

		return &public.TcpAddress{
			Ip:   ip(orig.GetIp()),
			Port: orig.GetPort(),
		}
	}

	event := func(orig *proxy.TapEvent_Http) *public.TapEvent_Http_ {
		id := func(orig *proxy.TapEvent_Http_StreamId) *public.TapEvent_Http_StreamId {
			return &public.TapEvent_Http_StreamId{
				Base:   orig.GetBase(),
				Stream: orig.GetStream(),
			}
		}

		method := func(orig *proxy.HttpMethod) *public.HttpMethod {
			switch m := orig.GetType().(type) {
			case *proxy.HttpMethod_Registered_:
				return &public.HttpMethod{
					Type: &public.HttpMethod_Registered_{
						Registered: public.HttpMethod_Registered(m.Registered),
					},
				}
			case *proxy.HttpMethod_Unregistered:
				return &public.HttpMethod{
					Type: &public.HttpMethod_Unregistered{
						Unregistered: m.Unregistered,
					},
				}
			default:
				return nil
			}
		}

		scheme := func(orig *proxy.Scheme) *public.Scheme {
			switch s := orig.GetType().(type) {
			case *proxy.Scheme_Registered_:
				return &public.Scheme{
					Type: &public.Scheme_Registered_{
						Registered: public.Scheme_Registered(s.Registered),
					},
				}
			case *proxy.Scheme_Unregistered:
				return &public.Scheme{
					Type: &public.Scheme_Unregistered{
						Unregistered: s.Unregistered,
					},
				}
			default:
				return nil
			}
		}

		switch orig := orig.GetEvent().(type) {
		case *proxy.TapEvent_Http_RequestInit_:
			return &public.TapEvent_Http_{
				Http: &public.TapEvent_Http{
					Event: &public.TapEvent_Http_RequestInit_{
						RequestInit: &public.TapEvent_Http_RequestInit{
							Id:        id(orig.RequestInit.GetId()),
							Method:    method(orig.RequestInit.GetMethod()),
							Scheme:    scheme(orig.RequestInit.GetScheme()),
							Authority: orig.RequestInit.Authority,
							Path:      orig.RequestInit.Path,
						},
					},
				},
			}

		case *proxy.TapEvent_Http_ResponseInit_:
			return &public.TapEvent_Http_{
				Http: &public.TapEvent_Http{
					Event: &public.TapEvent_Http_ResponseInit_{
						ResponseInit: &public.TapEvent_Http_ResponseInit{
							Id:               id(orig.ResponseInit.GetId()),
							SinceRequestInit: orig.ResponseInit.GetSinceRequestInit(),
							HttpStatus:       orig.ResponseInit.GetHttpStatus(),
						},
					},
				},
			}

		case *proxy.TapEvent_Http_ResponseEnd_:
			eos := func(orig *proxy.Eos) *public.Eos {
				switch e := orig.GetEnd().(type) {
				case *proxy.Eos_ResetErrorCode:
					return &public.Eos{
						End: &public.Eos_ResetErrorCode{
							ResetErrorCode: e.ResetErrorCode,
						},
					}
				case *proxy.Eos_GrpcStatusCode:
					return &public.Eos{
						End: &public.Eos_GrpcStatusCode{
							GrpcStatusCode: e.GrpcStatusCode,
						},
					}
				default:
					return nil
				}
			}

			return &public.TapEvent_Http_{
				Http: &public.TapEvent_Http{
					Event: &public.TapEvent_Http_ResponseEnd_{
						ResponseEnd: &public.TapEvent_Http_ResponseEnd{
							Id:                id(orig.ResponseEnd.GetId()),
							SinceRequestInit:  orig.ResponseEnd.GetSinceRequestInit(),
							SinceResponseInit: orig.ResponseEnd.GetSinceResponseInit(),
							ResponseBytes:     orig.ResponseEnd.GetResponseBytes(),
							Eos:               eos(orig.ResponseEnd.GetEos()),
						},
					},
				},
			}

		default:
			return nil
		}
	}

	ev := &public.TapEvent{
		Source: tcp(orig.GetSource()),
		SourceMeta: &public.TapEvent_EndpointMeta{
			Labels: orig.GetSourceMeta().GetLabels(),
		},
		Destination: tcp(orig.GetDestination()),
		DestinationMeta: &public.TapEvent_EndpointMeta{
			Labels: orig.GetDestinationMeta().GetLabels(),
		},
		ProxyDirection: direction(orig.GetProxyDirection()),
		Event:          event(orig.GetHttp()),
	}

	sourceIPMeta, err := s.hydrateIPMeta(ev.Source.Ip)
	if err != nil {
		log.WithFields(log.Fields{
			"src":   ev.Source,
			"dst":   ev.Destination,
			"proxy": ev.ProxyDirection,
		}).Errorf("error hydrating source metadata: %s", err)
	} else {
		for k, v := range sourceIPMeta {
			ev.SourceMeta.Labels[k] = v
		}
	}

	return ev
}

// NewServer creates a new gRPC Tap server
func NewServer(
	addr string,
	tapPort uint,
	k8sAPI *k8s.API,
) (*grpc.Server, net.Listener, error) {
	k8sAPI.Pod().Informer().AddIndexers(cache.Indexers{podIPIndex: indexPodByIP})

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	s := prometheus.NewGrpcServer()
	srv := server{
		tapPort: tapPort,
		k8sAPI:  k8sAPI,
	}
	pb.RegisterTapServer(s, &srv)

	return s, lis, nil
}

func indexPodByIP(obj interface{}) ([]string, error) {
	if pod, ok := obj.(*apiv1.Pod); ok {
		return []string{pod.Status.PodIP}, nil
	}
	return []string{""}, fmt.Errorf("object is not a pod")
}

func (s *server) hydrateIPMeta(ip *public.IPAddress) (map[string]string, error) {
	pod, err := s.podForIP(ip)
	if err != nil {
		return nil, err
	}
	ownerKind, ownerName := s.k8sAPI.GetOwnerKindAndName(pod)
	return pkgK8s.GetPodLabels(ownerKind, ownerName, pod), nil
}

func (s *server) podForIP(ip *public.IPAddress) (*apiv1.Pod, error) {
	ipStr := ip.String()
	objs, err := s.k8sAPI.Pod().Informer().GetIndexer().ByIndex(podIPIndex, ipStr)
	if err != nil {
		return nil, err
	}

	// If there's a currently-running pod with this IP, use that. Otherwise,
	// we'll need to keep track of all the pods which _have_ had this IP, so
	// that we can use the most recently stopped one.
	var mostRecentlyStopped *apiv1.Pod
	stopTime := int64(0)
	for _, obj := range objs {
		pod, ok := obj.(*apiv1.Pod)
		if !ok {
			log.Errorf("found something that wasn't a pod when indexing pods by IP")
			continue
		}
		switch pod.Status.Phase {
		case apiv1.PodRunning:
			// Found a running pod with this IP --- it's that!
			return pod, nil
		case apiv1.PodFailed, apiv1.PodSucceeded:
			// The pod has stopped. It may have sent the request before it
			// stopped; so, see if it stopped more recently than any previously
			// observed stopped pods.
			if t := podStopTime(pod); t != nil && *t > stopTime {
				mostRecentlyStopped = pod
				stopTime = *t
			}
		default:
			// Otherwise, the pod's status is either pending (in which case it
			// can't have sent the request yet), or unknown. Skip it.
			continue
		}
	}
	// If we didn't find a running pod, choose the most recently stopped
	// pod that has had that IP.
	return mostRecentlyStopped, nil
}

func podStopTime(pod *apiv1.Pod) *int64 {
	status := pod.Status
	stopTime := int64(0)
	switch status.Phase {
	case apiv1.PodFailed, apiv1.PodSucceeded:
		for _, containerStatus := range status.ContainerStatuses {
			terminated := containerStatus.State.Terminated
			if terminated == nil {
				// A container in the pod has not stopped, so it
				// has no stop time.
				return nil
			}
			if t := terminated.FinishedAt.Unix(); t > stopTime {
				stopTime = t
			}
		}
	default:
		// The pod is not in the stopped state.
		return nil
	}
	return &stopTime
}
