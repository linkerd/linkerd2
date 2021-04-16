package api

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/prometheus/common/model"
	"google.golang.org/grpc"
)

// MockAPIClient satisfies the metrics-api gRPC interfaces
type MockAPIClient struct {
	ErrorToReturn                error
	ListPodsResponseToReturn     *pb.ListPodsResponse
	ListServicesResponseToReturn *pb.ListServicesResponse
	StatSummaryResponseToReturn  *pb.StatSummaryResponse
	GatewaysResponseToReturn     *pb.GatewaysResponse
	TopRoutesResponseToReturn    *pb.TopRoutesResponse
	EdgesResponseToReturn        *pb.EdgesResponse
	SelfCheckResponseToReturn    *pb.SelfCheckResponse
}

// StatSummary provides a mock of a metrics-api method.
func (c *MockAPIClient) StatSummary(ctx context.Context, in *pb.StatSummaryRequest, opts ...grpc.CallOption) (*pb.StatSummaryResponse, error) {
	return c.StatSummaryResponseToReturn, c.ErrorToReturn
}

// Gateways provides a mock of a metrics-api method.
func (c *MockAPIClient) Gateways(ctx context.Context, in *pb.GatewaysRequest, opts ...grpc.CallOption) (*pb.GatewaysResponse, error) {
	return c.GatewaysResponseToReturn, c.ErrorToReturn
}

// TopRoutes provides a mock of a metrics-api method.
func (c *MockAPIClient) TopRoutes(ctx context.Context, in *pb.TopRoutesRequest, opts ...grpc.CallOption) (*pb.TopRoutesResponse, error) {
	return c.TopRoutesResponseToReturn, c.ErrorToReturn
}

// Edges provides a mock of a metrics-api method.
func (c *MockAPIClient) Edges(ctx context.Context, in *pb.EdgesRequest, opts ...grpc.CallOption) (*pb.EdgesResponse, error) {
	return c.EdgesResponseToReturn, c.ErrorToReturn
}

// ListPods provides a mock of a metrics-api method.
func (c *MockAPIClient) ListPods(ctx context.Context, in *pb.ListPodsRequest, opts ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	return c.ListPodsResponseToReturn, c.ErrorToReturn
}

// ListServices provides a mock of a metrics-api method.
func (c *MockAPIClient) ListServices(ctx context.Context, in *pb.ListServicesRequest, opts ...grpc.CallOption) (*pb.ListServicesResponse, error) {
	return c.ListServicesResponseToReturn, c.ErrorToReturn
}

// SelfCheck provides a mock of a metrics-api method.
func (c *MockAPIClient) SelfCheck(ctx context.Context, in *pb.SelfCheckRequest, _ ...grpc.CallOption) (*pb.SelfCheckResponse, error) {
	return c.SelfCheckResponseToReturn, c.ErrorToReturn
}

// PodCounts is a test helper struct that is used for representing data in a
// StatTable.PodGroup.Row.
type PodCounts struct {
	Status      string
	MeshedPods  uint64
	RunningPods uint64
	FailedPods  uint64
	Errors      map[string]*pb.PodErrors
}

// GenStatSummaryResponse generates a mock metrics-api StatSummaryResponse
// object.
func GenStatSummaryResponse(resName, resType string, resNs []string, counts *PodCounts, basicStats bool, tcpStats bool) *pb.StatSummaryResponse {
	rows := []*pb.StatTable_PodGroup_Row{}
	for _, ns := range resNs {
		statTableRow := &pb.StatTable_PodGroup_Row{
			Resource: &pb.Resource{
				Namespace: ns,
				Type:      resType,
				Name:      resName,
			},
			TimeWindow: "1m",
		}

		if basicStats {
			statTableRow.Stats = &pb.BasicStats{
				SuccessCount: 123,
				FailureCount: 0,
				LatencyMsP50: 123,
				LatencyMsP95: 123,
				LatencyMsP99: 123,
			}
		}

		if tcpStats {
			statTableRow.TcpStats = &pb.TcpStats{
				OpenConnections: 123,
				ReadBytesTotal:  123,
				WriteBytesTotal: 123,
			}
		}

		if counts != nil {
			statTableRow.MeshedPodCount = counts.MeshedPods
			statTableRow.RunningPodCount = counts.RunningPods
			statTableRow.FailedPodCount = counts.FailedPods
			statTableRow.Status = counts.Status
			statTableRow.ErrorsByPod = counts.Errors
		}

		rows = append(rows, statTableRow)
	}

	resp := &pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: []*pb.StatTable{
					{
						Table: &pb.StatTable_PodGroup_{
							PodGroup: &pb.StatTable_PodGroup{
								Rows: rows,
							},
						},
					},
				},
			},
		},
	}

	return resp
}

