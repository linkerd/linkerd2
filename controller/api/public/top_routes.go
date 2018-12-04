package public

import (
	"context"
	"fmt"
	"sort"
	"strings"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/common/model"
)

const (
	routeReqQuery             = "sum(increase(route_response_total%s[%s])) by (%s, classification, tls)"
	routeLatencyQuantileQuery = "histogram_quantile(%s, sum(irate(route_response_latency_ms_bucket%s[%s])) by (le, %s))"
	dstLabel                  = `dst=~"%s(:\\d+)?"`
)

func (s *grpcServer) TopRoutes(ctx context.Context, req *pb.TopRoutesRequest) (*pb.TopRoutesResponse, error) {

	// check for well-formed request
	if req.GetSelector().GetResource() == nil {
		return topRoutesError(req, "TopRoutes request missing Selector Resource"), nil
	}

	table, err := s.routeResourceQuery(ctx, req)
	if err != nil {
		return nil, err
	}

	return &pb.TopRoutesResponse{
		Response: &pb.TopRoutesResponse_Routes{
			Routes: table,
		},
	}, nil
}

func topRoutesError(req *pb.TopRoutesRequest, message string) *pb.TopRoutesResponse {
	return &pb.TopRoutesResponse{
		Response: &pb.TopRoutesResponse_Error{
			Error: &pb.ResourceError{
				Resource: req.GetSelector().GetResource(),
				Error:    message,
			},
		},
	}
}

func (s *grpcServer) routeResourceQuery(ctx context.Context, req *pb.TopRoutesRequest) (*pb.RouteTable, error) {
	routeMetrics, err := s.getRouteMetrics(ctx, req, req.TimeWindow)
	if err != nil {
		return nil, err
	}
	rows := make([]*pb.RouteTable_Row, 0)

	for route, metrics := range routeMetrics {

		row := pb.RouteTable_Row{
			Route:      route,
			TimeWindow: req.TimeWindow,
			Stats:      metrics,
		}
		rows = append(rows, &row)
	}

	rsp := &pb.RouteTable{
		Rows: rows,
	}
	return rsp, nil
}

func (s *grpcServer) getRouteMetrics(ctx context.Context, req *pb.TopRoutesRequest, timeWindow string) (map[string]*pb.BasicStats, error) {
	reqLabels := buildRouteLabels(req)
	groupBy := "rt_route"

	results, err := s.getPrometheusMetrics(ctx, routeReqQuery, routeLatencyQuantileQuery, reqLabels, timeWindow, groupBy)
	if err != nil {
		return nil, err
	}

	return processRouteMetrics(results), nil
}

func buildRouteLabels(req *pb.TopRoutesRequest) string {
	// labels: the labels for the resource we want to query for
	var labels model.LabelSet

	switch out := req.Outbound.(type) {

	case *pb.TopRoutesRequest_ToService:
		labels = labels.Merge(promQueryLabels(req.Selector.Resource))
		labels = labels.Merge(promDirectionLabels("outbound"))
		return renderLabels(labels, out.ToService)

	case *pb.TopRoutesRequest_ToAll:
		labels = labels.Merge(promQueryLabels(req.Selector.Resource))
		labels = labels.Merge(promDirectionLabels("outbound"))
		return renderLabels(labels, "")

	default:
		labels = labels.Merge(promDirectionLabels("inbound"))

		if req.Selector.Resource.GetType() == k8s.Service {
			service := fmt.Sprintf("%s.%s.svc.cluster.local", req.Selector.Resource.GetName(), req.Selector.Resource.GetNamespace())
			return renderLabels(labels, service)
		}

		labels = labels.Merge(promQueryLabels(req.Selector.Resource))
		return renderLabels(labels, "")
	}
}

func renderLabels(labels model.LabelSet, service string) string {
	pairs := make([]string, 0)
	for k, v := range labels {
		pairs = append(pairs, fmt.Sprintf("%s=%q", k, v))
	}
	if service != "" {
		pairs = append(pairs, fmt.Sprintf(dstLabel, service))
	}
	sort.Strings(pairs)
	return fmt.Sprintf("{%s}", strings.Join(pairs, ", "))
}

func processRouteMetrics(results []promResult) map[string]*pb.BasicStats {
	routeStats := make(map[string]*pb.BasicStats)

	for _, result := range results {
		for _, sample := range result.vec {

			route := string(sample.Metric[model.LabelName("rt_route")])

			if routeStats[route] == nil {
				routeStats[route] = &pb.BasicStats{}
			}

			value := extractSampleValue(sample)

			switch result.prom {
			case promRequests:
				switch string(sample.Metric[model.LabelName("classification")]) {
				case "success":
					routeStats[route].SuccessCount += value
				case "failure":
					routeStats[route].FailureCount += value
				}
				switch string(sample.Metric[model.LabelName("tls")]) {
				case "true":
					routeStats[route].TlsRequestCount += value
				}
			case promLatencyP50:
				routeStats[route].LatencyMsP50 = value
			case promLatencyP95:
				routeStats[route].LatencyMsP95 = value
			case promLatencyP99:
				routeStats[route].LatencyMsP99 = value
			}
		}
	}

	return routeStats
}
