package public

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	apiUtil "github.com/runconduit/conduit/controller/api/util"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	reqQuery = "sum(increase(response_total{%s}[%s])) by (%s, classification)"
)

var (
	k8sResourceTypesToPromLabels = map[string]model.LabelName{
		k8s.KubernetesDeployments: "deployment",
	}
)

type meshedCount struct {
	inMesh uint64
	total  uint64
}

func (s *grpcServer) StatSummary(ctx context.Context, req *pb.StatSummaryRequest) (*pb.StatSummaryResponse, error) {
	switch req.Selector.Resource.Type {
	case k8s.KubernetesDeployments:
		return s.deploymentQuery(ctx, req)
	default:
		return nil, errors.New("Unimplemented resource type: " + req.Selector.Resource.Type)
	}
}

func (s *grpcServer) deploymentQuery(ctx context.Context, req *pb.StatSummaryRequest) (*pb.StatSummaryResponse, error) {
	rows := make([]*pb.StatTable_PodGroup_Row, 0)
	deployments := []appsv1.Deployment{}
	var meshCount map[string]*meshedCount

	timeWindow, err := apiUtil.GetWindowString(req.TimeWindow)
	if err != nil {
		return nil, err
	}

	// TODO: parallelize the k8s api query and the prometheus query
	if req.Selector.Resource.Name == "" {
		deployments, meshCount, err = s.getDeployments(req.Selector.Resource.Namespace)
	} else {
		deployments, meshCount, err = s.getDeployment(req.Selector.Resource.Namespace, req.Selector.Resource.Name)
	}
	if err != nil {
		return nil, err
	}

	requestLabels := buildRequestLabels(req)
	promResourceLabel, ok := k8sResourceTypesToPromLabels[req.Selector.Resource.Type]
	if !ok {
		return nil, errors.New("Resource Type has no Prometheus equivalent: " + req.Selector.Resource.Type)
	}

	requestMetrics, err := s.getRequests(ctx, requestLabels, string(promResourceLabel), timeWindow)
	if err != nil {
		return nil, err
	}

	for _, resource := range deployments {
		row := pb.StatTable_PodGroup_Row{
			Resource: &pb.Resource{
				Namespace: resource.Namespace,
				Type:      req.Selector.Resource.Type,
				Name:      resource.Name,
			},
			TimeWindow: req.TimeWindow,
			Stats:      requestMetrics[resource.Name],
		}
		if count, ok := meshCount[resource.Name]; ok {
			row.MeshedPodCount = count.inMesh
			row.TotalPodCount = count.total
		}

		rows = append(rows, &row)
	}

	rsp := pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: []*pb.StatTable{
					&pb.StatTable{
						Table: &pb.StatTable_PodGroup_{
							PodGroup: &pb.StatTable_PodGroup{
								Rows: rows,
							},
						},
					},
				},
			},
		},
	}

	return &rsp, nil
}

func promLabel(key, val string) string {
	return fmt.Sprintf("%s=\"%s\"", key, val)
}

func buildRequestLabels(req *pb.StatSummaryRequest) string {
	labels := []string{}

	var direction string
	switch req.Outbound.(type) {
	case *pb.StatSummaryRequest_OutToResource:
		direction = "outbound"

		dstLabel := fmt.Sprintf("dst_namespace=\"%s\", dst_%s=\"%s\"",
			req.GetOutToResource().Namespace,
			req.GetOutToResource().Type,
			req.GetOutToResource().Name,
		)
		labels = append(labels, dstLabel)

	case *pb.StatSummaryRequest_OutFromResource:
		direction = "outbound"

		srcLabel := fmt.Sprintf("dst_namespace=\"%s\", dst_%s=\"%s\"",
			req.Selector.Resource.Namespace,
			req.Selector.Resource.Type,
			req.Selector.Resource.Name,
		)
		labels = append(labels, srcLabel)

		outFromNs := req.GetOutFromResource().Namespace
		if outFromNs == "" {
			outFromNs = req.Selector.Resource.Namespace
		}

		labels = append(labels, promLabel("namespace", outFromNs))
		if req.Selector.Resource.Name != "" {
			labels = append(labels, promLabel(req.GetOutFromResource().Type, req.GetOutFromResource().Name))
		}
	default:
		direction = "inbound"
	}

	// it's weird to check this again outside the switch, but including this code
	// in the other three switch branches is very repetitive
	if req.GetOutFromResource() == nil {
		labels = append(labels, promLabel("namespace", req.Selector.Resource.Namespace))
		if req.Selector.Resource.Name != "" {
			labels = append(labels, promLabel(req.Selector.Resource.Type, req.Selector.Resource.Name))
		}
	}
	labels = append(labels, promLabel("direction", direction))

	return strings.Join(labels, ",")
}

