package api

import (
	"context"
	"errors"
	"fmt"
	"testing"

	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"github.com/linkerd/linkerd2/viz/pkg/prometheus"
	"github.com/prometheus/common/model"
	"google.golang.org/protobuf/proto"
)

const (
	serverIDLabel = model.LabelName("server_id")
	resourceLabel = model.LabelName("deployment")
	podLabel      = model.LabelName("pod")
)

type edgesExpected struct {
	expectedStatRPC
	req              *pb.EdgesRequest  // the request we would like to test
	expectedResponse *pb.EdgesResponse // the edges response we expect
}

func genOutboundPromSample(resourceNamespace, resourceName, resourceNameDst, resourceNamespaceDst, serverID string) *model.Sample {
	dstResourceLabel := "dst_" + resourceLabel

	return &model.Sample{
		Metric: model.Metric{
			resourceLabel:                model.LabelValue(resourceName),
			prometheus.NamespaceLabel:    model.LabelValue(resourceNamespace),
			prometheus.DstNamespaceLabel: model.LabelValue(resourceNamespaceDst),
			dstResourceLabel:             model.LabelValue(resourceNameDst),
			serverIDLabel:                model.LabelValue(serverID),
			podLabel:                     model.LabelValue(resourceName + "-0"),
		},
		Value:     123,
		Timestamp: 456,
	}
}

func genPod(name, namespace, sa string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  containers:
  - name: linkerd-proxy
    env:
    - name: LINKERD2_PROXY_IDENTITY_LOCAL_NAME
      value: $(_pod_sa).$(_pod_ns).serviceaccount.identity.linkerd.cluster.local
  serviceAccountName: %s
status:
  phase: Running
`, name, namespace, sa)
}

func testEdges(t *testing.T, expectations []edgesExpected) {
	for _, exp := range expectations {
		mockProm, fakeGrpcServer, err := newMockGrpcServer(exp.expectedStatRPC)
		if err != nil {
			t.Fatalf("Error creating mock grpc server: %s", err)
		}

		rsp, err := fakeGrpcServer.Edges(context.TODO(), exp.req)
		if !errors.Is(err, exp.err) {
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
		genOutboundPromSample("emojivoto", "web", "emoji", "emojivoto", "emoji.emojivoto.serviceaccount.identity.linkerd.cluster.local"),
		genOutboundPromSample("emojivoto", "web", "voting", "emojivoto", "voting.emojivoto.serviceaccount.identity.linkerd.cluster.local"),
		genOutboundPromSample("emojivoto", "vote-bot", "web", "emojivoto", "web.emojivoto.serviceaccount.identity.linkerd.cluster.local"),
		genOutboundPromSample("linkerd", "linkerd-identity", "linkerd-prometheus", "linkerd", "linkerd-prometheus.linkerd.serviceaccount.identity.linkerd.cluster.local"),
	}
	pods := []string{
		genPod("web-0", "emojivoto", "web"),
		genPod("vote-bot-0", "emojivoto", "default"),
		genPod("linkerd-identity-0", "linkerd", "linkerd-identity"),
	}

	t.Run("Successfully returns edges for resource type Deployment and namespace emojivoto", func(t *testing.T) {
		expectations := []edgesExpected{
			{
				expectedStatRPC: expectedStatRPC{
					err:              nil,
					mockPromResponse: mockPromResponse,
					k8sConfigs:       pods,
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
					k8sConfigs:       pods,
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
					k8sConfigs:       pods,
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
