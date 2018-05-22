package public

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	proto "github.com/golang/protobuf/proto"
	"github.com/prometheus/common/model"
	"github.com/runconduit/conduit/controller/api/util"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

type promType string
type promResult struct {
	prom promType
	vec  model.Vector
	err  error
}

type resourceResult struct {
	res *pb.StatTable
	err error
}

const (
	reqQuery             = "sum(increase(response_total%s[%s])) by (%s, classification)"
	meshReqQuery         = "sum(increase(response_total%s[%s])) by (%s)"
	latencyQuantileQuery = "histogram_quantile(%s, sum(irate(response_latency_ms_bucket%s[%s])) by (le, %s))"

	promRequests     = promType("QUERY_REQUESTS")
	promMeshRequests = promType("QUERY_MESH_REQUESTS")
	promLatencyP50   = promType("0.5")
	promLatencyP95   = promType("0.95")
	promLatencyP99   = promType("0.99")

	namespaceLabel           = model.LabelName("namespace")
	dstNamespaceLabel        = model.LabelName("dst_namespace")
	conduitControlPlaneNs    = model.LabelName("conduit_io_control_plane_ns")
	dstConduitControlPlaneNs = model.LabelName("dst_conduit_io_control_plane_ns")
)

var controlPlaneNsLabels = []model.LabelName{
	conduitControlPlaneNs,
	dstConduitControlPlaneNs,
}

var promTypes = []promType{promRequests, promMeshRequests, promLatencyP50, promLatencyP95, promLatencyP99}

// resources to query when Resource.Type is "all"
var resourceTypes = []string{k8s.Pods, k8s.Deployments, k8s.ReplicationControllers, k8s.Services}

type podCount struct {
	inMesh uint64
	total  uint64
	failed uint64
}

func (s *grpcServer) StatSummary(ctx context.Context, req *pb.StatSummaryRequest) (*pb.StatSummaryResponse, error) {
	// special case to check for services as outbound only
	if isInvalidServiceRequest(req) {
		return statSummaryError(req, "service only supported as a target on 'from' queries, or as a destination on 'to' queries"), nil
	}

	switch req.Outbound.(type) {
	case *pb.StatSummaryRequest_ToResource:
		if req.Outbound.(*pb.StatSummaryRequest_ToResource).ToResource.Type == k8s.All {
			return statSummaryError(req, "resource type 'all' is not supported as a filter"), nil
		}
	case *pb.StatSummaryRequest_FromResource:
		if req.Outbound.(*pb.StatSummaryRequest_FromResource).FromResource.Type == k8s.All {
			return statSummaryError(req, "resource type 'all' is not supported as a filter"), nil
		}
	}

	statTables := make([]*pb.StatTable, 0)

	var resourcesToQuery []string
	if req.Selector.Resource.Type == k8s.All {
		resourcesToQuery = resourceTypes
	} else {
		resourcesToQuery = []string{req.Selector.Resource.Type}
	}

	// request stats for the resourcesToQuery, in parallel
	resultChan := make(chan resourceResult)

	for _, resource := range resourcesToQuery {
		statReq := proto.Clone(req).(*pb.StatSummaryRequest)
		statReq.Selector.Resource.Type = resource

		go func() {
			resultChan <- s.resourceQuery(ctx, statReq)
		}()
	}

	for i := 0; i < len(resourcesToQuery); i++ {
		result := <-resultChan
		if result.err != nil {
			return nil, util.GRPCError(result.err)
		}
		statTables = append(statTables, result.res)
	}

	rsp := pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: statTables,
			},
		},
	}

	return &rsp, nil
}

func statSummaryError(req *pb.StatSummaryRequest, message string) *pb.StatSummaryResponse {
	return &pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Error{
			Error: &pb.ResourceError{
				Resource: req.Selector.Resource,
				Error:    message,
			},
		},
	}
}

func (s *grpcServer) resourceQuery(ctx context.Context, req *pb.StatSummaryRequest) resourceResult {
	objects, err := s.lister.GetObjects(req.Selector.Resource.Namespace, req.Selector.Resource.Type, req.Selector.Resource.Name)
	if err != nil {
		return resourceResult{res: nil, err: err}
	}

	// TODO: make these one struct:
	// string => {metav1.ObjectMeta, podCount}
	objectMap := map[string]metav1.Object{}
	meshCountMap := map[string]*podCount{}

	for _, object := range objects {
		key, err := cache.MetaNamespaceKeyFunc(object)
		if err != nil {
			return resourceResult{res: nil, err: err}
		}
		metaObj, err := meta.Accessor(object)
		if err != nil {
			return resourceResult{res: nil, err: err}
		}

		objectMap[key] = metaObj

		meshCount, err := s.getMeshedPodCount(object)
		if err != nil {
			return resourceResult{res: nil, err: err}
		}
		meshCountMap[key] = meshCount
	}

	res, err := s.objectQuery(ctx, req, objectMap, meshCountMap)
	if err != nil {
		return resourceResult{res: nil, err: err}
	}

	return resourceResult{res: res, err: nil}
}

