package api

import (
	"context"
	"fmt"

	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	gatewayAliveQuery           = "sum(gateway_alive%s) by (%s)"
	gatewayLatencyQuantileQuery = "histogram_quantile(%s, sum(irate(gateway_probe_latency_ms_bucket%s[%s])) by (le, %s))"
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

// this function returns a map of target cluster to the number of services mirrored
// from it
func (s *grpcServer) getNumServicesMap(ctx context.Context) (map[string]uint64, error) {

	results := make(map[string]uint64)
	selector := fmt.Sprintf("%s,!%s", k8s.MirroredResourceLabel, k8s.MirroredGatewayLabel)
	services, err := s.k8sAPI.Client.CoreV1().Services(corev1.NamespaceAll).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}

	for _, svc := range services.Items {
		clusterName := svc.Labels[k8s.RemoteClusterNameLabel]
		results[clusterName]++
	}

	return results, nil
}

func processPrometheusResult(results []promResult, numSvcMap map[string]uint64) map[string]*pb.GatewaysTable_Row {

	rows := make(map[string]*pb.GatewaysTable_Row)

	for _, result := range results {
		for _, sample := range result.vec {

			clusterName := string(sample.Metric[remoteClusterNameLabel])
			numPairedSvc := numSvcMap[clusterName]

			addRow := func() {
				if rows[clusterName] == nil {
					rows[clusterName] = &pb.GatewaysTable_Row{}
					rows[clusterName].ClusterName = clusterName
					rows[clusterName].PairedServices = numPairedSvc
				}
			}

			value := extractSampleValue(sample)

			switch result.prom {
			case promGatewayAlive:
				addRow()
				rows[clusterName].Alive = value > 0
			case promLatencyP50:
				addRow()
				rows[clusterName].LatencyMsP50 = value
			case promLatencyP95:
				addRow()
				rows[clusterName].LatencyMsP95 = value
			case promLatencyP99:
				addRow()
				rows[clusterName].LatencyMsP99 = value
			}
		}
	}

	return rows
}

func (s *grpcServer) getGatewaysMetrics(ctx context.Context, req *pb.GatewaysRequest, timeWindow string) (map[string]*pb.GatewaysTable_Row, error) {
	labels, groupBy := buildGatewaysRequestLabels(req)

	promQueries := map[promType]string{
		promGatewayAlive: fmt.Sprintf(gatewayAliveQuery, labels.String(), groupBy.String()),
	}

	quantileQueries := generateQuantileQueries(gatewayLatencyQuantileQuery, labels.String(), timeWindow, groupBy.String())
	metricsResp, err := s.getPrometheusMetrics(ctx, promQueries, quantileQueries)

	if err != nil {
		return nil, err
	}
	numSvcMap, err := s.getNumServicesMap(ctx)

	if err != nil {
		return nil, err
	}

	rowsMap := processPrometheusResult(metricsResp, numSvcMap)

	return rowsMap, nil
}
