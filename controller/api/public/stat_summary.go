package public

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	apiUtil "github.com/runconduit/conduit/controller/api/util"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
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
	reqQuery             = "sum(increase(response_total{%s}[%s])) by (%s, namespace, classification)"
	latencyQuantileQuery = "histogram_quantile(%s, sum(irate(response_latency_ms_bucket{%s}[%s])) by (le, namespace, %s))"

	promRequests   = promType("QUERY_REQUESTS")
	promLatencyP50 = promType("0.5")
	promLatencyP95 = promType("0.95")
	promLatencyP99 = promType("0.99")

	namespaceLabel = "namespace"
)

var (
	promTypes = []promType{promRequests, promLatencyP50, promLatencyP95, promLatencyP99}

	k8sResourceTypesToPromLabels = map[string]string{
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

	timeWindow, err := apiUtil.GetWindowString(req.TimeWindow)
	if err != nil {
		return nil, err
	}

	deployments, meshCount, err := s.getDeployments(req.Selector.Resource)
	if err != nil {
		return nil, err
	}

	requestLabels := buildRequestLabels(req)
	groupBy := promResourceType(req.Selector.Resource)

	requestMetrics, err := s.getRequests(ctx, requestLabels, groupBy, timeWindow)
	if err != nil {
		return nil, err
	}

	for _, resource := range deployments {
		key, err := cache.MetaNamespaceKeyFunc(resource)
		if err != nil {
			return nil, err
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

func promLabel(key, val string) string {
	return fmt.Sprintf("%s=\"%s\"", key, val)
}

func promNameLabel(resource *pb.Resource) string {
	return promLabel(promResourceType(resource), resource.Name)
}

func promNamespaceLabel(resource *pb.Resource) string {
	return promLabel(namespaceLabel, resource.Namespace)
}

func promDstLabels(resource *pb.Resource) []string {
	return []string{
		promLabel("dst_"+namespaceLabel, resource.Namespace),
		promLabel("dst_"+promResourceType(resource), resource.Name),
	}
}

func promResourceType(resource *pb.Resource) string {
	return k8sResourceTypesToPromLabels[resource.Type]
}

func buildRequestLabels(req *pb.StatSummaryRequest) string {
	labels := []string{}

	var direction string
	switch req.Outbound.(type) {
	case *pb.StatSummaryRequest_OutToResource:
		direction = "outbound"
		labels = append(labels, promDstLabels(req.GetOutToResource())...)

	case *pb.StatSummaryRequest_OutFromResource:
		direction = "outbound"
		labels = append(labels, promDstLabels(req.Selector.Resource)...)

		outFromNs := req.GetOutFromResource().Namespace
		if outFromNs == "" {
			outFromNs = req.Selector.Resource.Namespace
		}

		if outFromNs != "" {
			labels = append(labels, promLabel(namespaceLabel, outFromNs))
		}
		if req.Selector.Resource.Name != "" {
			labels = append(labels, promNameLabel(req.GetOutFromResource()))
		}

	default:
		direction = "inbound"
	}

	// it's weird to check this again outside the switch, but including this code
	// in the other three switch branches is very repetitive
	if req.GetOutFromResource() == nil {
		if req.Selector.Resource.Namespace != "" {
			labels = append(labels, promNamespaceLabel(req.Selector.Resource))
		}
		if req.Selector.Resource.Name != "" {
			labels = append(labels, promNameLabel(req.Selector.Resource))
		}
	}
	labels = append(labels, promLabel("direction", direction))

	return strings.Join(labels, ",")
}

func (s *grpcServer) getRequests(ctx context.Context, reqLabels string, groupBy string, timeWindow string) (map[string]*pb.BasicStats, error) {
	resultChan := make(chan promResult)

	// kick off 4 asynchronous queries: 1 request volume + 3 latency
	go func() {
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
			log.Errorf("queryProm failed with: %s", err)
			err = result.err
		} else {
			results = append(results, result)
		}
	}
	if err != nil {
		return nil, err
	}

	return processRequests(results, groupBy), nil
}

func processRequests(results []promResult, labelSelector string) map[string]*pb.BasicStats {
	basicStats := make(map[string]*pb.BasicStats)

	for _, result := range results {
		for _, sample := range result.vec {
			label := metricToKey(sample.Metric, labelSelector)
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

func metricToKey(metric model.Metric, labelSelector string) string {
	if metric[model.LabelName(namespaceLabel)] == "" {
		return string(metric[model.LabelName(labelSelector)])
	}

	return fmt.Sprintf("%s/%s",
		metric[model.LabelName(namespaceLabel)],
		metric[model.LabelName(labelSelector)],
	)
}

func (s *grpcServer) getDeployments(res *pb.Resource) ([]*appsv1.Deployment, map[string]*meshedCount, error) {
	var err error
	var deployments []*appsv1.Deployment

	if res.Namespace == "" {
		deployments, err = s.deployLister.List(labels.Everything())
	} else if res.Name == "" {
		deployments, err = s.deployLister.Deployments(res.Namespace).List(labels.Everything())
	} else {
		var deployment *appsv1.Deployment
		deployment, err = s.deployLister.Deployments(res.Namespace).Get(res.Name)
		deployments = []*appsv1.Deployment{deployment}
	}

	if err != nil {
		return nil, nil, err
	}

	meshedPodCount := make(map[string]*meshedCount)
	for _, deployment := range deployments {
		meshCount, err := s.getMeshedPodCount(deployment.Namespace, deployment)
		if err != nil {
			return nil, nil, err
		}
		key, err := cache.MetaNamespaceKeyFunc(deployment)
		if err != nil {
			return nil, nil, err
		}
		meshedPodCount[key] = meshCount
	}

	return deployments, meshedPodCount, nil
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
	case *appsv1.Deployment:
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
