package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang/protobuf/ptypes/duration"
	"github.com/linkerd/linkerd2/controller/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/metrics-api/util"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Server specifies the interface the Viz metric API server should implement
type Server interface {
	pb.ApiServer
}

type grpcServer struct {
	pb.UnimplementedApiServer
	prometheusAPI       promv1.API
	k8sAPI              *k8s.API
	controllerNamespace string
	clusterDomain       string
	ignoredNamespaces   []string
}

type podReport struct {
	lastReport              time.Time
	processStartTimeSeconds time.Time
}

const (
	podQuery                   = "max(process_start_time_seconds{%s}) by (pod, namespace)"
	k8sClientSubsystemName     = "kubernetes"
	k8sClientCheckDescription  = "linkerd viz can talk to Kubernetes"
	promClientSubsystemName    = "prometheus"
	promClientCheckDescription = "linkerd viz can talk to Prometheus"
)

// NewGrpcServer creates a new instance of the Api server and registers it
// with Prometheus.
func NewGrpcServer(
	promAPI promv1.API,
	k8sAPI *k8s.API,
	controllerNamespace string,
	clusterDomain string,
	ignoredNamespaces []string,
) *grpc.Server {

	server := &grpcServer{
		prometheusAPI:       promAPI,
		k8sAPI:              k8sAPI,
		controllerNamespace: controllerNamespace,
		clusterDomain:       clusterDomain,
		ignoredNamespaces:   ignoredNamespaces,
	}

	pb.RegisterApiServer(prometheus.NewGrpcServer(), server)
	s := prometheus.NewGrpcServer()
	pb.RegisterApiServer(s, server)

	return s
}

func (s *grpcServer) ListPods(ctx context.Context, req *pb.ListPodsRequest) (*pb.ListPodsResponse, error) {
	log.Debugf("ListPods request: %+v", req)

	targetOwner := req.GetSelector().GetResource()

	// Reports is a map from instance name to the absolute time of the most recent
	// report from that instance and its process start time
	reports := make(map[string]podReport)

	labelSelector := labels.Everything()
	if s := req.GetSelector().GetLabelSelector(); s != "" {
		var err error
		labelSelector, err = labels.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector %q: %w", s, err)
		}
	}

	nsQuery := ""
	namespace := ""
	if targetOwner.GetNamespace() != "" {
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
	if err != nil && !errors.Is(err, ErrNoPrometheusInstance) {
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

		ownerKind, ownerName := s.k8sAPI.GetOwnerKindAndName(ctx, pod, false)
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

		podList = append(podList, item)
	}

	rsp := pb.ListPodsResponse{Pods: podList}

	log.Debugf("ListPods response: %s", rsp.String())

	return &rsp, nil
}

func (s *grpcServer) SelfCheck(ctx context.Context, in *pb.SelfCheckRequest) (*pb.SelfCheckResponse, error) {
	k8sClientCheck := &pb.CheckResult{
		SubsystemName:    k8sClientSubsystemName,
		CheckDescription: k8sClientCheckDescription,
		Status:           pb.CheckStatus_OK,
	}
	_, err := s.k8sAPI.Pod().Lister().List(labels.Everything())
	if err != nil {
		k8sClientCheck.Status = pb.CheckStatus_ERROR
		k8sClientCheck.FriendlyMessageToUser = fmt.Sprintf("Error calling the Kubernetes API: %s", err)
	}

	response := &pb.SelfCheckResponse{
		Results: []*pb.CheckResult{
			k8sClientCheck,
		},
	}

	if s.prometheusAPI != nil {
		promClientCheck := &pb.CheckResult{
			SubsystemName:    promClientSubsystemName,
			CheckDescription: promClientCheckDescription,
			Status:           pb.CheckStatus_OK,
		}
		_, err = s.queryProm(ctx, fmt.Sprintf(podQuery, ""))
		if err != nil {
			promClientCheck.Status = pb.CheckStatus_ERROR
			promClientCheck.FriendlyMessageToUser = fmt.Sprintf("Error calling Prometheus from the control plane: %s", err)
		}

		response.Results = append(response.Results, promClientCheck)
	}

	return response, nil
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

// validateTimeWindow returns an error if the Prometheus scrape interval
// is longer than the query time window. This is an opportunistic, best-effort
// validation: if we cannot determine the Prometheus scrape interval for any
// reason, we do not return an error.
func (s *grpcServer) validateTimeWindow(ctx context.Context, window string) error {
	config, err := s.prometheusAPI.Config(ctx)
	if err != nil {
		return nil
	}

	type PrometheusConfig struct {
		Global map[string]string
	}

	var prom PrometheusConfig
	err = yaml.Unmarshal([]byte(config.YAML), &prom)
	if err != nil {
		return nil
	}

	scrape_interval_str, found := prom.Global["scrape_interval"]
	if !found {
		return nil
	}

	scrape_interval, err := time.ParseDuration(scrape_interval_str)
	if err != nil {
		return nil
	}

	t, err := time.ParseDuration(window)
	if err != nil {
		return err
	}

	if t < scrape_interval {
		return fmt.Errorf("time window (%s) must be at least as long as the Prometheus scrape interval (%s)", window, scrape_interval)
	}
	return nil
}