func (s *grpcServer) objectQuery(
	ctx context.Context,
	req *pb.StatSummaryRequest,
	objects map[string]metav1.Object,
	meshCount map[string]*podCount,
) (*pb.StatTable, error) {
	rows := make([]*pb.StatTable_PodGroup_Row, 0)

	requestMetrics, err := s.getPrometheusMetrics(ctx, req, req.TimeWindow)
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
				Namespace: resource.GetNamespace(),
				Type:      req.Selector.Resource.Type,
				Name:      resource.GetName(),
			},
			TimeWindow: req.TimeWindow,
			Stats:      requestMetrics[key],
		}

		if count, ok := meshCount[key]; ok {
			row.MeshedPodCount = count.inMesh
			row.RunningPodCount = count.total
			row.FailedPodCount = count.failed
		}

		rows = append(rows, &row)
	}

	rsp := pb.StatTable{
		Table: &pb.StatTable_PodGroup_{
			PodGroup: &pb.StatTable_PodGroup{
				Rows: rows,
			},
		},
	}

	return &rsp, nil
}

func promLabelNames(resource *pb.Resource) model.LabelNames {
	names := model.LabelNames{namespaceLabel}
	if resource.Type != k8s.Namespaces {
		names = append(names, promResourceType(resource))
	}
	return names
}

func promDstLabelNames(resource *pb.Resource) model.LabelNames {
	names := model.LabelNames{dstNamespaceLabel}
	if resource.Type != k8s.Namespaces {
		names = append(names, "dst_"+promResourceType(resource))
	}
	return names
}

func appendControlPlaneLabels(labels model.LabelNames) model.LabelNames {
	// add these labels so that we can tell which traffic is wholly inside the mesh
	return append(labels, controlPlaneNsLabels...)
}

func promLabels(resource *pb.Resource) model.LabelSet {
	set := model.LabelSet{}
	if resource.Name != "" {
		set[promResourceType(resource)] = model.LabelValue(resource.Name)
	}
	if resource.Type != k8s.Namespaces && resource.Namespace != "" {
		set[namespaceLabel] = model.LabelValue(resource.Namespace)
	}
	return set
}

func promDstLabels(resource *pb.Resource) model.LabelSet {
	set := model.LabelSet{}
	if resource.Name != "" {
		set["dst_"+promResourceType(resource)] = model.LabelValue(resource.Name)
	}
	if resource.Type != k8s.Namespaces && resource.Namespace != "" {
		set[dstNamespaceLabel] = model.LabelValue(resource.Namespace)
	}
	return set
}

func promDirectionLabels(direction string) model.LabelSet {
	return model.LabelSet{
		model.LabelName("direction"): model.LabelValue(direction),
	}
}

func promResourceType(resource *pb.Resource) model.LabelName {
	return model.LabelName(k8s.ResourceTypesToProxyLabels[resource.Type])
}

func buildRequestLabels(req *pb.StatSummaryRequest) (labels, dstLabels model.LabelSet, labelNames, dstLabelNames model.LabelNames) {
	// labelNames: the group by in the prometheus query
	// labels: the labels we query prometheus for metrics about
	// dstLabels: set the objects we're interested in as dsts, to determine incoming meshed traffic
	// dstLabelnames: the group by for the dstLabels query
	dstLabels = dstLabels.Merge(promDirectionLabels("outbound"))

	switch out := req.Outbound.(type) {
	case *pb.StatSummaryRequest_ToResource:
		labelNames = promLabelNames(req.Selector.Resource)

		labels = labels.Merge(promDstLabels(out.ToResource))
		labels = labels.Merge(promLabels(req.Selector.Resource))
		labels = labels.Merge(promDirectionLabels("outbound"))

		dstLabels = labels
		dstLabelNames = appendControlPlaneLabels(promLabelNames(req.Selector.Resource))

	case *pb.StatSummaryRequest_FromResource:
		labelNames = promDstLabelNames(req.Selector.Resource)
		labels = labels.Merge(promLabels(out.FromResource))
		labels = labels.Merge(promDirectionLabels("outbound"))

		dstLabels = labels
		dstLabelNames = appendControlPlaneLabels(promDstLabelNames(req.Selector.Resource))

	default:
		labelNames = promLabelNames(req.Selector.Resource)
		labels = labels.Merge(promLabels(req.Selector.Resource))
		labels = labels.Merge(promDirectionLabels("inbound"))

		dstLabels = dstLabels.Merge(promDstLabels(req.Selector.Resource))
		dstLabelNames = appendControlPlaneLabels(promDstLabelNames(req.Selector.Resource))
	}

	return
}

