package tap

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	apiUtil "github.com/runconduit/conduit/controller/api/util"
	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/controller/tap"
	proxy "github.com/runconduit/conduit/controller/gen/proxy/tap"
	public "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/controller/util"
	pkgK8s "github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apiv1 "k8s.io/api/core/v1"
)

type (
	server struct {
		tapPort uint
		lister  *k8s.Lister
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

	objects, err := s.lister.GetObjects(req.Target.Resource.Namespace, req.Target.Resource.Type, req.Target.Resource.Name)
	if err != nil {
		return apiUtil.GRPCError(err)
	}

	pods := []*apiv1.Pod{}
	for _, object := range objects {
		podsFor, err := s.lister.GetPodsFor(object, false)
		if err != nil {
			return apiUtil.GRPCError(err)
		}

		pods = append(pods, podsFor...)
	}

	if len(pods) == 0 {
		return status.Errorf(codes.NotFound, "no pods found for ResourceSelection: %+v", *req.Target)
	}

	log.Infof("Tapping %d pods for target: %+v", len(pods), *req.Target.Resource)

	events := make(chan *common.TapEvent)

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
func parseScheme(scheme string) *common.Scheme {
	value, ok := common.Scheme_Registered_value[strings.ToUpper(scheme)]
	if ok {
		return &common.Scheme{
			Type: &common.Scheme_Registered_{
				Registered: common.Scheme_Registered(value),
			},
		}
	}
	return &common.Scheme{
		Type: &common.Scheme_Unregistered{
			Unregistered: strings.ToUpper(scheme),
		},
	}
}

// TODO: validate method
func parseMethod(method string) *common.HttpMethod {
	value, ok := common.HttpMethod_Registered_value[strings.ToUpper(method)]
	if ok {
		return &common.HttpMethod{
			Type: &common.HttpMethod_Registered_{
				Registered: common.HttpMethod_Registered(value),
			},
		}
	}
	return &common.HttpMethod{
		Type: &common.HttpMethod_Unregistered{
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
		dstLabels[pkgK8s.ResourceTypesToProxyLabels[resource.Type]] = resource.Name
	}
	if resource.Type != pkgK8s.Namespaces && resource.Namespace != "" {
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
func (s *server) tapProxy(ctx context.Context, maxRps float32, match *proxy.ObserveRequest_Match, addr string, events chan *common.TapEvent) {
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
			events <- event
		}
		if time.Now().Before(windowEnd) {
			time.Sleep(time.Until(windowEnd))
		}
	}
}

// NewServer creates a new gRPC Tap server
func NewServer(
	addr string,
	tapPort uint,
	lister *k8s.Lister,
) (*grpc.Server, net.Listener, error) {

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	s := util.NewGrpcServer()
	srv := server{
		tapPort: tapPort,
		lister:  lister,
	}
	pb.RegisterTapServer(s, &srv)

	return s, lis, nil
}
