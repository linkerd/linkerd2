package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/pkg/prometheus"
	"github.com/prometheus/common/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	httpAuthzDenyQuery  = "sum(increase(inbound_http_authz_deny_total%s[%s])) by (%s)"
	httpAuthzAllowQuery = "sum(increase(inbound_http_authz_allow_total%s[%s])) by (%s)"
)

func isPolicyResource(resource *pb.Resource) bool {
	if resource != nil {
		if resource.GetType() == k8s.Server ||
			resource.GetType() == k8s.ServerAuthorization ||
			resource.GetType() == k8s.AuthorizationPolicy ||
			resource.GetType() == k8s.HTTPRoute {
			return true
		}
	}
	return false
}

func (s *grpcServer) policyResourceQuery(ctx context.Context, req *pb.StatSummaryRequest) resourceResult {

	policyResources, err := s.getPolicyResourceKeys(req)
	if err != nil {
		return resourceResult{res: nil, err: err}
	}

	var requestMetrics map[rKey]*pb.BasicStats
	var tcpMetrics map[rKey]*pb.TcpStats
	var authzMetrics map[rKey]*pb.ServerStats
	if !req.SkipStats {
		requestMetrics, tcpMetrics, authzMetrics, err = s.getPolicyMetrics(ctx, req, req.TimeWindow)
		if err != nil {
			return resourceResult{res: nil, err: err}
		}
	}

	rows := make([]*pb.StatTable_PodGroup_Row, 0)
	for _, key := range policyResources {
		row := pb.StatTable_PodGroup_Row{
			Resource: &pb.Resource{
				Name:      key.Name,
				Namespace: key.Namespace,
				Type:      req.GetSelector().GetResource().GetType(),
			},
			TimeWindow: req.TimeWindow,
			Stats:      requestMetrics[key],
			TcpStats:   tcpMetrics[key],
			SrvStats:   authzMetrics[key],
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

func (s *grpcServer) getPolicyResourceKeys(req *pb.StatSummaryRequest) ([]rKey, error) {
	var err error
	var unstructuredResources *unstructured.UnstructuredList

	// TODO(ver): We should use a typed client
	var gvr schema.GroupVersionResource
	if req.GetSelector().Resource.GetType() == k8s.Server {
		gvr = k8s.ServerGVR
	} else if req.GetSelector().Resource.GetType() == k8s.ServerAuthorization {
		gvr = k8s.SazGVR
	} else if req.GetSelector().Resource.GetType() == k8s.AuthorizationPolicy {
		gvr = k8s.AuthorizationPolicyGVR
	} else if req.GetSelector().Resource.GetType() == k8s.HTTPRoute {
		gvr = k8s.HTTPRouteGVR
	}

	res := req.GetSelector().GetResource()
	labelSelector, err := getLabelSelector(req)
	if err != nil {
		return nil, err
	}

	if res.GetNamespace() == "" {
		unstructuredResources, err = s.k8sAPI.DynamicClient.Resource(gvr).Namespace("").
			List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector.String()})
	} else if res.GetName() == "" {
		unstructuredResources, err = s.k8sAPI.DynamicClient.Resource(gvr).Namespace(res.GetNamespace()).
			List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector.String()})
	} else {
		var ts *unstructured.Unstructured
		ts, err = s.k8sAPI.DynamicClient.Resource(gvr).Namespace(res.GetNamespace()).
			Get(context.TODO(), res.GetName(), metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		unstructuredResources = &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*ts}}
	}
	if err != nil {
		return nil, err
	}

	var resourceKeys []rKey
	for _, resource := range unstructuredResources.Items {
		// Resource Key's type should be singular and lowercased while the kind isn't
		resourceKeys = append(resourceKeys, rKey{
			Namespace: resource.GetNamespace(),
			// TODO(ver) This isn't a reliable way to make a plural name singular.
			Type: strings.ToLower(resource.GetKind()[0:len(resource.GetKind())]),
			Name: resource.GetName(),
		})
	}
	return resourceKeys, nil
}

