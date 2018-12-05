package public

import (
	"context"
	"testing"

	"github.com/golang/protobuf/proto"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/common/model"
)

type topRoutesExpected struct {
	expectedStatRpc
	req              pb.TopRoutesRequest  // the request we would like to test
	expectedResponse pb.TopRoutesResponse // the routes response we expect
}

func genEmptyTopRoutesResponse() pb.TopRoutesResponse {
	return pb.TopRoutesResponse{
		Response: &pb.TopRoutesResponse_Routes{
			Routes: &pb.RouteTable{
				Rows: nil,
			},
		},
	}
}

func routesMetric(routes []string) model.Vector {
	samples := make(model.Vector, 0)
	for _, route := range routes {
		samples = append(samples, genRouteSample(route))
	}
	return samples
}

func genRouteSample(route string) *model.Sample {
	return &model.Sample{
		Metric: model.Metric{
			"rt_route":       model.LabelValue(route),
			"dst":            "foo.default.svc.cluster.local",
			"classification": "success",
			"tls":            "true",
		},
		Value:     123,
		Timestamp: 456,
	}
}

func testTopRoutes(t *testing.T, expectations []topRoutesExpected) {
	for _, exp := range expectations {

		mockProm, fakeGrpcServer, err := newMockGrpcServer(exp.expectedStatRpc)
		if err != nil {
			t.Fatalf("Error creating mock grpc server: %s", err)
		}

		rsp, err := fakeGrpcServer.TopRoutes(context.TODO(), &exp.req)
		if err != exp.err {
			t.Fatalf("Expected error: %s, Got: %s", exp.err, err)
		}

		err = exp.verifyPromQueries(mockProm)
		if err != nil {
			t.Fatal(err)
		}

		rows := rsp.GetRoutes().Rows

		if len(rows) != len(exp.expectedResponse.GetRoutes().Rows) {
			t.Fatalf(
				"Expected [%d] rows, got [%d].\nExpected:\n%s\nGot:\n%s",
				len(exp.expectedResponse.GetRoutes().Rows),
				len(rows),
				exp.expectedResponse.GetRoutes().Rows,
				rows,
			)
		}

		for i, row := range rows {
			expected := exp.expectedResponse.GetRoutes().Rows[i]
			if !proto.Equal(row, expected) {
				t.Fatalf("Expected: %+v\n Got: %+v", expected, row)
			}
		}
	}
}

