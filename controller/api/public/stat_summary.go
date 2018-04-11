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
)

type promType string
type promResult struct {
	prom promType
	vec  model.Vector
	err  error
}

const (
	reqQuery             = "sum(increase(response_total{%s}[%s])) by (%s, classification)"
	latencyQuantileQuery = "histogram_quantile(%s, sum(irate(response_latency_ms_bucket{%s}[%s])) by (le, %s))"

	promRequests   = promType("QUERY_REQUESTS")
	promLatencyP50 = promType("0.5")
	promLatencyP95 = promType("0.95")
	promLatencyP99 = promType("0.99")
)

var (
	promTypes = []promType{promRequests, promLatencyP50, promLatencyP95, promLatencyP99}

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
	deployments := make([]*appsv1.Deployment, 0)
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
			label := string(sample.Metric[model.LabelName(labelSelector)])
			if basicStats[label] == nil {
				basicStats[label] = &pb.BasicStats{}
			}

			value := uint64(math.Round(float64(sample.Value)))

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

func (s *grpcServer) getDeployment(namespace string, name string) ([]*appsv1.Deployment, map[string]*meshedCount, error) {
	if namespace == "" {
		namespace = apiv1.NamespaceDefault
	}

	deployment, err := s.deployLister.Deployments(namespace).Get(name)
	if err != nil {
		return nil, nil, err
	}

	meshCount, err := s.getMeshedPodCount(namespace, deployment)
	if err != nil {
		return nil, nil, err
	}
	meshMap := map[string]*meshedCount{deployment.Name: meshCount}
	return []*appsv1.Deployment{deployment}, meshMap, nil
}

func (s *grpcServer) getDeployments(namespace string) ([]*appsv1.Deployment, map[string]*meshedCount, error) {
	if namespace == "" {
		namespace = apiv1.NamespaceDefault
	}

	deployments, err := s.deployLister.Deployments(namespace).List(labels.Everything())
	if err != nil {
		return nil, nil, err
	}

	meshedPodCount := make(map[string]*meshedCount)
	for _, deployment := range deployments {
		// TODO: parallelize
		meshCount, err := s.getMeshedPodCount(namespace, deployment)
		if err != nil {
			return nil, nil, err
		}
		meshedPodCount[deployment.Name] = meshCount
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
