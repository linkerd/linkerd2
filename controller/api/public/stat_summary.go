package public

import (
	"context"
	"fmt"
	"math"
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

type k8sStat struct {
	object   metav1.Object
	podStats *podStats
}

const (
	reqQuery             = "sum(increase(response_total%s[%s])) by (%s, classification, tls)"
	latencyQuantileQuery = "histogram_quantile(%s, sum(irate(response_latency_ms_bucket%s[%s])) by (le, %s))"

	promRequests   = promType("QUERY_REQUESTS")
	promLatencyP50 = promType("0.5")
	promLatencyP95 = promType("0.95")
	promLatencyP99 = promType("0.99")

	namespaceLabel    = model.LabelName("namespace")
	dstNamespaceLabel = model.LabelName("dst_namespace")
)

var promTypes = []promType{promRequests, promLatencyP50, promLatencyP95, promLatencyP99}

type podStats struct {
	inMesh uint64
	total  uint64
	failed uint64
	errors map[string]*pb.PodErrors
}

func (s *grpcServer) StatSummary(ctx context.Context, req *pb.StatSummaryRequest) (*pb.StatSummaryResponse, error) {

	// check for well-formed request
	if req.GetSelector().GetResource() == nil {
		return statSummaryError(req, "StatSummary request missing Selector Resource"), nil
	}

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
		resourcesToQuery = k8s.StatAllResourceTypes
	} else {
		resourcesToQuery = []string{req.Selector.Resource.Type}
	}

	// request stats for the resourcesToQuery, in parallel
	resultChan := make(chan resourceResult)

	for _, resource := range resourcesToQuery {
		statReq := proto.Clone(req).(*pb.StatSummaryRequest)
		statReq.Selector.Resource.Type = resource

		go func() {
			if isNonK8sResourceQuery(statReq.GetSelector().GetResource().GetType()) {
				resultChan <- s.nonK8sResourceQuery(ctx, statReq)
			} else {
				resultChan <- s.k8sResourceQuery(ctx, statReq)
			}
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
				Resource: req.GetSelector().GetResource(),
				Error:    message,
			},
		},
	}
}

func (s *grpcServer) getKubernetesObjectStats(req *pb.StatSummaryRequest) (map[pb.Resource]k8sStat, error) {
	requestedResource := req.GetSelector().GetResource()
	objects, err := s.k8sAPI.GetObjects(requestedResource.Namespace, requestedResource.Type, requestedResource.Name)
	if err != nil {
		return nil, err
	}

	objectMap := map[pb.Resource]k8sStat{}

	for _, object := range objects {
		metaObj, err := meta.Accessor(object)
		if err != nil {
			return nil, err
		}

		key := pb.Resource{
			Name:      metaObj.GetName(),
			Namespace: metaObj.GetNamespace(),
			Type:      requestedResource.GetType(),
		}

		podStats, err := s.getPodStats(object)
		if err != nil {
			return nil, err
		}

		objectMap[key] = k8sStat{
			object:   metaObj,
			podStats: podStats,
		}
	}
	return objectMap, nil
}

func (s *grpcServer) k8sResourceQuery(ctx context.Context, req *pb.StatSummaryRequest) resourceResult {
	k8sObjects, err := s.getKubernetesObjectStats(req)
	if err != nil {
		return resourceResult{res: nil, err: err}
	}

	requestMetrics, err := s.getPrometheusMetrics(ctx, req, req.TimeWindow)
	if err != nil {
		return resourceResult{res: nil, err: err}
	}

	rows := make([]*pb.StatTable_PodGroup_Row, 0)
	keys := getResultKeys(req, k8sObjects, requestMetrics)

	for _, key := range keys {
		objInfo, ok := k8sObjects[key]
		if !ok {
			continue
		}
		k8sResource := objInfo.object
		row := pb.StatTable_PodGroup_Row{
			Resource: &pb.Resource{
				Name:      k8sResource.GetName(),
				Namespace: k8sResource.GetNamespace(),
				Type:      req.GetSelector().GetResource().GetType(),
			},
			TimeWindow: req.TimeWindow,
			Stats:      requestMetrics[key],
		}

		podStat := objInfo.podStats
		row.MeshedPodCount = podStat.inMesh
		row.RunningPodCount = podStat.total
		row.FailedPodCount = podStat.failed
		row.ErrorsByPod = podStat.errors

		rows = append(rows, &row)
	}

	rsp := pb.StatTable{
		Table: &pb.StatTable_PodGroup_{
			PodGroup: &pb.StatTable_PodGroup{
				Rows: rows,
			},
		},
	}

	return resourceResult{res: &rsp, err: nil}
}

