package tap

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

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
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	apiv1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	applisters "k8s.io/client-go/listers/apps/v1beta2"
	corelisters "k8s.io/client-go/listers/core/v1"
)

type (
	server struct {
		tapPort uint
		// We use the Kubernetes API to find the IP addresses of pods to tap
		// TODO: remove these when TapByResource replaces tap
		replicaSets *k8s.ReplicaSetStore
		pods        k8s.PodIndex

		// TODO: factor out with public-api
		namespaceLister             corelisters.NamespaceLister
		deployLister                applisters.DeploymentLister
		replicaSetLister            applisters.ReplicaSetLister
		podLister                   corelisters.PodLister
		replicationControllerLister corelisters.ReplicationControllerLister
		serviceLister               corelisters.ServiceLister
	}
)

var (
	tapInterval = 10 * time.Second

	// TODO: factor out with public-api
	k8sResourceTypesToDestinationLabels = map[string]string{
		pkgK8s.KubernetesDeployments:            "deployment",
		pkgK8s.KubernetesNamespaces:             "namespace",
		pkgK8s.KubernetesPods:                   "pod",
		pkgK8s.KubernetesReplicationControllers: "replication_controller",
		pkgK8s.KubernetesServices:               "service",
	}
)

func (s *server) Tap(req *public.TapRequest, stream pb.Tap_TapServer) error {

	// TODO: Allow a configurable aperture A.
	//       If the target contains more than A pods, select A of them at random.
	var pods []*apiv1.Pod
	var targetName string
	switch target := req.Target.(type) {
	case *public.TapRequest_Pod:
		targetName = target.Pod
		pod, err := s.pods.GetPod(target.Pod)
		if err != nil {
			return status.Errorf(codes.NotFound, err.Error())
		}
		pods = []*apiv1.Pod{pod}
	case *public.TapRequest_Deployment:
		targetName = target.Deployment
		var err error
		pods, err = s.pods.GetPodsByIndex(target.Deployment)
		if err != nil {
			return err
		}
	}

	log.Infof("Tapping %d pods for target %s", len(pods), targetName)

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

	for _, pod := range pods {
		// initiate a tap on the pod
		match, err := makeMatch(req)
		if err != nil {
			return err
		}
		go s.tapProxy(stream.Context(), rpsPerPod, match, pod.Status.PodIP, events)
	}

	// read events from the taps and send them back
	for event := range events {
		err := stream.Send(event)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *server) TapByResource(req *public.TapByResourceRequest, stream pb.Tap_TapByResourceServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "TapByResource received nil TapByResourceRequest")
	}
	if req.Target == nil {
		return status.Errorf(codes.InvalidArgument, "TapByResource received nil target ResourceSelection: %+v", *req)
	}

	pods, err := s.getPodsFor(*req.Target)
	if err != nil {
		if status.Code(err) == codes.Unknown {
			if k8sErrors.ReasonForError(err) == metaV1.StatusReasonNotFound {
				err = status.Errorf(codes.NotFound, err.Error())
			} else {
				err = status.Errorf(codes.Internal, err.Error())
			}
		}
		return err
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
		if status.Code(err) == codes.Unknown {
			err = status.Errorf(codes.Internal, err.Error())
		}
		return err
	}

	for _, pod := range pods {
		// initiate a tap on the pod
		go s.tapProxy(stream.Context(), rpsPerPod, match, pod.Status.PodIP, events)
	}

	// read events from the taps and send them back
	for event := range events {
		err := stream.Send(event)
		if err != nil {
			if status.Code(err) == codes.Unknown {
				err = status.Errorf(codes.Internal, err.Error())
			}
			return err
		}
	}
	return nil
}

func validatePort(port uint32) error {
	if port > 65535 {
		return status.Errorf(codes.InvalidArgument, "port number of range: %d", port)
	}
	return nil
}