func (s *grpcServer) getPolicyMetrics(
	ctx context.Context,
	req *pb.StatSummaryRequest,
	timeWindow string,
) (map[rKey]*pb.BasicStats, map[rKey]*pb.TcpStats, map[rKey]*pb.ServerStats, error) {
	labels, groupBy := buildServerRequestLabels(req)
	// These metrics are always inbound.
	reqLabels := labels.Merge(model.LabelSet{
		"direction": model.LabelValue("inbound"),
	})

	promQueries := make(map[promType]string)
	if req.GetSelector().GetResource().GetType() == k8s.Server {
		// TCP metrics are only supported with servers
		if req.TcpStats {
			// peer is always `src` as these are inbound metrics
			tcpLabels := reqLabels.Merge(promPeerLabel("src"))
			promQueries[promTCPConnections] = fmt.Sprintf(tcpConnectionsQuery, tcpLabels.String(), groupBy.String())
			promQueries[promTCPReadBytes] = fmt.Sprintf(tcpReadBytesQuery, tcpLabels.String(), timeWindow, groupBy.String())
			promQueries[promTCPWriteBytes] = fmt.Sprintf(tcpWriteBytesQuery, tcpLabels.String(), timeWindow, groupBy.String())
		}
	}

	promQueries[promRequests] = fmt.Sprintf(reqQuery, reqLabels, timeWindow, groupBy.String())
	// Use `labels` as direction isn't present with authorization metrics
	promQueries[promAllowedRequests] = fmt.Sprintf(httpAuthzAllowQuery, labels, timeWindow, groupBy.String())
	promQueries[promDeniedRequests] = fmt.Sprintf(httpAuthzDenyQuery, labels, timeWindow, groupBy.String())
	quantileQueries := generateQuantileQueries(latencyQuantileQuery, reqLabels.String(), timeWindow, groupBy.String())
	results, err := s.getPrometheusMetrics(ctx, promQueries, quantileQueries)
	if err != nil {
		return nil, nil, nil, err
	}

	basicStats, tcpStats, authzStats := processPrometheusMetrics(req, results, groupBy)
	return basicStats, tcpStats, authzStats, nil
}

func buildServerRequestLabels(req *pb.StatSummaryRequest) (labels model.LabelSet, labelNames model.LabelNames) {
	if req.GetSelector().GetResource().GetNamespace() != "" {
		labels = labels.Merge(model.LabelSet{
			prometheus.NamespaceLabel: model.LabelValue(req.GetSelector().GetResource().GetNamespace()),
		})
	}
	var groupBy model.LabelNames
	if req.GetSelector().GetResource().GetType() == k8s.Server {
		// note that metricToKey assumes the label ordering (..., namespace, name)
		groupBy = model.LabelNames{prometheus.ServerKindLabel, prometheus.NamespaceLabel, prometheus.ServerNameLabel}
		labels = labels.Merge(model.LabelSet{
			prometheus.ServerKindLabel: model.LabelValue("server"),
		})
		if req.GetSelector().GetResource().GetName() != "" {
			labels = labels.Merge(model.LabelSet{
				prometheus.ServerNameLabel: model.LabelValue(req.GetSelector().GetResource().GetName()),
			})
		}
	} else if req.GetSelector().GetResource().GetType() == k8s.ServerAuthorization {
		// note that metricToKey assumes the label ordering (..., namespace, name)
		groupBy = model.LabelNames{prometheus.NamespaceLabel, prometheus.AuthorizationNameLabel}
		labels = labels.Merge(model.LabelSet{
			prometheus.AuthorizationKindLabel: model.LabelValue("serverauthorization"),
		})
		if req.GetSelector().GetResource().GetName() != "" {
			labels = labels.Merge(model.LabelSet{
				prometheus.AuthorizationNameLabel: model.LabelValue(req.GetSelector().GetResource().GetName()),
			})
		}
	} else if req.GetSelector().GetResource().GetType() == k8s.AuthorizationPolicy {
		// note that metricToKey assumes the label ordering (..., namespace, name)
		groupBy = model.LabelNames{prometheus.NamespaceLabel, prometheus.AuthorizationNameLabel}
		labels = labels.Merge(model.LabelSet{
			prometheus.AuthorizationKindLabel: model.LabelValue("authorizationpolicy"),
		})
		if req.GetSelector().GetResource().GetName() != "" {
			labels = labels.Merge(model.LabelSet{
				prometheus.AuthorizationNameLabel: model.LabelValue(req.GetSelector().GetResource().GetName()),
			})
		}
	} else if req.GetSelector().GetResource().GetType() == k8s.HTTPRoute {
		// note that metricToKey assumes the label ordering (..., namespace, name)
		groupBy = model.LabelNames{prometheus.ServerNameLabel, prometheus.ServerKindLabel, prometheus.RouteNameLabel, prometheus.RouteKindLabel, prometheus.NamespaceLabel, prometheus.RouteNameLabel}
		if req.GetSelector().GetResource().GetName() != "" {
			labels = labels.Merge(model.LabelSet{
				prometheus.RouteNameLabel: model.LabelValue(req.GetSelector().GetResource().GetName()),
			})
		}
	}

	switch out := req.Outbound.(type) {
	case *pb.StatSummaryRequest_ToResource:
		// if --to flag is passed, Calculate traffic sent to the policy resource
		// with additional filtering narrowing down to the workload
		// it is sent to.
		labels = labels.Merge(prometheus.QueryLabels(out.ToResource))

	// No FromResource case as policy metrics are all inbound
	default:
		// no extra labels needed
	}

	return labels, groupBy
}
