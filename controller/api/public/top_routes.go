package public

import (
	"context"
	"fmt"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
)

const (
	routeReqQuery             = "sum(increase(route_response_total%s[%s])) by (rt_route, classification, tls)"
	routeLatencyQuantileQuery = "histogram_quantile(%s, sum(irate(route_response_latency_ms_bucket%s[%s])) by (le, rt_route))"
)

func (s *grpcServer) TopRoutes(ctx context.Context, req *pb.TopRoutesRequest) (*pb.TopRoutesResponse, error) {

	// check for well-formed request
	if req.GetSelector().GetResource() == nil {
		return topRoutesError(req, "TopRoutes request missing Selector Resource"), nil
	}

	// special case to check for services as outbound only
	if isInvalidServiceRequest(req.Selector, req.GetFromResource()) {
		return topRoutesError(req, "service only supported as a target on 'from' queries, or as a destination on 'to' queries"), nil
	}

	switch req.Outbound.(type) {
	case *pb.TopRoutesRequest_ToResource:
		if req.Outbound.(*pb.TopRoutesRequest_ToResource).ToResource.Type == k8s.All {
			return topRoutesError(req, "resource type 'all' is not supported as a filter"), nil
		}
	case *pb.TopRoutesRequest_FromResource:
		if req.Outbound.(*pb.TopRoutesRequest_FromResource).FromResource.Type == k8s.All {
			return topRoutesError(req, "resource type 'all' is not supported as a filter"), nil
		}
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
	resultChan := make(chan promResult)

	// kick off 4 asynchronous queries: 1 request volume + 3 latency
	go func() {
		// success/failure counts
		requestsQuery := fmt.Sprintf(routeReqQuery, reqLabels, timeWindow)
		resultVector, err := s.queryProm(ctx, requestsQuery)

		resultChan <- promResult{
			prom: promRequests,
			vec:  resultVector,
			err:  err,
		}
	}()

	for _, quantile := range []promType{promLatencyP50, promLatencyP95, promLatencyP99} {
		go func(quantile promType) {
			latencyQuery := fmt.Sprintf(routeLatencyQuantileQuery, quantile, reqLabels, timeWindow)
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

	return processRouteMetrics(results), nil
}

func buildRouteLabels(req *pb.TopRoutesRequest) (labels model.LabelSet) {
	// labelNames: the group by in the prometheus query
	// labels: the labels for the resource we want to query for

	switch out := req.Outbound.(type) {
	case *pb.TopRoutesRequest_ToResource:
		labels = labels.Merge(promDstQueryLabels(out.ToResource))
		labels = labels.Merge(promQueryLabels(req.Selector.Resource))
		labels = labels.Merge(promDirectionLabels("outbound"))

	case *pb.TopRoutesRequest_FromResource:
		labels = labels.Merge(promQueryLabels(out.FromResource))
		labels = labels.Merge(promDirectionLabels("outbound"))

	default:
		labels = labels.Merge(promQueryLabels(req.Selector.Resource))
		labels = labels.Merge(promDirectionLabels("inbound"))
	}

	return
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
