package public

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/golang/protobuf/ptypes/duration"
	"github.com/linkerd/linkerd2/controller/api/util"
	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	discoveryPb "github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	tapPb "github.com/linkerd/linkerd2/controller/gen/controller/tap"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/version"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// APIServer specifies the interface the Public API server should implement
type APIServer interface {
	pb.ApiServer
	discoveryPb.DiscoveryServer
}

type grpcServer struct {
	prometheusAPI       promv1.API
	tapClient           tapPb.TapClient
	discoveryClient     discoveryPb.DiscoveryClient
	k8sAPI              *k8s.API
	controllerNamespace string
	ignoredNamespaces   []string
	singleNamespace     bool
}

type podReport struct {
	lastReport              time.Time
	processStartTimeSeconds time.Time
}

const (
	podQuery                   = "max(process_start_time_seconds{%s}) by (pod, namespace)"
	k8sClientSubsystemName     = "kubernetes"
	k8sClientCheckDescription  = "control plane can talk to Kubernetes"
	promClientSubsystemName    = "prometheus"
	promClientCheckDescription = "control plane can talk to Prometheus"
)

func newGrpcServer(
	promAPI promv1.API,
	tapClient tapPb.TapClient,
	discoveryClient discoveryPb.DiscoveryClient,
	k8sAPI *k8s.API,
	controllerNamespace string,
	ignoredNamespaces []string,
	singleNamespace bool,
) *grpcServer {

	grpcServer := &grpcServer{
		prometheusAPI:       promAPI,
		tapClient:           tapClient,
		discoveryClient:     discoveryClient,
		k8sAPI:              k8sAPI,
		controllerNamespace: controllerNamespace,
		ignoredNamespaces:   ignoredNamespaces,
		singleNamespace:     singleNamespace,
	}

	pb.RegisterApiServer(prometheus.NewGrpcServer(), grpcServer)

	return grpcServer
}

func (*grpcServer) Version(ctx context.Context, req *pb.Empty) (*pb.VersionInfo, error) {
	return &pb.VersionInfo{GoVersion: runtime.Version(), ReleaseVersion: version.Version, BuildDate: "1970-01-01T00:00:00Z"}, nil
}

func (s *grpcServer) ListPods(ctx context.Context, req *pb.ListPodsRequest) (*pb.ListPodsResponse, error) {
	log.Debugf("ListPods request: %+v", req)

	targetOwner := req.GetSelector().GetResource()

	// Reports is a map from instance name to the absolute time of the most recent
	// report from that instance and its process start time
	reports := make(map[string]podReport)

	if req.GetNamespace() != "" && req.GetSelector() != nil {
		return nil, errors.New("cannot set both namespace and resource in the request. These are mutually exclusive")
	}

	labelSelector := labels.Everything()
	if s := req.GetSelector().GetLabelSelector(); s != "" {
		var err error
		labelSelector, err = labels.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector \"%s\": %s", s, err)
		}
	}

	nsQuery := ""
	namespace := ""
	if req.GetNamespace() != "" {
		namespace = req.GetNamespace()
	} else if targetOwner.GetNamespace() != "" {
		namespace = targetOwner.GetNamespace()
	} else if targetOwner.GetType() == pkgK8s.Namespace {
		namespace = targetOwner.GetName()
	}
	if namespace != "" {
		nsQuery = fmt.Sprintf("namespace=\"%s\"", namespace)
	}
	processStartTimeQuery := fmt.Sprintf(podQuery, nsQuery)

	// Query Prometheus for all pods present
	vec, err := s.queryProm(ctx, processStartTimeQuery)
	if err != nil {
		return nil, err
	}
	for _, sample := range vec {
		pod := string(sample.Metric["pod"])
		timestamp := sample.Timestamp

		reports[pod] = podReport{
			lastReport:              time.Unix(0, int64(timestamp)*int64(time.Millisecond)),
			processStartTimeSeconds: time.Unix(0, int64(sample.Value)*int64(time.Second)),
		}
	}

	var pods []*corev1.Pod
	if namespace != "" {
		pods, err = s.k8sAPI.Pod().Lister().Pods(namespace).List(labelSelector)
	} else {
		pods, err = s.k8sAPI.Pod().Lister().List(labelSelector)
	}

	if err != nil {
		return nil, err
	}
	podList := make([]*pb.Pod, 0)

	for _, pod := range pods {
		if s.shouldIgnore(pod) {
			continue
		}

		ownerKind, ownerName := s.k8sAPI.GetOwnerKindAndName(pod)
		// filter out pods without matching owner
		if targetOwner.GetNamespace() != "" && targetOwner.GetNamespace() != pod.GetNamespace() {
			continue
		}
		if targetOwner.GetType() != "" && targetOwner.GetType() != ownerKind {
			continue
		}
		if targetOwner.GetName() != "" && targetOwner.GetName() != ownerName {
			continue
		}

		updated, added := reports[pod.Name]

		item := util.K8sPodToPublicPod(*pod, ownerKind, ownerName)
		item.Added = added

		if added {
			since := time.Since(updated.lastReport)
			item.SinceLastReport = &duration.Duration{
				Seconds: int64(since / time.Second),
				Nanos:   int32(since % time.Second),
			}
			sinceStarting := time.Since(updated.processStartTimeSeconds)
			item.Uptime = &duration.Duration{
				Seconds: int64(sinceStarting / time.Second),
				Nanos:   int32(sinceStarting % time.Second),
			}
		}

		podList = append(podList, &item)
	}

	rsp := pb.ListPodsResponse{Pods: podList}

	log.Debugf("ListPods response: %+v", rsp)

	return &rsp, nil
}

