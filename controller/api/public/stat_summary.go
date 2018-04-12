package public

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/prometheus/common/model"
	apiUtil "github.com/runconduit/conduit/controller/api/util"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

type promType string
type promResult struct {
	prom promType
	vec  model.Vector
	err  error
}

const (
	reqQuery             = "sum(increase(response_total%s[%s])) by (%s)"
	latencyQuantileQuery = "histogram_quantile(%s, sum(irate(response_latency_ms_bucket%s[%s])) by (%s))"

	promRequests   = promType("QUERY_REQUESTS")
	promLatencyP50 = promType("0.5")
	promLatencyP95 = promType("0.95")
	promLatencyP99 = promType("0.99")

	namespaceLabel = model.LabelName("namespace")
)

var (
	promTypes = []promType{promRequests, promLatencyP50, promLatencyP95, promLatencyP99}

	k8sResourceTypesToPromLabels = map[string]model.LabelName{
		k8s.KubernetesDeployments: "deployment",
		k8s.KubernetesNamespaces:  namespaceLabel,
	}
)

type meshedCount struct {
	inMesh uint64
	total  uint64
}

func (s *grpcServer) StatSummary(ctx context.Context, req *pb.StatSummaryRequest) (*pb.StatSummaryResponse, error) {
	var err error
	var objectMap map[string]metav1.ObjectMeta
	var meshCount map[string]*meshedCount

	switch req.Selector.Resource.Type {
	case k8s.KubernetesDeployments:
		objectMap, meshCount, err = s.getDeployments(req.Selector.Resource)
		if err != nil {
			return nil, err
		}
	case k8s.KubernetesNamespaces:
		objectMap, meshCount, err = s.getNamespaces(req.Selector.Resource)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("Unimplemented resource type: " + req.Selector.Resource.Type)
	}

	return s.objectQuery(ctx, req, objectMap, meshCount)
}