func TestTopRoutes(t *testing.T) {
	t.Run("Successfully performs a routes query", func(t *testing.T) {
		routes := []string{"/a"}
		counts := []uint64{123}
		expectations := []topRoutesExpected{
			topRoutesExpected{
				expectedStatRpc: expectedStatRpc{
					err:              nil,
					mockPromResponse: routesMetric([]string{"/a"}),
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(route_response_latency_ms_bucket{deployment="webapp", direction="inbound", namespace="books"}[1m])) by (le, dst, rt_route))`,
						`histogram_quantile(0.95, sum(irate(route_response_latency_ms_bucket{deployment="webapp", direction="inbound", namespace="books"}[1m])) by (le, dst, rt_route))`,
						`histogram_quantile(0.99, sum(irate(route_response_latency_ms_bucket{deployment="webapp", direction="inbound", namespace="books"}[1m])) by (le, dst, rt_route))`,
						`sum(increase(route_response_total{deployment="webapp", direction="inbound", namespace="books"}[1m])) by (rt_route, dst, classification, tls)`,
					},
				},
				req: pb.TopRoutesRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "books",
							Type:      pkgK8s.Deployment,
							Name:      "webapp",
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenTopRoutesResponse(routes, counts),
			},
		}

		testTopRoutes(t, expectations)
	})

	t.Run("Successfully performs a routes query for a service", func(t *testing.T) {
		routes := []string{"/a"}
		counts := []uint64{123}
		expectations := []topRoutesExpected{
			topRoutesExpected{
				expectedStatRpc: expectedStatRpc{
					err:              nil,
					mockPromResponse: routesMetric([]string{"/a"}),
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(route_response_latency_ms_bucket{direction="inbound", dst=~"webapp.books.svc.cluster.local(:\\d+)?"}[1m])) by (le, dst, rt_route))`,
						`histogram_quantile(0.95, sum(irate(route_response_latency_ms_bucket{direction="inbound", dst=~"webapp.books.svc.cluster.local(:\\d+)?"}[1m])) by (le, dst, rt_route))`,
						`histogram_quantile(0.99, sum(irate(route_response_latency_ms_bucket{direction="inbound", dst=~"webapp.books.svc.cluster.local(:\\d+)?"}[1m])) by (le, dst, rt_route))`,
						`sum(increase(route_response_total{direction="inbound", dst=~"webapp.books.svc.cluster.local(:\\d+)?"}[1m])) by (rt_route, dst, classification, tls)`,
					},
				},
				req: pb.TopRoutesRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "books",
							Type:      pkgK8s.Service,
							Name:      "webapp",
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenTopRoutesResponse(routes, counts),
			},
		}

		testTopRoutes(t, expectations)
	})

	t.Run("Successfully performs an outbound routes query", func(t *testing.T) {
		routes := []string{"/a"}
		counts := []uint64{123}
		expectations := []topRoutesExpected{
			topRoutesExpected{
				expectedStatRpc: expectedStatRpc{
					err:              nil,
					mockPromResponse: routesMetric([]string{"/a"}),
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(route_response_latency_ms_bucket{deployment="traffic", direction="outbound", namespace="books"}[1m])) by (le, dst, rt_route))`,
						`histogram_quantile(0.95, sum(irate(route_response_latency_ms_bucket{deployment="traffic", direction="outbound", namespace="books"}[1m])) by (le, dst, rt_route))`,
						`histogram_quantile(0.99, sum(irate(route_response_latency_ms_bucket{deployment="traffic", direction="outbound", namespace="books"}[1m])) by (le, dst, rt_route))`,
						`sum(increase(route_response_total{deployment="traffic", direction="outbound", namespace="books"}[1m])) by (rt_route, dst, classification, tls)`,
					},
				},
				req: pb.TopRoutesRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "books",
							Type:      pkgK8s.Deployment,
							Name:      "traffic",
						},
					},
					Outbound: &pb.TopRoutesRequest_ToAll{
						ToAll: &pb.Empty{},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenTopRoutesResponse(routes, counts),
			},
		}

		testTopRoutes(t, expectations)
	})

	t.Run("Successfully performs an outbound service query", func(t *testing.T) {
		routes := []string{"/a"}
		counts := []uint64{123}
		expectations := []topRoutesExpected{
			topRoutesExpected{
				expectedStatRpc: expectedStatRpc{
					err:              nil,
					mockPromResponse: routesMetric([]string{"/a"}),
					expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(route_response_latency_ms_bucket{deployment="traffic", direction="outbound", dst=~"books.default.svc.cluster.local(:\\d+)?", namespace="books"}[1m])) by (le, dst, rt_route))`,
						`histogram_quantile(0.95, sum(irate(route_response_latency_ms_bucket{deployment="traffic", direction="outbound", dst=~"books.default.svc.cluster.local(:\\d+)?", namespace="books"}[1m])) by (le, dst, rt_route))`,
						`histogram_quantile(0.99, sum(irate(route_response_latency_ms_bucket{deployment="traffic", direction="outbound", dst=~"books.default.svc.cluster.local(:\\d+)?", namespace="books"}[1m])) by (le, dst, rt_route))`,
						`sum(increase(route_response_total{deployment="traffic", direction="outbound", dst=~"books.default.svc.cluster.local(:\\d+)?", namespace="books"}[1m])) by (rt_route, dst, classification, tls)`,
					},
				},
				req: pb.TopRoutesRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "books",
							Type:      pkgK8s.Deployment,
							Name:      "traffic",
						},
					},
					Outbound: &pb.TopRoutesRequest_ToAuthority{
						ToAuthority: "books.default.svc.cluster.local",
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenTopRoutesResponse(routes, counts),
			},
		}

		testTopRoutes(t, expectations)
	})
}
