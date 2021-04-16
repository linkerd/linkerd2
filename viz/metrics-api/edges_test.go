package api

import (
	"context"
	"testing"

	"github.com/golang/protobuf/proto"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/prometheus/common/model"
)

const (
	clientIDLabel = model.LabelName("client_id")
	serverIDLabel = model.LabelName("server_id")
	resourceLabel = model.LabelName("deployment")
)

type edgesExpected struct {
	expectedStatRPC
	req              *pb.EdgesRequest  // the request we would like to test
	expectedResponse *pb.EdgesResponse // the edges response we expect
}

func genInboundPromSample(resourceNamespace, resourceName, clientID string) *model.Sample {
	return &model.Sample{
		Metric: model.Metric{
			resourceLabel:  model.LabelValue(resourceName),
			namespaceLabel: model.LabelValue(resourceNamespace),
			clientIDLabel:  model.LabelValue(clientID),
		},
		Value:     123,
		Timestamp: 456,
	}
}

func genOutboundPromSample(resourceNamespace, resourceName, resourceNameDst, resourceNamespaceDst, serverID string) *model.Sample {
	dstResourceLabel := "dst_" + resourceLabel

	return &model.Sample{
		Metric: model.Metric{
			resourceLabel:     model.LabelValue(resourceName),
			namespaceLabel:    model.LabelValue(resourceNamespace),
			dstNamespaceLabel: model.LabelValue(resourceNamespaceDst),
			dstResourceLabel:  model.LabelValue(resourceNameDst),
			serverIDLabel:     model.LabelValue(serverID),
		},
		Value:     123,
		Timestamp: 456,
	}
}

func testEdges(t *testing.T, expectations []edgesExpected) {
	for _, exp := range expectations {
		mockProm, fakeGrpcServer, err := newMockGrpcServer(exp.expectedStatRPC)
		if err != nil {
			t.Fatalf("Error creating mock grpc server: %s", err)
		}

		rsp, err := fakeGrpcServer.Edges(context.TODO(), exp.req)
		if err != exp.err {
			t.Fatalf("Expected error: %s, Got: %s", exp.err, err)
		}

		err = exp.verifyPromQueries(mockProm)
		if err != nil {
			t.Fatal(err)
		}

		rspEdgeRows := rsp.GetOk().Edges

		if len(rspEdgeRows) != len(exp.expectedResponse.GetOk().Edges) {
			t.Fatalf(
				"Expected [%d] edge rows, got [%d].\nExpected:\n%s\nGot:\n%s",
				len(exp.expectedResponse.GetOk().Edges),
				len(rspEdgeRows),
				exp.expectedResponse.GetOk().Edges,
				rspEdgeRows,
			)
		}

		for i, st := range rspEdgeRows {
			expected := exp.expectedResponse.GetOk().Edges[i]
			if !proto.Equal(st, expected) {
				t.Fatalf("Expected: %+v\n Got: %+v\n", expected, st)
			}
		}

		if !proto.Equal(exp.expectedResponse.GetOk(), rsp.GetOk()) {
			t.Fatalf("Expected edgesOkResp: %+v\n Got: %+v", &exp.expectedResponse, rsp)
		}
	}
}

func TestEdges(t *testing.T) {
	mockPromResponse := model.Vector{
		genInboundPromSample("emojivoto", "emoji", "web.emojivoto.serviceaccount.identity.linkerd.cluster.local"),
		genInboundPromSample("emojivoto", "voting", "web.emojivoto.serviceaccount.identity.linkerd.cluster.local"),
		genInboundPromSample("emojivoto", "web", "default.emojivoto.serviceaccount.identity.linkerd.cluster.local"),
		genInboundPromSample("linkerd", "linkerd-prometheus", "linkerd-identity.linkerd.identity.linkerd.cluster.local"),

		genOutboundPromSample("emojivoto", "web", "emoji", "emojivoto", "emoji.emojivoto.serviceaccount.identity.linkerd.cluster.local"),
		genOutboundPromSample("emojivoto", "web", "voting", "emojivoto", "voting.emojivoto.serviceaccount.identity.linkerd.cluster.local"),
		genOutboundPromSample("emojivoto", "vote-bot", "web", "emojivoto", "web.emojivoto.serviceaccount.identity.linkerd.cluster.local"),
		genOutboundPromSample("linkerd", "linkerd-identity", "linkerd-prometheus", "linkerd", "linkerd-prometheus.linkerd.identity.linkerd.cluster.local"),
	}

	t.Run("Successfully returns edges for resource type Deployment and namespace emojivoto", func(t *testing.T) {
		expectations := []edgesExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err:              nil,
					mockPromResponse: mockPromResponse,
				},
				req: &pb.EdgesRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "emojivoto",
							Type:      pkgK8s.Deployment,
						},
					},
				},
				expectedResponse: GenEdgesResponse("deployment", "emojivoto"),
			}}

		testEdges(t, expectations)
	})

	t.Run("Successfully returns edges for resource type Deployment and namespace linkerd", func(t *testing.T) {
		expectations := []edgesExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err:              nil,
					mockPromResponse: mockPromResponse,
				},
				req: &pb.EdgesRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Namespace: "linkerd",
							Type:      pkgK8s.Deployment,
						},
					},
				},
				expectedResponse: GenEdgesResponse("deployment", "linkerd"),
			}}

		testEdges(t, expectations)
	})

	t.Run("Successfully returns edges for resource type Deployment and all namespaces", func(t *testing.T) {
		expectations := []edgesExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err:              nil,
					mockPromResponse: mockPromResponse,
				},
				req: &pb.EdgesRequest{
					Selector: &pb.ResourceSelection{
						Resource: &pb.Resource{
							Type: pkgK8s.Deployment,
						},
					},
				},
				expectedResponse: GenEdgesResponse("deployment", "all"),
			}}

		testEdges(t, expectations)
	})
}
