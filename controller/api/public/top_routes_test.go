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
					// Comparing Prometheus queries is flakey because label order is
					// non-deterministic.
					/*expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(route_response_latency_ms_bucket{namespace="books", service="webapp", direction="inbound", dst=~"webapp(:\\d+)?"}[1m])) by (le, rt_route))`,
						`histogram_quantile(0.95, sum(irate(route_response_latency_ms_bucket{namespace="books", service="webapp", direction="inbound", dst=~"webapp(:\\d+)?"}[1m])) by (le, rt_route))`,
						`histogram_quantile(0.99, sum(irate(route_response_latency_ms_bucket{namespace="books", service="webapp", direction="inbound", dst=~"webapp(:\\d+)?"}[1m])) by (le, rt_route))`,
						`sum(increase(route_response_total{namespace="books", service="webapp", direction="inbound", dst=~"webapp(:\\d+)?"}[1m])) by (rt_route, classification, tls)`,
					},*/
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
					// Comparing Prometheus queries is flakey because label order is
					// non-deterministic.
					/*expectedPrometheusQueries: []string{
						`histogram_quantile(0.5, sum(irate(route_response_latency_ms_bucket{direction="outbound", namespace="books", deployment="traffic", dst=~"webapp(:\\d+)?"}[1m])) by (le, rt_route))`,
						`histogram_quantile(0.95, sum(irate(route_response_latency_ms_bucket{direction="outbound", namespace="books", deployment="traffic", dst=~"webapp(:\\d+)?"}[1m])) by (le, rt_route))`,
						`histogram_quantile(0.99, sum(irate(route_response_latency_ms_bucket{direction="outbound", namespace="books", deployment="traffic", dst=~"webapp(:\\d+)?"}[1m])) by (le, rt_route))`,
						`sum(increase(route_response_total{direction="outbound", namespace="books", deployment="traffic", dst=~"webapp(:\\d+)?"}[1m])) by (rt_route, classification, tls)`,
					},*/
				},
				req: pb.TopRoutesRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "books",
							Type:      pkgK8s.Service,
							Name:      "webapp",
						},
					},
					Outbound: &pb.TopRoutesRequest_FromResource{
						FromResource: &pb.Resource{
							Namespace: "books",
							Type:      pkgK8s.Deployment,
							Name:      "traffic",
						},
					},
					TimeWindow: "1m",
				},
				expectedResponse: GenTopRoutesResponse(routes, counts),
			},
		}

		testTopRoutes(t, expectations)
	})
}