func (s *grpcServer) SelfCheck(ctx context.Context, in *healthcheckPb.SelfCheckRequest) (*healthcheckPb.SelfCheckResponse, error) {
	k8sClientCheck := &healthcheckPb.CheckResult{
		SubsystemName:    k8sClientSubsystemName,
		CheckDescription: k8sClientCheckDescription,
		Status:           healthcheckPb.CheckStatus_OK,
	}
	_, err := s.k8sAPI.Pod().Lister().List(labels.Everything())
	if err != nil {
		k8sClientCheck.Status = healthcheckPb.CheckStatus_ERROR
		k8sClientCheck.FriendlyMessageToUser = fmt.Sprintf("Error calling the Kubernetes API: %s", err)
	}

	promClientCheck := &healthcheckPb.CheckResult{
		SubsystemName:    promClientSubsystemName,
		CheckDescription: promClientCheckDescription,
		Status:           healthcheckPb.CheckStatus_OK,
	}
	_, err = s.queryProm(ctx, fmt.Sprintf(podQuery, ""))
	if err != nil {
		promClientCheck.Status = healthcheckPb.CheckStatus_ERROR
		promClientCheck.FriendlyMessageToUser = fmt.Sprintf("Error calling Prometheus from the control plane: %s", err)
	}

	response := &healthcheckPb.SelfCheckResponse{
		Results: []*healthcheckPb.CheckResult{
			k8sClientCheck,
			promClientCheck,
		},
	}
	return response, nil
}

func (s *grpcServer) Tap(req *pb.TapRequest, stream pb.Api_TapServer) error {
	return status.Error(codes.Unimplemented, "Tap is deprecated, use TapByResource")
}

// Pass through to tap service
func (s *grpcServer) TapByResource(req *pb.TapByResourceRequest, stream pb.Api_TapByResourceServer) error {
	tapStream := stream.(tapServer)
	tapClient, err := s.tapClient.TapByResource(tapStream.Context(), req)
	if err != nil {
		log.Errorf("Unexpected error tapping [%v]: %v", req, err)
		return err
	}
	for {
		select {
		case <-tapStream.Context().Done():
			return nil
		default:
			event, err := tapClient.Recv()
			if err != nil {
				return err
			}
			tapStream.Send(event)
		}
	}
}

func (s *grpcServer) shouldIgnore(pod *corev1.Pod) bool {
	for _, namespace := range s.ignoredNamespaces {
		if pod.Namespace == namespace {
			return true
		}
	}
	return false
}

func (s *grpcServer) ListServices(ctx context.Context, req *pb.ListServicesRequest) (*pb.ListServicesResponse, error) {
	log.Debugf("ListServices request: %+v", req)

	services, err := s.k8sAPI.GetServices(req.Namespace, "")
	if err != nil {
		return nil, err
	}

	svcs := make([]*pb.Service, 0)
	for _, svc := range services {
		svcs = append(svcs, &pb.Service{
			Name:      svc.GetName(),
			Namespace: svc.GetNamespace(),
		})
	}

	return &pb.ListServicesResponse{Services: svcs}, nil
}

func (s *grpcServer) Endpoints(ctx context.Context, params *discoveryPb.EndpointsParams) (*discoveryPb.EndpointsResponse, error) {
	log.Debugf("Endpoints request: %+v", params)

	rsp, err := s.discoveryClient.Endpoints(ctx, params)
	if err != nil {
		log.Errorf("endpoints request to destination API failed: %s", err)
		return nil, err
	}

	return rsp, nil
}