func (s *grpcServer) objectQuery(
	ctx context.Context,
	req *pb.StatSummaryRequest,
	objects map[string]metav1.ObjectMeta,
	meshCount map[string]*meshedCount,
) (*pb.StatSummaryResponse, error) {
	rows := make([]*pb.StatTable_PodGroup_Row, 0)

	timeWindow, err := apiUtil.GetWindowString(req.TimeWindow)
	if err != nil {
		return nil, err
	}

	requestMetrics, err := s.getRequests(ctx, req, timeWindow)
	if err != nil {
		return nil, err
	}

	var keys []string

	if req.GetOutbound() == nil || req.GetNone() != nil {
		// if this request doesn't have outbound filtering, return all rows
		for key := range objects {
			keys = append(keys, key)
		}
	} else {
		// otherwise only return rows for which we have stats
		for key := range requestMetrics {
			keys = append(keys, key)
		}
	}

	for _, key := range keys {
		resource, ok := objects[key]
		if !ok {
			continue
		}

		row := pb.StatTable_PodGroup_Row{
			Resource: &pb.Resource{
				Namespace: resource.Namespace,
				Type:      req.Selector.Resource.Type,
				Name:      resource.Name,
			},
			TimeWindow: req.TimeWindow,
			Stats:      requestMetrics[key],
		}

		if count, ok := meshCount[key]; ok {
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

func promNameLabel(resource *pb.Resource) model.LabelPair {
	return model.LabelPair{
		Name:  promResourceType(resource),
		Value: model.LabelValue(resource.Name),
	}
}

func promNsLabel(resource *pb.Resource) model.LabelValue {
	if resource.Type == k8s.KubernetesNamespaces {
		return model.LabelValue(resource.Name)
	}

	return model.LabelValue(resource.Namespace)
}

func promNsLabelFallback(primary *pb.Resource, secondary *pb.Resource) model.LabelValue {
	var ns string
	if primary.Type == k8s.KubernetesNamespaces {
		ns = primary.Name
	}
	if ns == "" && secondary.Type == k8s.KubernetesNamespaces {
		ns = secondary.Name
	}
	if ns == "" {
		ns = primary.Namespace
	}
	if ns == "" {
		ns = secondary.Namespace
	}

	return model.LabelValue(ns)
}

func promDstNameLabel(resource *pb.Resource) model.LabelPair {
	return model.LabelPair{
		Name:  "dst_" + promResourceType(resource),
		Value: model.LabelValue(resource.Name),
	}
}

func promResourceType(resource *pb.Resource) model.LabelName {
	return k8sResourceTypesToPromLabels[resource.Type]
}

func buildRequestLabels(req *pb.StatSummaryRequest) model.LabelSet {
	labels := model.LabelSet{}

	var direction model.LabelValue

	switch req.Outbound.(type) {
	case *pb.StatSummaryRequest_ToResource:
		direction = "outbound"
		src := req.Selector.Resource
		dst := req.GetToResource()

		dstNs := promNsLabelFallback(dst, src)
		if dstNs != "" {
			labels["dst_"+namespaceLabel] = model.LabelValue(dstNs)
		}

		srcNs := promNsLabel(src)
		if srcNs != "" {
			labels[namespaceLabel] = srcNs
		}

		if dst.Type != k8s.KubernetesNamespaces && dst.Name != "" {
			dstNameLabel := promDstNameLabel(dst)
			labels[dstNameLabel.Name] = dstNameLabel.Value
		}

		if src.Name != "" {
			nameLabel := promNameLabel(src)
			labels[nameLabel.Name] = nameLabel.Value
		}

	case *pb.StatSummaryRequest_FromResource:
		direction = "outbound"
		src := req.GetFromResource()
		dst := req.Selector.Resource

		srcNs := promNsLabelFallback(src, dst)
		if srcNs != "" {
			labels[namespaceLabel] = model.LabelValue(srcNs)
		}

		dstNs := promNsLabel(src)
		if dstNs != "" {
			labels["dst_"+namespaceLabel] = dstNs
		}

		if src.Type != k8s.KubernetesNamespaces && src.Name != "" {
			srcNameLabel := promNameLabel(src)
			labels[srcNameLabel.Name] = srcNameLabel.Value
		}

		if dst.Name != "" {
			dstNameLabel := promDstNameLabel(dst)
			labels[dstNameLabel.Name] = dstNameLabel.Value
		}

	default:
		direction = "inbound"
		src := req.Selector.Resource

		ns := promNsLabel(src)
		if ns != "" {
			labels[namespaceLabel] = ns
		}

		if src.Name != "" {
			nameLabel := promNameLabel(src)
			labels[nameLabel.Name] = nameLabel.Value
		}
	}

	labels["direction"] = direction

	return labels
}

func buildGroupBy(req *pb.StatSummaryRequest, groupByType promType) model.LabelNames {
	groupBy := model.LabelNames{
		promResourceType(req.Selector.Resource),
	}

	if req.Selector.Resource.Type != k8s.KubernetesNamespaces {
		groupBy = append(groupBy, "namespace")
	}

	switch groupByType {
	case promRequests:
		groupBy = append(groupBy, "classification")
	case promLatencyP50, promLatencyP95, promLatencyP99:
		groupBy = append(groupBy, model.BucketLabel)
	}

	switch req.Outbound.(type) {
	case *pb.StatSummaryRequest_ToResource, *pb.StatSummaryRequest_FromResource:
		groupBy = append(groupBy, "dst_"+promResourceType(req.Selector.Resource))
	}

	return groupBy
}

func (s *grpcServer) getRequests(ctx context.Context, req *pb.StatSummaryRequest, timeWindow string) (map[string]*pb.BasicStats, error) {
	reqLabels := buildRequestLabels(req)
	resultChan := make(chan promResult)

	// kick off 4 asynchronous queries: 1 request volume + 3 latency
	go func() {
		groupBy := buildGroupBy(req, promRequests)
		requestsQuery := fmt.Sprintf(reqQuery, reqLabels, timeWindow, groupBy)
		resultVector, err := s.queryProm(ctx, requestsQuery)

		resultChan <- promResult{
			prom: promRequests,
			vec:  resultVector,
			err:  err,
		}
	}()

	for _, quantile := range []promType{promLatencyP50, promLatencyP95, promLatencyP99} {
		go func(quantile promType) {
			groupBy := buildGroupBy(req, quantile)
			latencyQuery := fmt.Sprintf(latencyQuantileQuery, quantile, reqLabels, timeWindow, groupBy)
			latencyResult, err := s.queryProm(ctx, latencyQuery)

			resultChan <- promResult{
				prom: quantile,
				vec:  latencyResult,
				err:  err,
			}
		}(quantile)
	}

	// process results, receive one message per prometheus query type
	var err error
	results := []promResult{}
	for i := 0; i < len(promTypes); i++ {
		result := <-resultChan
		if result.err != nil {
			log.Errorf("queryProm failed with: %s", result.err)
			err = result.err
		} else {
			results = append(results, result)
		}
	}
	if err != nil {
		return nil, err
	}

	var resourceType model.LabelName
	switch req.Outbound.(type) {
	case *pb.StatSummaryRequest_FromResource:
		resourceType = "dst_" + promResourceType(req.Selector.Resource)
	case *pb.StatSummaryRequest_ToResource:
		resourceType = promResourceType(req.Selector.Resource)
	default:
		resourceType = promResourceType(req.Selector.Resource)
	}

	return processRequests(results, resourceType), nil
}

func processRequests(results []promResult, resourceType model.LabelName) map[string]*pb.BasicStats {
	basicStats := make(map[string]*pb.BasicStats)

	for _, result := range results {
		for _, sample := range result.vec {
			label := metricToKey(sample.Metric, resourceType)
			if basicStats[label] == nil {
				basicStats[label] = &pb.BasicStats{}
			}

			value := uint64(0)
			if !math.IsNaN(float64(sample.Value)) {
				value = uint64(math.Round(float64(sample.Value)))
			}

			switch result.prom {
			case promRequests:
				switch string(sample.Metric[model.LabelName("classification")]) {
				case "success":
					basicStats[label].SuccessCount = value
				case "failure":
					basicStats[label].FailureCount = value
				}
			case promLatencyP50:
				basicStats[label].LatencyMsP50 = value
			case promLatencyP95:
				basicStats[label].LatencyMsP95 = value
			case promLatencyP99:
				basicStats[label].LatencyMsP99 = value
			}
		}
	}

	return basicStats
}

func metricToKey(metric model.Metric, resourceType model.LabelName) string {
	if resourceType == namespaceLabel ||
		resourceType == "dst_"+namespaceLabel ||
		metric[namespaceLabel] == "" {
		return string(metric[resourceType])
	}

	return fmt.Sprintf("%s/%s",
		metric[namespaceLabel],
		metric[resourceType],
	)
}

func (s *grpcServer) getDeployments(res *pb.Resource) (map[string]metav1.ObjectMeta, map[string]*meshedCount, error) {
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
		return nil, nil, err
	}

	meshedPodCount := make(map[string]*meshedCount)
	deploymentMap := make(map[string]metav1.ObjectMeta)
	for _, deployment := range deployments {
		key, err := cache.MetaNamespaceKeyFunc(deployment)
		if err != nil {
			return nil, nil, err
		}
		deploymentMap[key] = deployment.ObjectMeta

		meshCount, err := s.getMeshedPodCount(deployment.Namespace, deployment)
		if err != nil {
			return nil, nil, err
		}
		meshedPodCount[key] = meshCount
	}

	return deploymentMap, meshedPodCount, nil
}

func (s *grpcServer) getNamespaces(res *pb.Resource) (map[string]metav1.ObjectMeta, map[string]*meshedCount, error) {
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
		return nil, nil, err
	}

	meshedPodCount := make(map[string]*meshedCount)
	namespaceMap := make(map[string]metav1.ObjectMeta)
	for _, namespace := range namespaces {
		key, err := cache.MetaNamespaceKeyFunc(namespace)
		if err != nil {
			return nil, nil, err
		}
		namespaceMap[key] = namespace.ObjectMeta

		meshCount, err := s.getMeshedPodCount(namespace.Name, namespace)
		if err != nil {
			return nil, nil, err
		}
		meshedPodCount[key] = meshCount
	}

	return namespaceMap, meshedPodCount, nil
}

// this takes a long time for namespaces with many pods
func (s *grpcServer) getMeshedPodCount(namespace string, obj runtime.Object) (*meshedCount, error) {
	selector, err := getSelectorFromObject(obj)

	if err != nil {
		return nil, err
	}

	pods, err := s.podLister.Pods(namespace).List(selector)
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

func isInMesh(pod *apiv1.Pod) bool {
	_, ok := pod.Annotations[k8s.ProxyVersionAnnotation]
	return ok
}

func getSelectorFromObject(obj runtime.Object) (labels.Selector, error) {
	switch typed := obj.(type) {
	case *apiv1.Namespace:
		return labels.Everything(), nil

	case *appsv1beta2.Deployment:
		return labels.Set(typed.Spec.Selector.MatchLabels).AsSelector(), nil

	default:
		return nil, fmt.Errorf("Cannot get object selector: %v", obj)
	}
}

func (s *grpcServer) queryProm(ctx context.Context, query string) (model.Vector, error) {
	log.Debugf("Query request: %+v", query)

	// single data point (aka summary) query
	res, err := s.prometheusAPI.Query(ctx, query, time.Time{})
	if err != nil {
		log.Errorf("Query(%+v) failed with: %+v", query, err)
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
