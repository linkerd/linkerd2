package public

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/fields"

	"github.com/prometheus/common/model"
	apiUtil "github.com/runconduit/conduit/controller/api/util"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	v1beta1 "k8s.io/api/apps/v1beta1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	reqQuery = "sum(increase(response_total{%s}[%s])) by (%s, classification)"
)

type meshedCount struct {
	inMesh    uint64
	notInMesh uint64
}

func (h *handler) StatSummary(ctx context.Context, req *pb.StatSummaryRequest) (*pb.StatSummaryResponse, error) {
	resourceType, err := k8s.CanonicalKubernetesNameFromFriendlyName(req.Resource.Spec.Type)
	if err != nil {
		return nil, err
	}

	switch resourceType {
	case k8s.KubernetesDeployments:
		return h.deploymentQuery(ctx, req)
	default:
		return nil, errors.New("Unimplemented resource type")
	}
}

func (h *handler) deploymentQuery(ctx context.Context, req *pb.StatSummaryRequest) (*pb.StatSummaryResponse, error) {
	rows := make([]*pb.StatTable_PodGroup_Row, 0)
	deployments := []v1beta1.Deployment{}
	var meshCount map[string]meshedCount

	timeWindow, err := apiUtil.GetWindowString(req.TimeWindow)
	if err != nil {
		return nil, err
	}

	// TODO: parallelize the k8s api query and the prometheus query
	if req.Resource.Spec.Name == "" || req.Resource.Spec.Name == "all" {
		deployments, meshCount, err = h.getDeployments(req.Resource.Spec.Namespace)
	} else {
		deployments, meshCount, err = h.getDeployment(req.Resource.Spec.Namespace, req.Resource.Spec.Name)
	}
	if err != nil {
		return nil, err
	}

	requestLabels := buildRequestLabels(req)

	requestMetrics, err := h.getRequests(ctx, requestLabels, req.Resource.Spec.Type, timeWindow)
	if err != nil {
		return nil, err
	}

	for _, resource := range deployments {
		row := pb.StatTable_PodGroup_Row{
			Spec: &pb.Resource{
				Namespace: resource.Namespace,
				Type:      req.Resource.Spec.Type,
				Name:      resource.Name,
			},
			TimeWindow: req.TimeWindow,
			Stats:      &pb.BasicStats{},
		}
		if val, ok := requestMetrics[resource.Name]; ok {
			row.Stats.SuccessCount = val["success"]
			row.Stats.FailureCount = val["fail"]
		}
		if count, ok := meshCount[resource.Name]; ok {
			row.MeshedPodCount = count.inMesh
			row.TotalPodCount = count.inMesh + count.notInMesh
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
	case *pb.StatSummaryRequest_None:
		direction = "inbound"

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
			req.Resource.Spec.Namespace,
			req.Resource.Spec.Type,
			req.Resource.Spec.Name,
		)
		labels = append(labels, srcLabel)

		outFromNs := req.GetOutFromResource().Namespace
		if outFromNs == "" {
			outFromNs = req.Resource.Spec.Namespace
		}

		labels = append(labels, promLabel("namespace", outFromNs))
		if req.Resource.Spec.Name != "" {
			labels = append(labels, promLabel(req.GetOutFromResource().Type, req.GetOutFromResource().Name))
		}
	default:
		direction = "inbound"

	}

	// it's weird to check this again outside the switch, but including this code
	// in the other three switch branches is very repetitive
	if req.GetOutFromResource() == nil {
		labels = append(labels, promLabel("namespace", req.Resource.Spec.Namespace))
		if req.Resource.Spec.Name != "" {
			labels = append(labels, promLabel(req.Resource.Spec.Type, req.Resource.Spec.Name))
		}
	}
	labels = append(labels, promLabel("direction", direction))

	return strings.Join(labels, ",")
}

func (h *handler) getRequests(ctx context.Context, reqLabels string, groupBy string, timeWindow string) (map[string]map[string]uint64, error) {
	requestsQuery := fmt.Sprintf(reqQuery, reqLabels, timeWindow, groupBy)

	resultVector, err := h.QueryProm(ctx, requestsQuery)
	if err != nil {
		return nil, err
	}
	return processRequests(resultVector, groupBy), nil
}

func processRequests(vec model.Vector, labelSelector string) map[string]map[string]uint64 {
	result := make(map[string]map[string]uint64, 0)
	for _, sample := range vec {
		labels := metricToMap(sample.Metric)
		if result[labels[labelSelector]] == nil {
			result[labels[labelSelector]] = make(map[string]uint64)
		}

		// 'increase' extrapolates values, so sample.Value could be a non-integer
		result[labels[labelSelector]][labels["classification"]] = uint64(sample.Value)
	}
	return result
}

func metricToMap(metric model.Metric) map[string]string {
	labels := make(map[string]string)
	for k, v := range metric {
		labels[string(k)] = string(v)
	}
	return labels
}

func (h *handler) getDeployment(namespace string, name string) ([]v1beta1.Deployment, map[string]meshedCount, error) {
	if namespace == "" {
		namespace = apiv1.NamespaceDefault
	}

	deploymentsClient := h.k8sClient.AppsV1beta1().Deployments(namespace)
	deployment, err := deploymentsClient.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}

	return []v1beta1.Deployment{*deployment}, nil, nil
}

func (h *handler) getDeployments(namespace string) ([]v1beta1.Deployment, map[string]meshedCount, error) {
	if namespace == "" {
		namespace = apiv1.NamespaceDefault
	}

	deploymentsClient := h.k8sClient.AppsV1beta1().Deployments(namespace)
	list, err := deploymentsClient.List(metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}

	meshedPodCount := make(map[string]meshedCount)
	for _, item := range list.Items {
		// TODO: parallelize
		meshCount, err := h.getMeshedPodCount(namespace, item.DeepCopyObject())
		if err != nil {
			return nil, nil, err
		}
		meshedPodCount[item.Name] = meshCount
	}

	return list.Items, meshedPodCount, nil
}

// this takes a long time for namespaces with many pods
func (h *handler) getMeshedPodCount(namespace string, obj runtime.Object) (meshedCount, error) {
	var meshCount meshedCount
	selector, _ := getSelectorFromObject(obj)

	pods, err := h.getAssociatedPods(namespace, selector)
	if err != nil {
		return meshCount, err
	}

	for _, pod := range pods {
		if isInMesh(pod) {
			meshCount.inMesh++
		} else {
			meshCount.notInMesh++
		}
	}

	return meshCount, nil
}

func isInMesh(pod apiv1.Pod) bool {
	_, ok := pod.Annotations["conduit.io/proxy-version"]
	return ok
}

func (h *handler) getAssociatedPods(namespace string, selector map[string]string) ([]apiv1.Pod, error) {
	var podList *apiv1.PodList

	selectorSet := labels.Set(selector).AsSelector().String()
	if selectorSet == "" {
		return podList.Items, nil
	}

	podsClient := h.k8sClient.Core().Pods(namespace)
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
	case *v1beta1.Deployment:
		return typed.Spec.Selector.MatchLabels, nil

	default:
		return nil, fmt.Errorf("Cannot get object selector: %v", obj)
	}
}

func (h *handler) QueryProm(ctx context.Context, query string) (model.Vector, error) {
	log.Debugf("Query request: %+v", query)
	end := time.Now()

	// single data point (aka summary) query
	res, err := h.prometheusAPI.Query(ctx, query, end)
	if err != nil {
		log.Errorf("Query(%+v, %+v) failed with: %+v", query, end, err)
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
