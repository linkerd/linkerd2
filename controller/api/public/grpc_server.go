package public

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/golang/protobuf/ptypes/duration"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	tapPb "github.com/runconduit/conduit/controller/gen/controller/tap"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/controller/k8s"
	pkgK8s "github.com/runconduit/conduit/pkg/k8s"
	"github.com/runconduit/conduit/pkg/version"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8sV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type (
	grpcServer struct {
		prometheusAPI       promv1.API
		tapClient           tapPb.TapClient
		lister              *k8s.Lister
		controllerNamespace string
		ignoredNamespaces   []string
	}
)

const (
	podQuery                   = "count(process_start_time_seconds) by (pod)"
	K8sClientSubsystemName     = "kubernetes"
	K8sClientCheckDescription  = "control plane can talk to Kubernetes"
	PromClientSubsystemName    = "prometheus"
	PromClientCheckDescription = "control plane can talk to Prometheus"
)

func newGrpcServer(
	promAPI promv1.API,
	tapClient tapPb.TapClient,
	lister *k8s.Lister,
	controllerNamespace string,
	ignoredNamespaces []string,
) *grpcServer {
	return &grpcServer{
		prometheusAPI:       promAPI,
		tapClient:           tapClient,
		lister:              lister,
		controllerNamespace: controllerNamespace,
		ignoredNamespaces:   ignoredNamespaces,
	}
}

func (*grpcServer) Version(ctx context.Context, req *pb.Empty) (*pb.VersionInfo, error) {
	return &pb.VersionInfo{GoVersion: runtime.Version(), ReleaseVersion: version.Version, BuildDate: "1970-01-01T00:00:00Z"}, nil
}

func (s *grpcServer) ListPods(ctx context.Context, req *pb.Empty) (*pb.ListPodsResponse, error) {
	log.Debugf("ListPods request: %+v", req)

	// Reports is a map from instance name to the absolute time of the most recent
	// report from that instance.
	reports := make(map[string]time.Time)

	// Query Prometheus for all pods present
	vec, err := s.queryProm(ctx, podQuery)
	if err != nil {
		return nil, err
	}
	for _, sample := range vec {
		pod := string(sample.Metric["pod"])
		timestamp := sample.Timestamp

		reports[pod] = time.Unix(0, int64(timestamp)*int64(time.Millisecond))
	}

	pods, err := s.lister.Pod.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	podList := make([]*pb.Pod, 0)

	for _, pod := range pods {
		if s.shouldIgnore(pod) {
			continue
		}

		deployment, err := s.getDeploymentFor(pod)
		if err != nil {
			log.Debugf("Cannot get deployment for pod %s: %s", pod.Name, err)
			deployment = ""
		}

		updated, added := reports[pod.Name]

		status := string(pod.Status.Phase)
		if pod.DeletionTimestamp != nil {
			status = "Terminating"
		}

		controllerComponent := pod.Labels[pkgK8s.ControllerComponentLabel]
		controllerNS := pod.Labels[pkgK8s.ControllerNSLabel]

		item := &pb.Pod{
			Name:                pod.Namespace + "/" + pod.Name,
			Deployment:          deployment, // TODO: this is of the form `namespace/deployment`, it should just be `deployment`
			Status:              status,
			PodIP:               pod.Status.PodIP,
			Added:               added,
			ControllerNamespace: controllerNS,
			ControlPlane:        controllerComponent != "",
		}
		if added {
			since := time.Since(updated)
			item.SinceLastReport = &duration.Duration{
				Seconds: int64(since / time.Second),
				Nanos:   int32(since % time.Second),
			}
		}
		podList = append(podList, item)
	}

	rsp := pb.ListPodsResponse{Pods: podList}

	log.Debugf("ListPods response: %+v", rsp)

	return &rsp, nil
}

func (s *grpcServer) SelfCheck(ctx context.Context, in *healthcheckPb.SelfCheckRequest) (*healthcheckPb.SelfCheckResponse, error) {
	k8sClientCheck := &healthcheckPb.CheckResult{
		SubsystemName:    K8sClientSubsystemName,
		CheckDescription: K8sClientCheckDescription,
		Status:           healthcheckPb.CheckStatus_OK,
	}
	_, err := s.lister.Pod.List(labels.Everything())
	if err != nil {
		k8sClientCheck.Status = healthcheckPb.CheckStatus_ERROR
		k8sClientCheck.FriendlyMessageToUser = fmt.Sprintf("Error talking to Kubernetes from control plane: %s", err.Error())
	}

	promClientCheck := &healthcheckPb.CheckResult{
		SubsystemName:    PromClientSubsystemName,
		CheckDescription: PromClientCheckDescription,
		Status:           healthcheckPb.CheckStatus_OK,
	}
	_, err = s.queryProm(ctx, podQuery)
	if err != nil {
		promClientCheck.Status = healthcheckPb.CheckStatus_ERROR
		promClientCheck.FriendlyMessageToUser = fmt.Sprintf("Error talking to Prometheus from control plane: %s", err.Error())
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
		//TODO: why not return the error?
		log.Errorf("Unexpected error tapping [%v]: %v", req, err)
		return nil
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

func (s *grpcServer) shouldIgnore(pod *k8sV1.Pod) bool {
	for _, namespace := range s.ignoredNamespaces {
		if pod.Namespace == namespace {
			return true
		}
	}
	return false
}

func (s *grpcServer) getDeploymentFor(pod *k8sV1.Pod) (string, error) {
	namespace := pod.Namespace
	if len(pod.GetOwnerReferences()) == 0 {
		return "", fmt.Errorf("Pod %s has no owner", pod.Name)
	}
	parent := pod.GetOwnerReferences()[0]
	if parent.Kind != "ReplicaSet" {
		return "", fmt.Errorf("Pod %s parent is not a ReplicaSet", pod.Name)
	}

	rs, err := s.lister.RS.GetPodReplicaSets(pod)
	if err != nil {
		return "", err
	}
	if len(rs) == 0 || len(rs[0].GetOwnerReferences()) == 0 {
		return "", fmt.Errorf("Pod %s has no replicasets", pod.Name)
	}

	for _, r := range rs {
		for _, owner := range r.GetOwnerReferences() {
			switch owner.Kind {
			case "Deployment":
				return namespace + "/" + owner.Name, nil
			}
		}
	}

	return "", fmt.Errorf("Pod %s owner is not a Deployment", pod.Name)
}
