package api

import (
	"context"
	"fmt"

	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/pkg/prometheus"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
)

func (s *grpcServer) Authz(ctx context.Context, req *pb.AuthzRequest) (*pb.AuthzResponse, error) {

	// check for well-formed request
	if req.GetResource() == nil {
		return &pb.AuthzResponse{
			Response: &pb.AuthzResponse_Error{
				Error: &pb.ResourceError{
					Error: "AuthzRequest missing resource",
				},
			},
		}, nil
	}

	labels := prometheus.QueryLabels(req.GetResource())
	reqLabels := labels.Merge(model.LabelSet{
		"direction": model.LabelValue("inbound"),
	})

	groupBy := model.LabelNames{
		prometheus.RouteKindLabel, prometheus.RouteNameLabel, prometheus.AuthorizationKindLabel, prometheus.AuthorizationNameLabel, prometheus.ServerKindLabel, prometheus.ServerNameLabel,
	}
	promQueries := make(map[promType]string)
	promQueries[promRequests] = fmt.Sprintf(reqQuery, reqLabels, req.TimeWindow, groupBy.String())
	// Use `labels` as direction isn't present with authorization metrics
	promQueries[promAllowedRequests] = fmt.Sprintf(httpAuthzAllowQuery, labels, req.TimeWindow, groupBy.String())
	promQueries[promDeniedRequests] = fmt.Sprintf(httpAuthzDenyQuery, labels, req.TimeWindow, groupBy.String())
	quantileQueries := generateQuantileQueries(latencyQuantileQuery, reqLabels.String(), req.TimeWindow, groupBy.String())
	results, err := s.getPrometheusMetrics(ctx, promQueries, quantileQueries)
	if err != nil {
		return &pb.AuthzResponse{
			Response: &pb.AuthzResponse_Error{
				Error: &pb.ResourceError{
					Error: err.Error(),
				},
			},
		}, nil
	}

	type rowKey struct {
		routeName  string
		routeKind  string
		serverName string
		serverKind string
		authzName  string
		authzKind  string
	}
	rows := map[rowKey]*pb.StatTable_PodGroup_Row{}

	for _, result := range results {
		for _, sample := range result.vec {
			key := rowKey{
				routeName:  string(sample.Metric[prometheus.RouteNameLabel]),
				routeKind:  string(sample.Metric[prometheus.RouteKindLabel]),
				serverName: string(sample.Metric[prometheus.ServerNameLabel]),
				serverKind: string(sample.Metric[prometheus.ServerKindLabel]),
				authzName:  string(sample.Metric[prometheus.AuthorizationNameLabel]),
				authzKind:  string(sample.Metric[prometheus.AuthorizationKindLabel]),
			}
			// Get the row if it exists or initialize an empty one
			row := rows[key]
			if row == nil {
				row = &pb.StatTable_PodGroup_Row{
					Resource: req.Resource,
					Stats:    &pb.BasicStats{},
					SrvStats: &pb.ServerStats{
						Srv: &pb.Resource{
							Namespace: string(sample.Metric[prometheus.NamespaceLabel]),
							Type:      key.serverKind,
							Name:      key.serverName,
						},
						Route: &pb.Resource{
							Namespace: string(sample.Metric[prometheus.NamespaceLabel]),
							Type:      key.routeKind,
							Name:      key.routeName,
						},
						Authz: &pb.Resource{
							Namespace: string(sample.Metric[prometheus.NamespaceLabel]),
							Type:      key.authzKind,
							Name:      key.authzName,
						},
					},
				}
				rows[key] = row
			}

			value := extractSampleValue(sample)
			switch result.prom {
			case promRequests:
				switch string(sample.Metric[model.LabelName("classification")]) {
				case success:
					row.Stats.SuccessCount += value
				case failure:
					row.Stats.FailureCount += value
				}
			case promLatencyP50:
				row.Stats.LatencyMsP50 = value
			case promLatencyP95:
				row.Stats.LatencyMsP95 = value
			case promLatencyP99:
				row.Stats.LatencyMsP99 = value
			case promAllowedRequests:
				row.SrvStats.AllowedCount = value
			case promDeniedRequests:
				row.SrvStats.DeniedCount = value
			}
		}
	}

	table := []*pb.StatTable_PodGroup_Row{}
	for _, row := range rows {
		table = append(table, row)
	}

	rsp := pb.AuthzResponse{
		Response: &pb.AuthzResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.AuthzResponse_Ok{
				StatTable: &pb.StatTable{
					Table: &pb.StatTable_PodGroup_{
						PodGroup: &pb.StatTable_PodGroup{
							Rows: table,
						},
					},
				},
			},
		},
	}

	log.Debugf("Sent response as %+v\n", table)
	return &rsp, nil
}
