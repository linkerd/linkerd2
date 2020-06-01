package public

import (
	"context"
	"fmt"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/k8s"
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

// this function returns a map of gateways to the number of services using them
func (s *grpcServer) getNumServicesMap() (map[string]uint64, error) {

	results := make(map[string]uint64)
	selector := fmt.Sprintf("%s,!%s", k8s.MirroredResourceLabel, k8s.MirroredGatewayLabel)
	services, err := s.k8sAPI.Client.CoreV1().Services(corev1.NamespaceAll).List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}

	for _, svc := range services.Items {
		clusterName := svc.Labels[k8s.RemoteClusterNameLabel]
		gatewayName := svc.Labels[k8s.RemoteGatewayNameLabel]
		gatewayNs := svc.Labels[k8s.RemoteGatewayNsLabel]
		key := fmt.Sprintf("%s-%s-%s", clusterName, gatewayName, gatewayNs)

		results[key]++
	}

	return results, nil
}

func processPrometheusResult(results []promResult, numSvcMap map[string]uint64) map[string]*pb.GatewaysTable_Row {

	rows := make(map[string]*pb.GatewaysTable_Row)

	for _, result := range results {
		for _, sample := range result.vec {

			clusterName := sample.Metric[remoteClusterNameLabel]
			gatewayName := sample.Metric[gatewayNameLabel]
			gatewayNamespace := sample.Metric[gatewayNamespaceLabel]
			numPairedSvc := numSvcMap[fmt.Sprintf("%s-%s-%s", clusterName, gatewayName, gatewayNamespace)]

			key := fmt.Sprintf("%s-%s-%s", clusterName, gatewayNamespace, gatewayName)

			addRow := func() {
				if rows[key] == nil {
					rows[key] = &pb.GatewaysTable_Row{}
					rows[key].ClusterName = string(clusterName)
					rows[key].Name = string(gatewayName)
					rows[key].Namespace = string(gatewayNamespace)
					rows[key].PairedServices = numPairedSvc
				}
			}

			value := extractSampleValue(sample)

			switch result.prom {
			case promGatewayAlive:
				addRow()
				rows[key].Alive = value > 0
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
		promGatewayAlive: gatewayAliveQuery,
	}

	metricsResp, err := s.getPrometheusMetrics(ctx, promQueries, gatewayLatencyQuantileQuery, reqLabels, timeWindow, groupBy.String())

	if err != nil {
		return nil, err
	}
	numSvcMap, err := s.getNumServicesMap()

	if err != nil {
		return nil, err
	}

	rowsMap := processPrometheusResult(metricsResp, numSvcMap)

	return rowsMap, nil
}