// GenStatTsResponse generates a mock metrics-api StatSummaryResponse
// object in response to a request for trafficsplit stats.
func GenStatTsResponse(resName, resType string, resNs []string, basicStats bool, tsStats bool) *pb.StatSummaryResponse {
	leaves := map[string]string{
		"service-1": "900m",
		"service-2": "100m",
	}
	apex := "apex_name"

	rows := []*pb.StatTable_PodGroup_Row{}
	for _, ns := range resNs {
		for name, weight := range leaves {
			statTableRow := &pb.StatTable_PodGroup_Row{
				Resource: &pb.Resource{
					Namespace: ns,
					Type:      resType,
					Name:      resName,
				},
				TimeWindow: "1m",
			}

			if basicStats {
				statTableRow.Stats = &pb.BasicStats{
					SuccessCount: 123,
					FailureCount: 0,
					LatencyMsP50: 123,
					LatencyMsP95: 123,
					LatencyMsP99: 123,
				}
			}

			if tsStats {
				statTableRow.TsStats = &pb.TrafficSplitStats{
					Apex:   apex,
					Leaf:   name,
					Weight: weight,
				}
			}
			rows = append(rows, statTableRow)

		}
	}

	// sort rows before returning in order to have a consistent order for tests
	rows = sortTrafficSplitRows(rows)

	resp := &pb.StatSummaryResponse{
		Response: &pb.StatSummaryResponse_Ok_{ // https://github.com/golang/protobuf/issues/205
			Ok: &pb.StatSummaryResponse_Ok{
				StatTables: []*pb.StatTable{
					{
						Table: &pb.StatTable_PodGroup_{
							PodGroup: &pb.StatTable_PodGroup{
								Rows: rows,
							},
						},
					},
				},
			},
		},
	}
	return resp
}

type mockEdgeRow struct {
	resourceType string
	src          string
	dst          string
	srcNamespace string
	dstNamespace string
	clientID     string
	serverID     string
	msg          string
}

// a slice of edge rows to generate mock results
var emojivotoEdgeRows = []*mockEdgeRow{
	{
		resourceType: "deployment",
		src:          "web",
		dst:          "voting",
		srcNamespace: "emojivoto",
		dstNamespace: "emojivoto",
		clientID:     "web.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		serverID:     "voting.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		msg:          "",
	},
	{
		resourceType: "deployment",
		src:          "vote-bot",
		dst:          "web",
		srcNamespace: "emojivoto",
		dstNamespace: "emojivoto",
		clientID:     "default.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		serverID:     "web.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		msg:          "",
	},
	{
		resourceType: "deployment",
		src:          "web",
		dst:          "emoji",
		srcNamespace: "emojivoto",
		dstNamespace: "emojivoto",
		clientID:     "web.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		serverID:     "emoji.emojivoto.serviceaccount.identity.linkerd.cluster.local",
		msg:          "",
	},
}

// a slice of edge rows to generate mock results
var linkerdEdgeRows = []*mockEdgeRow{
	{
		resourceType: "deployment",
		src:          "linkerd-identity",
		dst:          "linkerd-prometheus",
		srcNamespace: "linkerd",
		dstNamespace: "linkerd",
		clientID:     "linkerd-identity.linkerd.identity.linkerd.cluster.local",
		serverID:     "linkerd-prometheus.linkerd.identity.linkerd.cluster.local",
		msg:          "",
	},
}