func (s *grpcServer) getPrometheusMetrics(ctx context.Context, req *pb.StatSummaryRequest, timeWindow string) (map[string]*pb.BasicStats, error) {
	reqLabels, dstLabels, groupBy, dstGroupBy := buildRequestLabels(req)
	resultChan := make(chan promResult)

	// kick off 5 asynchronous queries: 2 request volume + 3 latency
	go func() {
		// success/failure counts
		requestsQuery := fmt.Sprintf(reqQuery, reqLabels, timeWindow, groupBy)
		resultVector, err := s.queryProm(ctx, requestsQuery)

		resultChan <- promResult{
			prom: promRequests,
			vec:  resultVector,
			err:  err,
		}
	}()

	go func() {
		// find out what percentage of traffic reaching this target started in the mesh
		// so query with this target as the dst (in outbound), and count requests
		meshRequestsQuery := fmt.Sprintf(meshReqQuery, dstLabels, timeWindow, dstGroupBy)
		resultVector, err := s.queryProm(ctx, meshRequestsQuery)

		resultChan <- promResult{
			prom: promMeshRequests,
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
			log.Errorf("queryProm failed with: %s", result.err)
			err = result.err
		} else {
			results = append(results, result)
		}
	}
	if err != nil {
		return nil, err
	}

	stats := processPrometheusMetrics(results, groupBy, dstGroupBy)

	// add in the results from the dst queries, after we've constructed our result map
	// note that we coudl store the results in a map[promType]result instead to
	// avoid having to loop through results again
	for _, result := range results {
		for _, sample := range result.vec {
			if result.prom == promMeshRequests {
				dstLabel := metricToKey(sample.Metric, dstGroupBy)

				// check if traffic started and ended in the mesh
				if sample.Metric[conduitControlPlaneNs] == sample.Metric[dstConduitControlPlaneNs] {
					if basicStats, ok := stats[dstLabel]; ok {
						value := extractSampleValue(sample)
						basicStats.IntraMeshRequestCount = value
					}
				}
			}
		}
	}

	return stats, nil
}

func processPrometheusMetrics(results []promResult, groupBy model.LabelNames, dstGroupBy model.LabelNames) map[string]*pb.BasicStats {
	basicStats := make(map[string]*pb.BasicStats)

	for _, result := range results {
		for _, sample := range result.vec {
			label := metricToKey(sample.Metric, groupBy)
			if basicStats[label] == nil {
				basicStats[label] = &pb.BasicStats{}
			}

			value := extractSampleValue(sample)

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

func extractSampleValue(sample *model.Sample) uint64 {
	value := uint64(0)
	if !math.IsNaN(float64(sample.Value)) {
		value = uint64(math.Round(float64(sample.Value)))
	}
	return value
}

func metricToKey(metric model.Metric, groupBy model.LabelNames) string {
	// this needs to match keys generated by MetaNamespaceKeyFunc
	values := []string{}
	for i, k := range groupBy {
		if i < 2 { // need to return namespace/resourceType
			values = append(values, string(metric[k]))
		}
	}
	return strings.Join(values, "/")
}

func (s *grpcServer) getMeshedPodCount(obj runtime.Object) (*podCount, error) {
	pods, err := s.lister.GetPodsFor(obj, true)
	if err != nil {
		return nil, err
	}

	meshCount := &podCount{}
	for _, pod := range pods {
		if pod.Status.Phase == apiv1.PodFailed {
			meshCount.failed++
		} else {
			meshCount.total++
			if isInMesh(pod) {
				meshCount.inMesh++
			}
		}
	}

	return meshCount, nil
}

func isInMesh(pod *apiv1.Pod) bool {
	_, ok := pod.Annotations[k8s.ProxyVersionAnnotation]
	return ok
}

func isInvalidServiceRequest(req *pb.StatSummaryRequest) bool {
	fromResource := req.GetFromResource()
	if fromResource != nil {
		return fromResource.Type == k8s.Services
	} else {
		return req.Selector.Resource.Type == k8s.Services
	}
}

func (s *grpcServer) queryProm(ctx context.Context, query string) (model.Vector, error) {
	log.Debugf("Query request:\n %+v", query)

	// single data point (aka summary) query
	res, err := s.prometheusAPI.Query(ctx, query, time.Time{})
	if err != nil {
		log.Errorf("Query(%+v) failed with: %+v", query, err)
		return nil, err
	}
	log.Debugf("Query response:\n %+v", res)

	if res.Type() != model.ValVector {
		err = fmt.Errorf("Unexpected query result type (expected Vector): %s", res.Type())
		log.Error(err)
		return nil, err
	}

	return res.(model.Vector), nil
}
