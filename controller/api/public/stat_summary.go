package public

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/deislabs/smi-sdk-go/pkg/apis/split/v1alpha1"
	proto "github.com/golang/protobuf/proto"
	"github.com/linkerd/linkerd2/controller/api/util"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

type resourceResult struct {
	res *pb.StatTable
	err error
}

type k8sStat struct {
	object   metav1.Object
	podStats *podStats
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
	Weight    string
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
	namespace string
	name      string
	apex      string
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

	kind := req.GetSelector().GetResource().GetType()
	if !isSupportedKind(kind) {
		return nil, util.GRPCError(status.Errorf(codes.Unimplemented, "unimplemented resource type: %s", kind))
	}

	var statTables []*pb.StatTable

	// get stats for authority
	if isNonK8sResourceQuery(kind) || kind == k8s.All {
		clone := proto.Clone(req).(*pb.StatSummaryRequest)
		result := s.nonK8sResourceQuery(ctx, clone)
		if result.err != nil {
			return nil, util.GRPCError(result.err)
		}

		statTables = append(statTables, result.res)
	}

	// get stats for traffic split
	if isTrafficSplitQuery(kind) || kind == k8s.All {
		clone := proto.Clone(req).(*pb.StatSummaryRequest)
		result := s.trafficSplitResourceQuery(ctx, clone)
		if result.err != nil {
			return nil, util.GRPCError(result.err)
		}

		statTables = append(statTables, result.res)
	}

	// get stats for k8s workloads
	if isK8sWorkloadKind(kind) || kind == k8s.All {
		st, err := s.getStatsForK8sKind(ctx, req)
		if err != nil {
			return nil, util.GRPCError(err)
		}

		statTables = append(statTables, st...)
	}

	return statSummaryResponse(statTables), nil
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

func (s *grpcServer) getKubernetesObjectStats(kind, namespace, resourceName string) (map[rKey]k8sStat, error) {
	objects, err := s.k8sAPI.GetObjects(namespace, kind, resourceName)
	if err != nil {
		return nil, err
	}

	objectMap := map[rKey]k8sStat{}
	for _, object := range objects {
		metaObj, err := meta.Accessor(object)
		if err != nil {
			return nil, err
		}

		objKey := rKey{
			Name:      metaObj.GetName(),
			Namespace: metaObj.GetNamespace(),
			Type:      kind,
		}

		podStats, err := s.getPodStats(object)
		if err != nil {
			return nil, err
		}

		objectMap[objKey] = k8sStat{
			object:   metaObj,
			podStats: podStats,
		}
	}
	return objectMap, nil
}

func (s *grpcServer) getStatsForK8sKind(ctx context.Context, req *pb.StatSummaryRequest) ([]*pb.StatTable, error) {
	var (
		statTables []*pb.StatTable
		kinds      []string
	)

	if kind := req.GetSelector().GetResource().GetType(); kind == k8s.All {
		kinds = k8s.StatAllWorkloadKinds
	} else {
		kinds = append(kinds, kind)
	}

	// get object stats from the k8s api server
	k8sObjects := map[rKey]k8sStat{}
	for _, kind := range kinds {
		var (
			name      = req.GetSelector().GetResource().GetName()
			namespace = req.GetSelector().GetResource().GetNamespace()
		)
		objStats, err := s.getKubernetesObjectStats(kind, namespace, name)
		if err != nil {
			return nil, util.GRPCError(err)
		}

		// account for types that don't have object stats,
		// so that empty rows will be rendered for them.
		// only relevant for k8s.All.
		if len(objStats) == 0 {
			k := rKey{Type: kind}
			k8sObjects[k] = k8sStat{}
		}

		for key, stats := range objStats {
			k8sObjects[key] = stats
		}
	}

	// get metrics from prometheus
	var (
		requestMetrics map[rKey]*pb.BasicStats
		tcpMetrics     map[rKey]*pb.TcpStats
		err            error
	)
	if !req.SkipStats {
		requestMetrics, tcpMetrics, err = s.getStatMetrics(ctx, req, req.TimeWindow)
		if err != nil {
			return nil, err
		}
	}

	metricsStatTables := buildMetricsStatTables(req, k8sObjects, requestMetrics, tcpMetrics)
	statTables = append(statTables, metricsStatTables...)
	return statTables, nil
}

func (s *grpcServer) getTrafficSplits(res *pb.Resource) ([]*v1alpha1.TrafficSplit, error) {
	var err error
	var trafficSplits []*v1alpha1.TrafficSplit

	if res.GetNamespace() == "" {
		trafficSplits, err = s.k8sAPI.TS().Lister().List(labels.Everything())
	} else if res.GetName() == "" {
		trafficSplits, err = s.k8sAPI.TS().Lister().TrafficSplits(res.GetNamespace()).List(labels.Everything())
	} else {
		var ts *v1alpha1.TrafficSplit
		ts, err = s.k8sAPI.TS().Lister().TrafficSplits(res.GetNamespace()).Get(res.GetName())
		trafficSplits = []*v1alpha1.TrafficSplit{ts}
	}

	return trafficSplits, err
}

func (s *grpcServer) trafficSplitResourceQuery(ctx context.Context, req *pb.StatSummaryRequest) resourceResult {
	if req.GetSelector().GetResource().GetType() == k8s.All {
		req.GetSelector().GetResource().Type = k8s.TrafficSplit
	}

	tss, err := s.getTrafficSplits(req.GetSelector().GetResource())
	if err != nil {
		return resourceResult{res: nil, err: err}
	}

	tsBasicStats := make(map[tsKey]*pb.BasicStats)
	rows := make([]*pb.StatTable_PodGroup_Row, 0)

	for _, ts := range tss {
		backends := ts.Spec.Backends

		tsStats := &trafficSplitStats{
			namespace: ts.ObjectMeta.Namespace,
			name:      ts.ObjectMeta.Name,
			apex:      ts.Spec.Service,
		}

		if !req.SkipStats {
			tsBasicStats, err = s.getTrafficSplitMetrics(ctx, req, tsStats, req.TimeWindow)
			if err != nil {
				return resourceResult{res: nil, err: err}
			}
		}

		for _, backend := range backends {
			name := backend.Service
			weight := backend.Weight.String()

			currentLeaf := tsKey{
				Namespace: tsStats.namespace,
				Type:      k8s.TrafficSplit,
				Name:      tsStats.name,
				Apex:      tsStats.apex,
				Leaf:      name,
			}

			trafficSplitStats := &pb.TrafficSplitStats{
				Apex:   tsStats.apex,
				Leaf:   name,
				Weight: weight,
			}

			row := pb.StatTable_PodGroup_Row{
				Resource: &pb.Resource{
					Name:      tsStats.name,
					Namespace: tsStats.namespace,
					Type:      req.GetSelector().GetResource().GetType(),
				},
				TimeWindow: req.TimeWindow,
				Stats:      tsBasicStats[currentLeaf],
				TsStats:    trafficSplitStats,
			}
			rows = append(rows, &row)
		}
	}

	// sort rows before returning in order to have a consistent order for tests
	rows = sortTrafficSplitRows(rows)

	rsp := pb.StatTable{
		Table: &pb.StatTable_PodGroup_{
			PodGroup: &pb.StatTable_PodGroup{
				Rows: rows,
			},
		},
	}

	return resourceResult{res: &rsp, err: nil}
}

func sortTrafficSplitRows(rows []*pb.StatTable_PodGroup_Row) []*pb.StatTable_PodGroup_Row {
	sort.Slice(rows, func(i, j int) bool {
		key1 := rows[i].TsStats.Apex + rows[i].TsStats.Leaf
		key2 := rows[j].TsStats.Apex + rows[j].TsStats.Leaf
		return key1 < key2
	})
	return rows
}

func (s *grpcServer) nonK8sResourceQuery(ctx context.Context, req *pb.StatSummaryRequest) resourceResult {
	if req.GetSelector().GetResource().GetType() == k8s.All {
		req.GetSelector().GetResource().Type = k8s.Authority
	}

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

func isSupportedKind(kind string) bool {
	for _, k := range append(k8s.StatAllKinds, k8s.Namespace, k8s.All) {
		if kind == k {
			return true
		}
	}
	return false
}

func isK8sWorkloadKind(kind string) bool {
	for _, k := range append(k8s.StatAllWorkloadKinds, k8s.Namespace) {
		if kind == k {
			return true
		}
	}
	return false
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
		labels = labels.Merge(promDirectionLabels("inbound"))
	}

	return
}

func buildTrafficSplitRequestLabels(req *pb.StatSummaryRequest) (labels model.LabelSet, labelNames model.LabelNames) {
	// trafficsplit labels are always direction="outbound" with an optional namespace="value" if the -A flag is not used.
	// if the --from or --to flags were used, we merge an additional ToResource or FromResource label.
	// trafficsplit metrics results are always grouped by dst_service.
	labels = model.LabelSet{
		"direction": model.LabelValue("outbound"),
	}

	if req.Selector.Resource.Namespace != "" {
		labels["namespace"] = model.LabelValue(req.Selector.Resource.Namespace)
	}

	switch out := req.Outbound.(type) {
	case *pb.StatSummaryRequest_ToResource:
		labels = labels.Merge(promDstQueryLabels(out.ToResource))

	case *pb.StatSummaryRequest_FromResource:
		labels = labels.Merge(promQueryLabels(out.FromResource))

	default:
		// no extra labels needed
	}

	groupBy := model.LabelNames{model.LabelName("dst_service")}

	return labels, groupBy
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

	kind := req.GetSelector().GetResource().GetType()
	var outboundFrom bool
	if req.Outbound != nil {
		_, outboundFrom = req.Outbound.(*pb.StatSummaryRequest_FromResource)
	}

	if kind != k8s.All {
		basicStats, tcpStats := processPrometheusMetrics(kind, outboundFrom, results)
		return basicStats, tcpStats, nil
	}

	basicStats := map[rKey]*pb.BasicStats{}
	tcpStats := map[rKey]*pb.TcpStats{}
	for _, kind := range k8s.StatAllWorkloadKinds {
		basic, tcp := processPrometheusMetrics(kind, outboundFrom, results)
		for resource, stat := range basic {
			basicStats[resource] = stat
		}
		for resource, stat := range tcp {
			tcpStats[resource] = stat
		}
	}

	return basicStats, tcpStats, nil
}

func (s *grpcServer) getTrafficSplitMetrics(ctx context.Context, req *pb.StatSummaryRequest, tsStats *trafficSplitStats, timeWindow string) (map[tsKey]*pb.BasicStats, error) {
	tsBasicStats := make(map[tsKey]*pb.BasicStats)
	labels, groupBy := buildTrafficSplitRequestLabels(req)

	apex := tsStats.apex
	namespace := tsStats.namespace
	// TODO: add cluster domain to stringToMatch
	stringToMatch := fmt.Sprintf("%s.%s.svc", apex, namespace)

	reqLabels := generateLabelStringWithRegex(labels, "authority", stringToMatch)

	promQueries := map[promType]string{
		promRequests: reqQuery,
	}

	results, err := s.getPrometheusMetrics(ctx, promQueries, latencyQuantileQuery, reqLabels, timeWindow, groupBy.String())

	if err != nil {
		return nil, err
	}

	var outboundFrom bool
	if req.Outbound != nil {
		_, outboundFrom = req.Outbound.(*pb.StatSummaryRequest_FromResource)
	}
	basicStats, _ := processPrometheusMetrics(k8s.TrafficSplit, outboundFrom, results) // we don't need tcpStat info for traffic split

	for rKey, basicStatsVal := range basicStats {
		tsBasicStats[tsKey{
			Namespace: namespace,
			Name:      tsStats.name,
			Type:      req.Selector.Resource.Type,
			Apex:      apex,
			Leaf:      rKey.Name,
		}] = basicStatsVal
	}
	return tsBasicStats, nil
}

func processPrometheusMetrics(kind string, outboundFrom bool, results []promResult) (map[rKey]*pb.BasicStats, map[rKey]*pb.TcpStats) {
	basicStats := make(map[rKey]*pb.BasicStats)
	tcpStats := make(map[rKey]*pb.TcpStats)

	for _, result := range results {
		for _, sample := range result.vec {
			resource := metricToKey(kind, outboundFrom, sample.Metric)

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

// metricToKey generates a key which is used to match the metric stats we
// queried from prometheus with the k8s object stats we queried from k8s
func metricToKey(kind string, outboundFrom bool, metric model.Metric) rKey {
	kindLabel := model.LabelName(k8s.KindToStatsLabel(kind, outboundFrom))
	namespaceLabel := model.LabelName(k8s.KindToStatsLabel(k8s.Namespace, outboundFrom))

	// for k8s workloads, `metric` looks like `{<workload-kind>="<workload-name>", namespace="<ns>", pod="<pod-name>"}`.
	// if `<workload-name>` is a key in `metric`, it will be used as `rKey.Name`.
	// otherwise, `rKey.Name` is empty implying that `metric` belongs to the workload kind.
	key := rKey{
		Type: kind,
		Name: string(metric[kindLabel]),
	}

	// don't add a namespace if the resource is a namespace kind OR
	// the resource is an authority kind for outbound stats.
	if kind != k8s.Namespace && !(outboundFrom && isNonK8sResourceQuery(kind)) {
		key.Namespace = string(metric[namespaceLabel])
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

func buildMetricsStatTables(req *pb.StatSummaryRequest, k8sObjects map[rKey]k8sStat, requestMetrics map[rKey]*pb.BasicStats, tcpMetrics map[rKey]*pb.TcpStats) []*pb.StatTable {
	var statTables []*pb.StatTable

	// statTableByKind is used to group statTables by kinds
	statTablesByKind := map[string]*pb.StatTable{}
	for _, key := range getResultKeys(req, k8sObjects, requestMetrics) {
		objInfo := k8sObjects[key]

		// include an empty row for kinds that don't have any object stats
		if objInfo.object == nil {
			log.Debugf("no object stats for %s", key.Type)
			rsp := &pb.StatTable{
				Table: &pb.StatTable_PodGroup_{
					PodGroup: &pb.StatTable_PodGroup{},
				},
			}

			statTables = append(statTables, rsp)
			continue
		}

		var tcpStats *pb.TcpStats
		if req.TcpStats {
			tcpStats = tcpMetrics[key]
		}

		if _, exists := requestMetrics[key]; !exists {
			log.Debugf("no basic stats for -n %s %s/%s", key.Namespace, key.Type, key.Name)
		}

		var basicStats *pb.BasicStats
		if !reflect.DeepEqual(requestMetrics[key], &pb.BasicStats{}) {
			basicStats = requestMetrics[key]
		}

		k8sResource := objInfo.object
		row := &pb.StatTable_PodGroup_Row{
			Resource: &pb.Resource{
				Name:      k8sResource.GetName(),
				Namespace: k8sResource.GetNamespace(),
				Type:      key.Type,
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

		// group all the stat tables by kinds
		if _, exists := statTablesByKind[key.Type]; !exists {
			statTable := &pb.StatTable{
				Table: &pb.StatTable_PodGroup_{
					PodGroup: &pb.StatTable_PodGroup{
						Rows: []*pb.StatTable_PodGroup_Row{row},
					},
				},
			}
			statTablesByKind[key.Type] = statTable
		} else {
			statTablesByKind[key.Type].GetPodGroup().Rows = append(statTablesByKind[key.Type].GetPodGroup().Rows, row)
		}
	}

	// convert the map into a slice that the StatSummaryResponse can use
	for _, statTable := range statTablesByKind {
		statTables = append(statTables, statTable)
	}

	return statTables
}

func statSummaryResponse(statTables []*pb.StatTable) *pb.StatSummaryResponse {
	return &pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: statTables,
			},
		},
	}
}