func makeMatch(req *public.TapRequest) (*proxy.ObserveRequest_Match, error) {
	matches := make([]*proxy.ObserveRequest_Match, 0)
	if req.FromIP != "" {
		ip, err := util.ParseIPV4(req.FromIP)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Source{
				Source: &proxy.ObserveRequest_Match_Tcp{
					Match: &proxy.ObserveRequest_Match_Tcp_Netmask_{
						Netmask: &proxy.ObserveRequest_Match_Tcp_Netmask{
							Ip:   ip,
							Mask: 32,
						},
					},
				},
			},
		})
	}

	if req.FromPort != 0 {
		if err := validatePort(req.FromPort); err != nil {
			return nil, err
		}
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Source{
				Source: &proxy.ObserveRequest_Match_Tcp{
					Match: &proxy.ObserveRequest_Match_Tcp_Ports{
						Ports: &proxy.ObserveRequest_Match_Tcp_PortRange{
							Min: req.FromPort,
						},
					},
				},
			},
		})
	}

	if req.ToIP != "" {
		ip, err := util.ParseIPV4(req.ToIP)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Destination{
				Destination: &proxy.ObserveRequest_Match_Tcp{
					Match: &proxy.ObserveRequest_Match_Tcp_Netmask_{
						Netmask: &proxy.ObserveRequest_Match_Tcp_Netmask{
							Ip:   ip,
							Mask: 32,
						},
					},
				},
			},
		})
	}

	if req.ToPort != 0 {
		if err := validatePort(req.ToPort); err != nil {
			return nil, err
		}
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Destination{
				Destination: &proxy.ObserveRequest_Match_Tcp{
					Match: &proxy.ObserveRequest_Match_Tcp_Ports{
						Ports: &proxy.ObserveRequest_Match_Tcp_PortRange{
							Min: req.ToPort,
						},
					},
				},
			},
		})
	}

	if req.Scheme != "" {
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Http_{
				Http: &proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Scheme{
						Scheme: parseScheme(req.Scheme),
					},
				},
			},
		})
	}

	if req.Method != "" {
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Http_{
				Http: &proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Method{
						Method: parseMethod(req.Method),
					},
				},
			},
		})
	}

	// exact match
	if req.Authority != "" {
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Http_{
				Http: &proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Authority{
						Authority: &proxy.ObserveRequest_Match_Http_StringMatch{
							Match: &proxy.ObserveRequest_Match_Http_StringMatch_Exact{
								Exact: req.Authority,
							},
						},
					},
				},
			},
		})
	}

	// prefix match
	if req.Path != "" {
		matches = append(matches, &proxy.ObserveRequest_Match{
			Match: &proxy.ObserveRequest_Match_Http_{
				Http: &proxy.ObserveRequest_Match_Http{
					Match: &proxy.ObserveRequest_Match_Http_Path{
						Path: &proxy.ObserveRequest_Match_Http_StringMatch{
							Match: &proxy.ObserveRequest_Match_Http_StringMatch_Prefix{
								Prefix: req.Path,
							},
						},
					},
				},
			},
		})
	}

	return &proxy.ObserveRequest_Match{
		Match: &proxy.ObserveRequest_Match_All{
			All: &proxy.ObserveRequest_Match_Seq{
				Matches: matches,
			},
		},
	}, nil
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
		dstLabels[k8sResourceTypesToDestinationLabels[resource.Type]] = resource.Name
	}
	if resource.Type != pkgK8s.KubernetesNamespaces && resource.Namespace != "" {
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

//
// TODO: factor all these functions out of public-api into a shared k8s lister/resource module
//

func (s *server) getPodsFor(res public.ResourceSelection) ([]*apiv1.Pod, error) {
	var err error
	namespace := res.Resource.Namespace
	objects := []runtime.Object{}

	switch res.Resource.Type {
	case pkgK8s.KubernetesDeployments:
		objects, err = s.getDeployments(res.Resource)
	case pkgK8s.KubernetesNamespaces:
		namespace = res.Resource.Name // special case for namespace
		objects, err = s.getNamespaces(res.Resource)
	case pkgK8s.KubernetesReplicationControllers:
		objects, err = s.getReplicationControllers(res.Resource)
	case pkgK8s.KubernetesServices:
		objects, err = s.getServices(res.Resource)

	// special case for pods
	case pkgK8s.KubernetesPods:
		return s.getPods(res.Resource)

	default:
		err = status.Errorf(codes.Unimplemented, "unimplemented resource type: %v", res.Resource.Type)
	}

	if err != nil {
		return nil, err
	}

	allPods := []*apiv1.Pod{}
	for _, obj := range objects {
		selector, err := getSelectorFromObject(obj)
		if err != nil {
			return nil, err
		}

		// TODO: special case namespace
		pods, err := s.podLister.Pods(namespace).List(selector)
		if err != nil {
			return nil, err
		}

		for _, pod := range pods {
			if isPendingOrRunning(pod) {
				allPods = append(allPods, pod)
			}
		}
	}

	return allPods, nil
}

func isPendingOrRunning(pod *apiv1.Pod) bool {
	pending := pod.Status.Phase == apiv1.PodPending
	running := pod.Status.Phase == apiv1.PodRunning
	terminating := pod.DeletionTimestamp != nil
	return (pending || running) && !terminating
}

func getSelectorFromObject(obj runtime.Object) (labels.Selector, error) {
	switch typed := obj.(type) {
	case *apiv1.Namespace:
		return labels.Everything(), nil

	case *appsv1beta2.Deployment:
		return labels.Set(typed.Spec.Selector.MatchLabels).AsSelector(), nil

	case *apiv1.ReplicationController:
		return labels.Set(typed.Spec.Selector).AsSelector(), nil

	case *apiv1.Service:
		return labels.Set(typed.Spec.Selector).AsSelector(), nil

	default:
		return nil, status.Errorf(codes.Unimplemented, "cannot get object selector: %v", obj)
	}
}

func (s *server) getDeployments(res *public.Resource) ([]runtime.Object, error) {
	var err error
	var deployments []*appsv1beta2.Deployment

	if res.Namespace == "" {
		deployments, err = s.deployLister.List(labels.Everything())
	} else if res.Name == "" {
		deployments, err = s.deployLister.Deployments(res.Namespace).List(labels.Everything())
	} else {
		var deployment *appsv1beta2.Deployment
		deployment, err = s.deployLister.Deployments(res.Namespace).Get(res.Name)
		deployments = []*appsv1beta2.Deployment{deployment}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, deploy := range deployments {
		objects = append(objects, deploy)
	}

	return objects, nil
}

func (s *server) getNamespaces(res *public.Resource) ([]runtime.Object, error) {
	var err error
	var namespaces []*apiv1.Namespace

	if res.Name == "" {
		namespaces, err = s.namespaceLister.List(labels.Everything())
	} else {
		var namespace *apiv1.Namespace
		namespace, err = s.namespaceLister.Get(res.Name)
		namespaces = []*apiv1.Namespace{namespace}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, ns := range namespaces {
		objects = append(objects, ns)
	}

	return objects, nil
}

func (s *server) getPods(res *public.Resource) ([]*apiv1.Pod, error) {
	var err error
	var pods []*apiv1.Pod

	if res.Namespace == "" {
		pods, err = s.podLister.List(labels.Everything())
	} else if res.Name == "" {
		pods, err = s.podLister.Pods(res.Namespace).List(labels.Everything())
	} else {
		var pod *apiv1.Pod
		pod, err = s.podLister.Pods(res.Namespace).Get(res.Name)
		pods = []*apiv1.Pod{pod}
	}

	if err != nil {
		return nil, err
	}

	var runningPods []*apiv1.Pod
	for _, pod := range pods {
		if isPendingOrRunning(pod) {
			runningPods = append(runningPods, pod)
		}
	}

	return runningPods, nil
}

func (s *server) getReplicationControllers(res *public.Resource) ([]runtime.Object, error) {
	var err error
	var rcs []*apiv1.ReplicationController

	if res.Namespace == "" {
		rcs, err = s.replicationControllerLister.List(labels.Everything())
	} else if res.Name == "" {
		rcs, err = s.replicationControllerLister.ReplicationControllers(res.Namespace).List(labels.Everything())
	} else {
		var rc *apiv1.ReplicationController
		rc, err = s.replicationControllerLister.ReplicationControllers(res.Namespace).Get(res.Name)
		rcs = []*apiv1.ReplicationController{rc}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, rc := range rcs {
		objects = append(objects, rc)
	}

	return objects, nil
}

func (s *server) getServices(res *public.Resource) ([]runtime.Object, error) {
	var err error
	var services []*apiv1.Service

	if res.Namespace == "" {
		services, err = s.serviceLister.List(labels.Everything())
	} else if res.Name == "" {
		services, err = s.serviceLister.Services(res.Namespace).List(labels.Everything())
	} else {
		var svc *apiv1.Service
		svc, err = s.serviceLister.Services(res.Namespace).Get(res.Name)
		services = []*apiv1.Service{svc}
	}

	if err != nil {
		return nil, err
	}

	objects := []runtime.Object{}
	for _, svc := range services {
		objects = append(objects, svc)
	}

	return objects, nil
}

func NewServer(
	addr string,
	tapPort uint,
	replicaSets *k8s.ReplicaSetStore,
	pods k8s.PodIndex,
	namespaceLister corelisters.NamespaceLister,
	deployLister applisters.DeploymentLister,
	replicaSetLister applisters.ReplicaSetLister,
	podLister corelisters.PodLister,
	replicationControllerLister corelisters.ReplicationControllerLister,
	serviceLister corelisters.ServiceLister,
) (*grpc.Server, net.Listener, error) {

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	s := util.NewGrpcServer()
	srv := server{
		tapPort:                     tapPort,
		replicaSets:                 replicaSets,
		pods:                        pods,
		namespaceLister:             namespaceLister,
		deployLister:                deployLister,
		replicaSetLister:            replicaSetLister,
		podLister:                   podLister,
		replicationControllerLister: replicationControllerLister,
		serviceLister:               serviceLister,
	}
	pb.RegisterTapServer(s, &srv)

	// TODO: register shutdown hook to call pods.Stop() and replicatSets.Stop()

	return s, lis, nil
}
