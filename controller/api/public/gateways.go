package public

import (
	"context"
	"fmt"

	"github.com/prometheus/common/model"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
)

const (
	gatewayAliveQuery           = "sum(gateway_alive%s) by (%s)"
	numMirroredServicesQuery    = "sum(num_mirrored_services%s) by (%s)"
	gatewayLatencyQuantileQuery = "histogram_quantile(%s, sum(irate(gateway_request_latency_ms_bucket%s[%s])) by (le, %s))"
)

func (s *grpcServer) Gateways(ctx context.Context, req *pb.GatewaysRequest) (*pb.GatewaysResponse, error) {
	array := []*pb.GatewaysTable_Row{}
	metrics, err := s.getGatewaysMetrics(ctx, req, req.TimeWindow)

	if err != nil {
		return nil, err
	}

	for _, v := range metrics {
		array = append(array, v)
	}
	return &pb.GatewaysResponse{
		Response: &pb.GatewaysResponse_Ok_{
			Ok: &pb.GatewaysResponse_Ok{
				GatewaysTable: &pb.GatewaysTable{
					Rows: array,
				},
			},
		},
	}, nil
}

func buildGatewaysRequestLabels(req *pb.GatewaysRequest) (labels model.LabelSet, labelNames model.LabelNames) {
	labels = model.LabelSet{}

	if req.GatewayNamespace != "" {
		labels[gatewayNamespaceLabel] = model.LabelValue(req.GatewayNamespace)
	}

	if req.RemoteClusterName != "" {
		labels[remoteClusterNameLabel] = model.LabelValue(req.RemoteClusterName)
	}

	groupBy := model.LabelNames{gatewayNamespaceLabel, remoteClusterNameLabel, gatewayNameLabel}

	return labels, groupBy
}

func processPrometheusResult(results []promResult) map[string]*pb.GatewaysTable_Row {
	rows := make(map[string]*pb.GatewaysTable_Row)

	for _, result := range results {
		for _, sample := range result.vec {

			clusterName := sample.Metric[remoteClusterNameLabel]
			gatewayName := sample.Metric[gatewayNameLabel]
			gatewayNamespace := sample.Metric[gatewayNamespaceLabel]

			key := fmt.Sprintf("%s-%s-%s", clusterName, gatewayNamespace, gatewayName)

			addRow := func() {
				if rows[key] == nil {
					rows[key] = &pb.GatewaysTable_Row{}
					rows[key].ClusterName = string(clusterName)
					rows[key].Name = string(gatewayName)
					rows[key].Namespace = string(gatewayNamespace)

				}
			}

			value := extractSampleValue(sample)

			switch result.prom {
			case promGatewayAlive:
				addRow()
				if value == 0 {
					rows[key].Alive = false
				} else {
					rows[key].Alive = true
				}

			case promNumMirroredServices:
				addRow()
				rows[key].PairedServices = value
			case promLatencyP50:
				addRow()
				rows[key].LatencyMsP50 = value
			case promLatencyP95:
				addRow()
				rows[key].LatencyMsP95 = value
			case promLatencyP99:
				addRow()
				rows[key].LatencyMsP99 = value
			}
		}
	}

	return rows
}

func (s *grpcServer) getGatewaysMetrics(ctx context.Context, req *pb.GatewaysRequest, timeWindow string) (map[string]*pb.GatewaysTable_Row, error) {
	labels, groupBy := buildGatewaysRequestLabels(req)

	reqLabels := generateLabelStringWithExclusion(labels, string(gatewayNameLabel))

	promQueries := map[promType]string{
		promGatewayAlive:        gatewayAliveQuery,
		promNumMirroredServices: numMirroredServicesQuery,
	}

	metricsResp, err := s.getPrometheusMetrics(ctx, promQueries, gatewayLatencyQuantileQuery, reqLabels, timeWindow, groupBy.String())

	if err != nil {
		return nil, err
	}

	rowsMap := processPrometheusResult(metricsResp)

	return rowsMap, nil
}
