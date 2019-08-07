package public

import (
	"context"
	"fmt"
	"reflect"

	"github.com/deislabs/smi-sdk-go/pkg/apis/split/v1alpha1"
	proto "github.com/golang/protobuf/proto"
	"github.com/linkerd/linkerd2/controller/api/util"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type resourceResult struct {
	res *pb.StatTable
	err error
}

type k8sStat struct {
	object   metav1.Object
	podStats *podStats
	tsStats  *trafficSplitStats
}

type rKey struct {
	Namespace string
	Type      string
	Name      string
}

type tsKey struct {
	Namespace string
	Type      string
	Name      string
	Apex      string
	Leaf      string
}

const (
	success = "success"
	failure = "failure"

	reqQuery             = "sum(increase(response_total%s[%s])) by (%s, classification, tls)"
	latencyQuantileQuery = "histogram_quantile(%s, sum(irate(response_latency_ms_bucket%s[%s])) by (le, %s))"
	tcpConnectionsQuery  = "sum(tcp_open_connections%s) by (%s)"
	tcpReadBytesQuery    = "sum(increase(tcp_read_bytes_total%s[%s])) by (%s)"
	tcpWriteBytesQuery   = "sum(increase(tcp_write_bytes_total%s[%s])) by (%s)"
)

type podStats struct {
	status string
	inMesh uint64
	total  uint64
	failed uint64
	errors map[string]*pb.PodErrors
}

type trafficSplitStats struct {
	apex   string
	leaves []leaf
}

type leaf struct {
	leafName   string
	leafWeight string
}

func (s *grpcServer) StatSummary(ctx context.Context, req *pb.StatSummaryRequest) (*pb.StatSummaryResponse, error) {

	// check for well-formed request
	if req.GetSelector().GetResource() == nil {
		return statSummaryError(req, "StatSummary request missing Selector Resource"), nil
	}

	// special case to check for services as outbound only
	if isInvalidServiceRequest(req.Selector, req.GetFromResource()) {
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
			} else if isTrafficSplitQuery(statReq.GetSelector().GetResource().GetType()) {
				resultChan <- s.trafficSplitResourceQuery(ctx, statReq)
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

func isInvalidServiceRequest(selector *pb.ResourceSelection, fromResource *pb.Resource) bool {
	if fromResource != nil {
		return fromResource.Type == k8s.Service
	}

	return selector.Resource.Type == k8s.Service
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

func (s *grpcServer) getKubernetesObjectStats(req *pb.StatSummaryRequest) (map[rKey]k8sStat, error) {
	requestedResource := req.GetSelector().GetResource()
	objects, err := s.k8sAPI.GetObjects(requestedResource.Namespace, requestedResource.Type, requestedResource.Name)
	if err != nil {
		return nil, err
	}

	objectMap := map[rKey]k8sStat{}

	for _, object := range objects {
		metaObj, err := meta.Accessor(object)
		if err != nil {
			return nil, err
		}

		key := rKey{
			Name:      metaObj.GetName(),
			Namespace: metaObj.GetNamespace(),
			Type:      requestedResource.GetType(),
		}

		var tsStats *trafficSplitStats
		if requestedResource.GetType() == k8s.TrafficSplit {
			tsStats = unpackTrafficSplitInfo(object)
		}

		podStats, err := s.getPodStats(object)
		if err != nil {
			return nil, err
		}

		objectMap[key] = k8sStat{
			object:   metaObj,
			podStats: podStats,
			tsStats:  tsStats,
		}
	}
	return objectMap, nil
}

func unpackTrafficSplitInfo(object runtime.Object) *trafficSplitStats {
	tsInfo := object.(*v1alpha1.TrafficSplit)
	apex := tsInfo.Spec.Service
	backends := tsInfo.Spec.Backends
	tsStats := &trafficSplitStats{apex: apex, leaves: []leaf{}}

	for _, returnedLeaf := range backends {
		leafName := fmt.Sprint(returnedLeaf.Service)
		weight := fmt.Sprint(returnedLeaf.Weight.String())

		tsStats.leaves = append(tsStats.leaves, leaf{
			leafName:   leafName,
			leafWeight: weight})
	}
	return tsStats
}

func (s *grpcServer) k8sResourceQuery(ctx context.Context, req *pb.StatSummaryRequest) resourceResult {
	k8sObjects, err := s.getKubernetesObjectStats(req)
	if err != nil {
		return resourceResult{res: nil, err: err}
	}

	var requestMetrics map[rKey]*pb.BasicStats
	var tcpMetrics map[rKey]*pb.TcpStats
	if !req.SkipStats {
		requestMetrics, tcpMetrics, err = s.getStatMetrics(ctx, req, req.TimeWindow)
		if err != nil {
			return resourceResult{res: nil, err: err}
		}
	}

	rows := make([]*pb.StatTable_PodGroup_Row, 0)
	keys := getResultKeys(req, k8sObjects, requestMetrics)

	for _, key := range keys {
		objInfo, ok := k8sObjects[key]
		if !ok {
			continue
		}

		var tcpStats *pb.TcpStats
		if req.TcpStats {
			tcpStats = tcpMetrics[key]
		}

		var basicStats *pb.BasicStats
		if !reflect.DeepEqual(requestMetrics[key], &pb.BasicStats{}) {
			basicStats = requestMetrics[key]
		}

		k8sResource := objInfo.object
		row := pb.StatTable_PodGroup_Row{
			Resource: &pb.Resource{
				Name:      k8sResource.GetName(),
				Namespace: k8sResource.GetNamespace(),
				Type:      req.GetSelector().GetResource().GetType(),
			},
			TimeWindow: req.TimeWindow,
			Stats:      basicStats,
			TcpStats:   tcpStats,
		}

		podStat := objInfo.podStats
		row.Status = podStat.status
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

// because we need the leaf service and apex in order to match k8sObjects with prometheus metrics for trafficsplits,
// we create trafficSplitResourceQuery specifically for trafficsplit tables.
func (s *grpcServer) trafficSplitResourceQuery(ctx context.Context, req *pb.StatSummaryRequest) resourceResult {
	k8sObjects, err := s.getKubernetesObjectStats(req)

	if err != nil {
		return resourceResult{res: nil, err: err}
	}

	var tsBasicStats map[tsKey]*pb.BasicStats
	if !req.SkipStats {
		tsBasicStats, err = s.getTrafficSplitMetrics(ctx, req, k8sObjects, req.TimeWindow)

		if err != nil {
			return resourceResult{res: nil, err: err}
		}
	}

	rows := make([]*pb.StatTable_PodGroup_Row, 0)
	tsKeys := getTrafficSplitResultKeys(req, k8sObjects, tsBasicStats)

	for _, object := range k8sObjects {
		trafficSplit := object.tsStats

		for _, leaf := range trafficSplit.leaves {
			for _, tsKey := range tsKeys {
				if leaf.leafName == tsKey.Leaf {
					var basicStats *pb.BasicStats
					if !reflect.DeepEqual(tsBasicStats[tsKey], &pb.BasicStats{}) {
						basicStats = tsBasicStats[tsKey]
					}

					tsStats := &pb.TrafficSplitStats{
						Apex:   tsKey.Apex,
						Leaf:   leaf.leafName,
						Weight: leaf.leafWeight,
					}

					k8sResource := object.object
					row := pb.StatTable_PodGroup_Row{
						Resource: &pb.Resource{
							Name:      k8sResource.GetName(),
							Namespace: k8sResource.GetNamespace(),
							Type:      req.GetSelector().GetResource().GetType(),
						},
						TimeWindow: req.TimeWindow,
						Stats:      basicStats,
						TsStats:    tsStats,
					}

					podStat := object.podStats
					row.Status = podStat.status
					row.MeshedPodCount = podStat.inMesh
					row.RunningPodCount = podStat.total
					row.FailedPodCount = podStat.failed
					row.ErrorsByPod = podStat.errors

					rows = append(rows, &row)

				}
			}
		}
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
	var requestMetrics map[rKey]*pb.BasicStats
	if !req.SkipStats {
		var err error
		requestMetrics, _, err = s.getStatMetrics(ctx, req, req.TimeWindow)
		if err != nil {
			return resourceResult{res: nil, err: err}
		}
	}
	rows := make([]*pb.StatTable_PodGroup_Row, 0)

	for rkey, metrics := range requestMetrics {
		rkey.Type = req.GetSelector().GetResource().GetType()

		row := pb.StatTable_PodGroup_Row{
			Resource: &pb.Resource{
				Type:      rkey.Type,
				Namespace: rkey.Namespace,
				Name:      rkey.Name,
			},
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
	return resourceType == k8s.Authority
}

func isTrafficSplitQuery(resourceType string) bool {
	return resourceType == k8s.TrafficSplit
}

// get the list of objects for which we want to return results
func getResultKeys(
	req *pb.StatSummaryRequest,
	k8sObjects map[rKey]k8sStat,
	metricResults map[rKey]*pb.BasicStats,
) []rKey {
	var keys []rKey

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

func getTrafficSplitResultKeys(req *pb.StatSummaryRequest, k8sObjects map[rKey]k8sStat, metricResults map[tsKey]*pb.BasicStats) []tsKey {
	var trafficSplitKeys []tsKey

	if req.GetOutbound() == nil || req.GetNone() != nil {
		for key := range k8sObjects {

			objInfo := k8sObjects[key]

			leafServices := objInfo.tsStats.leaves
			apex := objInfo.tsStats.apex

			for _, leaf := range leafServices {
				newTsKey := tsKey{
					Name:      key.Name,
					Namespace: key.Namespace,
					Type:      key.Type,
					Leaf:      leaf.leafName,
					Apex:      apex,
				}
				trafficSplitKeys = append(trafficSplitKeys, newTsKey)
			}
		}
	} else {
		// if the request does have outbound filtering,
		// only return rows for which we have stats
		for key := range metricResults {
			trafficSplitKeys = append(trafficSplitKeys, key)
		}
	}
	return trafficSplitKeys
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
		labels = labels.Merge(promDstQueryLabels(req.Selector.Resource))
		labels = labels.Merge(promDirectionLabels("outbound"))

	default:
		labelNames = promGroupByLabelNames(req.Selector.Resource)
		labels = labels.Merge(promQueryLabels(req.Selector.Resource))

		if req.Selector.Resource.Type == k8s.TrafficSplit {
			labels = labels.Merge(promQueryLabels(req.Selector.Resource))
			labels = labels.Merge(promDirectionLabels("outbound"))
		} else {
			labels = labels.Merge(promDirectionLabels("inbound"))
		}
	}

	if req.Selector.Resource.Type == k8s.TrafficSplit {
		labelNames[1] = model.LabelName("dst_service") // replacing "trafficsplit" with "dst_service"
	}
	return labels, labelNames
}

func (s *grpcServer) getStatMetrics(ctx context.Context, req *pb.StatSummaryRequest, timeWindow string) (map[rKey]*pb.BasicStats, map[rKey]*pb.TcpStats, error) {
	reqLabels, groupBy := buildRequestLabels(req)
	promQueries := map[promType]string{
		promRequests: reqQuery,
	}

	if req.TcpStats {
		promQueries[promTCPConnections] = tcpConnectionsQuery
		promQueries[promTCPReadBytes] = tcpReadBytesQuery
		promQueries[promTCPWriteBytes] = tcpWriteBytesQuery
	}

	results, err := s.getPrometheusMetrics(ctx, promQueries, latencyQuantileQuery, reqLabels.String(), timeWindow, groupBy.String())

	if err != nil {
		return nil, nil, err
	}

	basicStats, tcpStats := processPrometheusMetrics(req, results, groupBy)
	return basicStats, tcpStats, nil
}

// when querying Prometheus in getTrafficSplitMetrics, processPrometheusMetrics returns a map[rKey]*pb.BasicStats.
// however, the rKey.Name returned is the leaf service name due to the need to query Prometheus for dst_service,
// whereas the the rKey.Name in the k8sObject data is the trafficsplit name. in order to match the k8sObjects
// to the Prometheus metrics, we return a new map[tsKey]*pb.BasicStats which includes apex and leaf information.
func (s *grpcServer) getTrafficSplitMetrics(ctx context.Context, req *pb.StatSummaryRequest, k8sObjects map[rKey]k8sStat, timeWindow string) (map[tsKey]*pb.BasicStats, error) {

	tsBasicStats := make(map[tsKey]*pb.BasicStats)

	for key, k8sStat := range k8sObjects {
		reqLabels, groupBy := buildRequestLabels(req)

		apex := k8sStat.tsStats.apex
		stringifiedReqLabels := generateLabelStringWithRegex(reqLabels, "dst", apex)

		promQueries := map[promType]string{
			promRequests: reqQuery,
		}

		if req.TcpStats {
			promQueries[promTCPConnections] = tcpConnectionsQuery
			promQueries[promTCPReadBytes] = tcpReadBytesQuery
			promQueries[promTCPWriteBytes] = tcpWriteBytesQuery
		}
		results, err := s.getPrometheusMetrics(ctx, promQueries, latencyQuantileQuery, stringifiedReqLabels, timeWindow, groupBy.String())

		if err != nil {
			return nil, err
		}

		basicStats, _ := processPrometheusMetrics(req, results, groupBy) // we don't need tcpStat info for traffic split

		for basicStatsKey, basicStatsVal := range basicStats {
			tsBasicStats[tsKey{
				Namespace: key.Namespace,
				Name:      key.Name,
				Type:      key.Type,
				Apex:      apex,
				Leaf:      basicStatsKey.Name,
			}] = basicStatsVal
		}
	}
	return tsBasicStats, nil
}

func processPrometheusMetrics(req *pb.StatSummaryRequest, results []promResult, groupBy model.LabelNames) (map[rKey]*pb.BasicStats, map[rKey]*pb.TcpStats) {
	basicStats := make(map[rKey]*pb.BasicStats)
	tcpStats := make(map[rKey]*pb.TcpStats)

	for _, result := range results {
		for _, sample := range result.vec {
			resource := metricToKey(req, sample.Metric, groupBy)

			addBasicStats := func() {
				if basicStats[resource] == nil {
					basicStats[resource] = &pb.BasicStats{}
				}
			}
			addTCPStats := func() {
				if tcpStats[resource] == nil {
					tcpStats[resource] = &pb.TcpStats{}
				}
			}

			value := extractSampleValue(sample)

			switch result.prom {
			case promRequests:
				addBasicStats()
				switch string(sample.Metric[model.LabelName("classification")]) {
				case success:
					basicStats[resource].SuccessCount += value
				case failure:
					basicStats[resource].FailureCount += value
				}
			case promLatencyP50:
				addBasicStats()
				basicStats[resource].LatencyMsP50 = value
			case promLatencyP95:
				addBasicStats()
				basicStats[resource].LatencyMsP95 = value
			case promLatencyP99:
				addBasicStats()
				basicStats[resource].LatencyMsP99 = value
			case promTCPConnections:
				addTCPStats()
				tcpStats[resource].OpenConnections = value
			case promTCPReadBytes:
				addTCPStats()
				tcpStats[resource].ReadBytesTotal = value
			case promTCPWriteBytes:
				addTCPStats()
				tcpStats[resource].WriteBytesTotal = value
			}

		}
	}

	return basicStats, tcpStats
}

func metricToKey(req *pb.StatSummaryRequest, metric model.Metric, groupBy model.LabelNames) rKey {
	// this key is used to match the metric stats we queried from prometheus
	// with the k8s object stats we queried from k8s
	// ASSUMPTION: this code assumes that groupBy is always ordered (..., namespace, name)
	key := rKey{
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

	if pod, ok := obj.(*corev1.Pod); ok {
		meshCount.status = k8s.GetPodStatus(*pod)
	}

	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodFailed {
			meshCount.failed++
		} else {
			meshCount.total++
			if k8s.IsMeshed(pod, s.controllerNamespace) {
				meshCount.inMesh++
			}
		}

		errors := checkContainerErrors(pod.Status.ContainerStatuses)
		errors = append(errors, checkContainerErrors(pod.Status.InitContainerStatuses)...)

		if len(errors) > 0 {
			podErrors[pod.Name] = &pb.PodErrors{Errors: errors}
		}
	}
	meshCount.errors = podErrors
	return meshCount, nil
}

func toPodError(container, image, reason, message string) *pb.PodErrors_PodError {
	return &pb.PodErrors_PodError{
		Error: &pb.PodErrors_PodError_Container{
			Container: &pb.PodErrors_PodError_ContainerError{
				Message:   message,
				Container: container,
				Image:     image,
				Reason:    reason,
			},
		},
	}
}

func checkContainerErrors(containerStatuses []corev1.ContainerStatus) []*pb.PodErrors_PodError {
	errors := []*pb.PodErrors_PodError{}
	for _, st := range containerStatuses {
		if !st.Ready {
			if st.State.Waiting != nil {
				errors = append(errors, toPodError(st.Name, st.Image, st.State.Waiting.Reason, st.State.Waiting.Message))
			}

			if st.State.Terminated != nil && (st.State.Terminated.ExitCode != 0 || st.State.Terminated.Signal != 0) {
				errors = append(errors, toPodError(st.Name, st.Image, st.State.Terminated.Reason, st.State.Terminated.Message))
			}

			if st.LastTerminationState.Waiting != nil {
				errors = append(errors, toPodError(st.Name, st.Image, st.LastTerminationState.Waiting.Reason, st.LastTerminationState.Waiting.Message))
			}

			if st.LastTerminationState.Terminated != nil {
				errors = append(errors, toPodError(st.Name, st.Image, st.LastTerminationState.Terminated.Reason, st.LastTerminationState.Terminated.Message))
			}
		}
	}
	return errors
}