func (s *grpcServer) nonK8sResourceQuery(ctx context.Context, req *pb.StatSummaryRequest) resourceResult {
	requestMetrics, err := s.getPrometheusMetrics(ctx, req, req.TimeWindow)
	if err != nil {
		return resourceResult{res: nil, err: err}
	}
	rows := make([]*pb.StatTable_PodGroup_Row, 0)

	for resourceKey, metrics := range requestMetrics {
		resource := resourceKey
		resource.Type = req.GetSelector().GetResource().GetType()

		row := pb.StatTable_PodGroup_Row{
			Resource:   &resource,
			TimeWindow: req.TimeWindow,
			Stats:      metrics,
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
	return resourceResult{res: &rsp, err: nil}
}

func isNonK8sResourceQuery(resourceType string) bool {
	return resourceType == k8s.Authorities
}

// get the list of objects for which we want to return results
func getResultKeys(
	req *pb.StatSummaryRequest,
	k8sObjects map[pb.Resource]k8sStat,
	metricResults map[pb.Resource]*pb.BasicStats,
) []pb.Resource {
	var keys []pb.Resource

	if req.GetOutbound() == nil || req.GetNone() != nil {
		// if the request doesn't have outbound filtering, return all rows
		for key := range k8sObjects {
			keys = append(keys, key)
		}
	} else {
		// if the request does have outbound filtering,
		// only return rows for which we have stats
		for key := range metricResults {
			keys = append(keys, key)
		}
	}
	return keys
}

// add filtering by resource type
// note that metricToKey assumes the label ordering (namespace, name)
func promGroupByLabelNames(resource *pb.Resource) model.LabelNames {
	names := model.LabelNames{namespaceLabel}

	if resource.Type != k8s.Namespaces {
		names = append(names, promResourceType(resource))
	}
	return names
}

// add filtering by resource type
// note that metricToKey assumes the label ordering (namespace, name)
func promDstGroupByLabelNames(resource *pb.Resource) model.LabelNames {
	names := model.LabelNames{dstNamespaceLabel}

	if resource.Type != k8s.Namespaces {
		names = append(names, "dst_"+promResourceType(resource))
	}
	return names
}

// query a named resource
func promQueryLabels(resource *pb.Resource) model.LabelSet {
	set := model.LabelSet{}
	if resource.Name != "" {
		set[promResourceType(resource)] = model.LabelValue(resource.Name)
	}
	if shouldAddNamespaceLabel(resource) {
		set[namespaceLabel] = model.LabelValue(resource.Namespace)
	}
	return set
}

// query a named resource
func promDstQueryLabels(resource *pb.Resource) model.LabelSet {
	set := model.LabelSet{}
	if resource.Name != "" {
		if isNonK8sResourceQuery(resource.GetType()) {
			set[promResourceType(resource)] = model.LabelValue(resource.Name)
		} else {
			set["dst_"+promResourceType(resource)] = model.LabelValue(resource.Name)
			if shouldAddNamespaceLabel(resource) {
				set[dstNamespaceLabel] = model.LabelValue(resource.Namespace)
			}
		}
	}

	return set
}

// determine if we should add "namespace=<namespace>" to a named query
func shouldAddNamespaceLabel(resource *pb.Resource) bool {
	return resource.Type != k8s.Namespaces && resource.Namespace != ""
}

// query for inbound or outbound requests
func promDirectionLabels(direction string) model.LabelSet {
	return model.LabelSet{
		model.LabelName("direction"): model.LabelValue(direction),
	}
}

func promResourceType(resource *pb.Resource) model.LabelName {
	return model.LabelName(k8s.ResourceTypesToProxyLabels[resource.Type])
}

func buildRequestLabels(req *pb.StatSummaryRequest) (labels model.LabelSet, labelNames model.LabelNames) {
	// labelNames: the group by in the prometheus query
	// labels: the labels for the resource we want to query for

	switch out := req.Outbound.(type) {
	case *pb.StatSummaryRequest_ToResource:
		labelNames = promGroupByLabelNames(req.Selector.Resource)

		labels = labels.Merge(promDstQueryLabels(out.ToResource))
		labels = labels.Merge(promQueryLabels(req.Selector.Resource))
		labels = labels.Merge(promDirectionLabels("outbound"))

	case *pb.StatSummaryRequest_FromResource:
		labelNames = promDstGroupByLabelNames(req.Selector.Resource)

		labels = labels.Merge(promQueryLabels(out.FromResource))
		labels = labels.Merge(promDirectionLabels("outbound"))

	default:
		labelNames = promGroupByLabelNames(req.Selector.Resource)

		labels = labels.Merge(promQueryLabels(req.Selector.Resource))
		labels = labels.Merge(promDirectionLabels("inbound"))
	}

	return
}

func (s *grpcServer) getPrometheusMetrics(ctx context.Context, req *pb.StatSummaryRequest, timeWindow string) (map[pb.Resource]*pb.BasicStats, error) {
	reqLabels, groupBy := buildRequestLabels(req)
	resultChan := make(chan promResult)

	// kick off 4 asynchronous queries: 1 request volume + 3 latency
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

	return processPrometheusMetrics(req, results, groupBy), nil
}

func processPrometheusMetrics(req *pb.StatSummaryRequest, results []promResult, groupBy model.LabelNames) map[pb.Resource]*pb.BasicStats {
	basicStats := make(map[pb.Resource]*pb.BasicStats)

	for _, result := range results {
		for _, sample := range result.vec {
			resource := metricToKey(req, sample.Metric, groupBy)

			if basicStats[resource] == nil {
				basicStats[resource] = &pb.BasicStats{}
			}

			value := extractSampleValue(sample)

			switch result.prom {
			case promRequests:
				switch string(sample.Metric[model.LabelName("classification")]) {
				case "success":
					basicStats[resource].SuccessCount += value
				case "failure":
					basicStats[resource].FailureCount += value
				}
				switch string(sample.Metric[model.LabelName("tls")]) {
				case "true":
					basicStats[resource].TlsRequestCount += value
				}
			case promLatencyP50:
				basicStats[resource].LatencyMsP50 = value
			case promLatencyP95:
				basicStats[resource].LatencyMsP95 = value
			case promLatencyP99:
				basicStats[resource].LatencyMsP99 = value
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

func metricToKey(req *pb.StatSummaryRequest, metric model.Metric, groupBy model.LabelNames) pb.Resource {
	// this key is used to match the metric stats we queried from prometheus
	// with the k8s object stats we queried from k8s
	// ASSUMPTION: this code assumes that groupBy is always ordered (..., namespace, name)
	key := pb.Resource{
		Type: req.GetSelector().GetResource().GetType(),
		Name: string(metric[groupBy[len(groupBy)-1]]),
	}

	if len(groupBy) == 2 {
		key.Namespace = string(metric[groupBy[0]])
	}

	return key
}

func (s *grpcServer) getPodStats(obj runtime.Object) (*podStats, error) {
	pods, err := s.k8sAPI.GetPodsFor(obj, true)
	if err != nil {
		return nil, err
	}
	podErrors := make(map[string]*pb.PodErrors)
	meshCount := &podStats{}

	for _, pod := range pods {
		if pod.Status.Phase == apiv1.PodFailed {
			meshCount.failed++
		} else {
			meshCount.total++
			if isInMesh(pod) {
				meshCount.inMesh++
			}
		}

		errors := checkContainerErrors(pod.Status.ContainerStatuses, "conduit-proxy")
		errors = append(errors, checkContainerErrors(pod.Status.InitContainerStatuses, "conduit-init")...)

		if len(errors) > 0 {
			podErrors[pod.Name] = &pb.PodErrors{Errors: errors}
		}
	}
	meshCount.errors = podErrors
	return meshCount, nil
}

func toPodError(container, image, message string) *pb.PodErrors_PodError {
	return &pb.PodErrors_PodError{
		Error: &pb.PodErrors_PodError_Container{
			Container: &pb.PodErrors_PodError_ContainerError{
				Message:   message,
				Container: container,
				Image:     image,
			},
		},
	}
}

func checkContainerErrors(containerStatuses []apiv1.ContainerStatus, containerName string) []*pb.PodErrors_PodError {
	errors := []*pb.PodErrors_PodError{}
	for _, st := range containerStatuses {
		if st.Name == containerName && st.State.Waiting != nil {
			errors = append(errors, toPodError(st.Name, st.Image, st.State.Waiting.Message))

			if st.LastTerminationState.Waiting != nil {
				errors = append(errors, toPodError(st.Name, st.Image, st.LastTerminationState.Waiting.Message))
			}

			if st.LastTerminationState.Terminated != nil {
				errors = append(errors, toPodError(st.Name, st.Image, st.LastTerminationState.Terminated.Message))
			}
		}
	}
	return errors
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
	log.Debugf("Query request:\n\t%+v", query)

	// single data point (aka summary) query
	res, err := s.prometheusAPI.Query(ctx, query, time.Time{})
	if err != nil {
		log.Errorf("Query(%+v) failed with: %+v", query, err)
		return nil, err
	}
	log.Debugf("Query response:\n\t%+v", res)

	if res.Type() != model.ValVector {
		err = fmt.Errorf("Unexpected query result type (expected Vector): %s", res.Type())
		log.Error(err)
		return nil, err
	}

	return res.(model.Vector), nil
}