// GenEdgesResponse generates a mock metrics-api EdgesResponse
// object.
func GenEdgesResponse(resourceType string, edgeRowNamespace string) *pb.EdgesResponse {
	edgeRows := emojivotoEdgeRows

	if edgeRowNamespace == "linkerd" {
		edgeRows = linkerdEdgeRows
	} else if edgeRowNamespace == "all" {
		// combine emojivotoEdgeRows and linkerdEdgeRows
		edgeRows = append(edgeRows, linkerdEdgeRows...)
	}

	edges := []*pb.Edge{}
	for _, row := range edgeRows {
		edge := &pb.Edge{
			Src: &pb.Resource{
				Name:      row.src,
				Namespace: row.srcNamespace,
				Type:      row.resourceType,
			},
			Dst: &pb.Resource{
				Name:      row.dst,
				Namespace: row.dstNamespace,
				Type:      row.resourceType,
			},
			ClientId:      row.clientID,
			ServerId:      row.serverID,
			NoIdentityMsg: row.msg,
		}
		edges = append(edges, edge)
	}

	// sorting to retain consistent order for tests
	edges = sortEdgeRows(edges)

	resp := &pb.EdgesResponse{
		Response: &pb.EdgesResponse_Ok_{
			Ok: &pb.EdgesResponse_Ok{
				Edges: edges,
			},
		},
	}
	return resp
}

// GenTopRoutesResponse generates a mock metrics-api TopRoutesResponse object.
func GenTopRoutesResponse(routes []string, counts []uint64, outbound bool, authority string) *pb.TopRoutesResponse {
	rows := []*pb.RouteTable_Row{}
	for i, route := range routes {
		row := &pb.RouteTable_Row{
			Route:     route,
			Authority: authority,
			Stats: &pb.BasicStats{
				SuccessCount: counts[i],
				FailureCount: 0,
				LatencyMsP50: 123,
				LatencyMsP95: 123,
				LatencyMsP99: 123,
			},
			TimeWindow: "1m",
		}
		if outbound {
			row.Stats.ActualSuccessCount = counts[i]
		}
		rows = append(rows, row)
	}
	defaultRow := &pb.RouteTable_Row{
		Route:     "[DEFAULT]",
		Authority: authority,
		Stats: &pb.BasicStats{
			SuccessCount: counts[len(counts)-1],
			FailureCount: 0,
			LatencyMsP50: 123,
			LatencyMsP95: 123,
			LatencyMsP99: 123,
		},
		TimeWindow: "1m",
	}
	if outbound {
		defaultRow.Stats.ActualSuccessCount = counts[len(counts)-1]
	}
	rows = append(rows, defaultRow)

	resp := &pb.TopRoutesResponse{
		Response: &pb.TopRoutesResponse_Ok_{
			Ok: &pb.TopRoutesResponse_Ok{
				Routes: []*pb.RouteTable{
					{
						Rows:     rows,
						Resource: "deploy/foobar",
					},
				},
			},
		},
	}

	return resp
}

type expectedStatRPC struct {
	err                       error
	k8sConfigs                []string    // k8s objects to seed the API
	mockPromResponse          model.Value // mock out a prometheus query response
	expectedPrometheusQueries []string    // queries we expect metrics-api to issue to prometheus
}

func newMockGrpcServer(exp expectedStatRPC) (*prometheus.MockProm, *grpcServer, error) {
	k8sAPI, err := k8s.NewFakeAPI(exp.k8sConfigs...)
	if err != nil {
		return nil, nil, err
	}

	mockProm := &prometheus.MockProm{Res: exp.mockPromResponse}
	fakeGrpcServer := newGrpcServer(
		mockProm,
		k8sAPI,
		"linkerd",
		"cluster.local",
		[]string{},
	)

	k8sAPI.Sync(nil)

	return mockProm, fakeGrpcServer, nil
}

func (exp expectedStatRPC) verifyPromQueries(mockProm *prometheus.MockProm) error {
	// if exp.expectedPrometheusQueries is an empty slice we still want to check no queries were executed.
	if exp.expectedPrometheusQueries != nil {
		sort.Strings(exp.expectedPrometheusQueries)
		sort.Strings(mockProm.QueriesExecuted)

		// because reflect.DeepEqual([]string{}, nil) is false
		if len(exp.expectedPrometheusQueries) == 0 && len(mockProm.QueriesExecuted) == 0 {
			return nil
		}

		if !reflect.DeepEqual(exp.expectedPrometheusQueries, mockProm.QueriesExecuted) {
			return fmt.Errorf("Prometheus queries incorrect. \nExpected:\n%+v \nGot:\n%+v",
				exp.expectedPrometheusQueries, mockProm.QueriesExecuted)
		}
	}
	return nil
}