func (s *grpcServer) getRequests(ctx context.Context, reqLabels string, groupBy string, timeWindow string) (map[string]*pb.BasicStats, error) {
	requestsQuery := fmt.Sprintf(reqQuery, reqLabels, timeWindow, groupBy)

	resultVector, err := s.queryProm(ctx, requestsQuery)
	if err != nil {
		return nil, err
	}

	return processRequests(resultVector, groupBy), nil
}

func processRequests(vec model.Vector, labelSelector string) map[string]*pb.BasicStats {
	result := make(map[string]*pb.BasicStats)

	for _, sample := range vec {
		label := string(sample.Metric[model.LabelName(labelSelector)])
		if result[label] == nil {
			result[label] = &pb.BasicStats{}
		}

		switch string(sample.Metric[model.LabelName("classification")]) {
		case "success":
			result[label].SuccessCount = uint64(sample.Value)
		case "fail":
			result[label].FailureCount = uint64(sample.Value)
		}
	}
	return result
}

func (s *grpcServer) getDeployment(namespace string, name string) ([]appsv1.Deployment, map[string]*meshedCount, error) {
	if namespace == "" {
		namespace = apiv1.NamespaceDefault
	}

	deploymentsClient := s.k8sClient.Apps().Deployments(namespace)
	deployment, err := deploymentsClient.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}

	meshCount, err := s.getMeshedPodCount(namespace, deployment)
	if err != nil {
		return nil, nil, err
	}
	meshMap := map[string]*meshedCount{deployment.Name: meshCount}
	return []appsv1.Deployment{*deployment}, meshMap, nil
}

func (s *grpcServer) getDeployments(namespace string) ([]appsv1.Deployment, map[string]*meshedCount, error) {
	if namespace == "" {
		namespace = apiv1.NamespaceDefault
	}

	deploymentsClient := s.k8sClient.Apps().Deployments(namespace)
	list, err := deploymentsClient.List(metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}

	meshedPodCount := make(map[string]*meshedCount)
	for _, item := range list.Items {
		// TODO: parallelize
		meshCount, err := s.getMeshedPodCount(namespace, &item)
		if err != nil {
			return nil, nil, err
		}
		meshedPodCount[item.Name] = meshCount
	}

	return list.Items, meshedPodCount, nil
}

// this takes a long time for namespaces with many pods
func (s *grpcServer) getMeshedPodCount(namespace string, obj runtime.Object) (*meshedCount, error) {
	selector, err := getSelectorFromObject(obj)
	if err != nil {
		return nil, err
	}

	pods, err := s.getAssociatedPods(namespace, selector)
	if err != nil {
		return nil, err
	}

	meshCount := &meshedCount{}
	for _, pod := range pods {
		meshCount.total++
		if isInMesh(pod) {
			meshCount.inMesh++
		}
	}

	return meshCount, nil
}

func isInMesh(pod apiv1.Pod) bool {
	_, ok := pod.Annotations[k8s.ProxyVersionAnnotation]
	return ok
}

func (s *grpcServer) getAssociatedPods(namespace string, selector map[string]string) ([]apiv1.Pod, error) {
	var podList *apiv1.PodList

	selectorSet := labels.Set(selector).AsSelector().String()
	if selectorSet == "" {
		return podList.Items, nil
	}

	podsClient := s.k8sClient.Core().Pods(namespace)
	podList, err := podsClient.List(metav1.ListOptions{
		FieldSelector: fields.Everything().String(),
		LabelSelector: selectorSet,
	})
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}

func getSelectorFromObject(obj runtime.Object) (map[string]string, error) {
	switch typed := obj.(type) {
	case *appsv1.Deployment:
		return typed.Spec.Selector.MatchLabels, nil

	default:
		return nil, fmt.Errorf("Cannot get object selector: %v", obj)
	}
}

func (s *grpcServer) queryProm(ctx context.Context, query string) (model.Vector, error) {
	log.Debugf("Query request: %+v", query)

	// single data point (aka summary) query
	res, err := s.prometheusAPI.Query(ctx, query, time.Time{})
	if err != nil {
		log.Errorf("Query(%+v, %+v) failed with: %+v", query, err)
		return nil, err
	}
	log.Debugf("Query response: %+v", res)

	if res.Type() != model.ValVector {
		err = fmt.Errorf("Unexpected query result type (expected Vector): %s", res.Type())
		log.Error(err)
		return nil, err
	}

	return res.(model.Vector), nil
}

func mapKeys(m map[string]interface{}) []string {
	res := make([]string, 0)
	for k := range m {
		res = append(res, k)
	}
	return res
}
