package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/golang/protobuf/proto"
	vizClient "github.com/linkerd/linkerd2/viz/metrics-api/client"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
)

type mockServer struct {
	LastRequestReceived proto.Message
	ResponseToReturn    proto.Message
	ErrorToReturn       error
}

type mockGrpcServer struct {
	mockServer
}

func (m *mockGrpcServer) StatSummary(ctx context.Context, req *pb.StatSummaryRequest) (*pb.StatSummaryResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.StatSummaryResponse), m.ErrorToReturn
}

func (m *mockGrpcServer) Gateways(ctx context.Context, req *pb.GatewaysRequest) (*pb.GatewaysResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.GatewaysResponse), m.ErrorToReturn
}

func (m *mockGrpcServer) TopRoutes(ctx context.Context, req *pb.TopRoutesRequest) (*pb.TopRoutesResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.TopRoutesResponse), m.ErrorToReturn
}

func (m *mockGrpcServer) Edges(ctx context.Context, req *pb.EdgesRequest) (*pb.EdgesResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.EdgesResponse), m.ErrorToReturn
}

func (m *mockGrpcServer) ListPods(ctx context.Context, req *pb.ListPodsRequest) (*pb.ListPodsResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.ListPodsResponse), m.ErrorToReturn
}

func (m *mockGrpcServer) ListServices(ctx context.Context, req *pb.ListServicesRequest) (*pb.ListServicesResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.ListServicesResponse), m.ErrorToReturn
}

func (m *mockGrpcServer) SelfCheck(ctx context.Context, req *pb.SelfCheckRequest) (*pb.SelfCheckResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.SelfCheckResponse), m.ErrorToReturn
}

type grpcCallTestCase struct {
	expectedRequest  proto.Message
	expectedResponse proto.Message
	functionCall     func() (proto.Message, error)
}

func TestServer(t *testing.T) {
	t.Run("Delegates all non-streaming RPC messages to the underlying grpc server", func(t *testing.T) {
		mockGrpcServer, client := getServerVizClient(t)

		listPodsReq := &pb.ListPodsRequest{}
		testListPods := grpcCallTestCase{
			expectedRequest: listPodsReq,
			expectedResponse: &pb.ListPodsResponse{
				Pods: []*pb.Pod{
					{Status: "ok-ish"},
				},
			},
			functionCall: func() (proto.Message, error) { return client.ListPods(context.TODO(), listPodsReq) },
		}

		statSummaryReq := &pb.StatSummaryRequest{}
		testStatSummary := grpcCallTestCase{
			expectedRequest:  statSummaryReq,
			expectedResponse: &pb.StatSummaryResponse{},
			functionCall:     func() (proto.Message, error) { return client.StatSummary(context.TODO(), statSummaryReq) },
		}

		for _, testCase := range []grpcCallTestCase{testListPods, testStatSummary} {
			assertCallWasForwarded(t, &mockGrpcServer.mockServer, testCase.expectedRequest, testCase.expectedResponse, testCase.functionCall)
		}
	})
}

func getServerVizClient(t *testing.T) (*mockGrpcServer, pb.ApiClient) {
	mockGrpcServer := &mockGrpcServer{}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Could not start listener: %v", err)
	}

	go func() {
		handler := &handler{
			grpcServer: mockGrpcServer,
		}
		err := http.Serve(listener, handler)
		if err != nil {
			t.Fatalf("Could not start server: %v", err)
		}
	}()

	client, err := vizClient.NewInternalClient("linkerd", listener.Addr().String())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	return mockGrpcServer, client
}

func assertCallWasForwarded(t *testing.T, mockServer *mockServer, expectedRequest proto.Message, expectedResponse proto.Message, functionCall func() (proto.Message, error)) {
	mockServer.ErrorToReturn = nil
	mockServer.ResponseToReturn = expectedResponse
	actualResponse, err := functionCall()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	actualRequest := mockServer.LastRequestReceived
	if !proto.Equal(actualRequest, expectedRequest) {
		t.Fatalf("Expecting server call to return [%v], but got [%v]", expectedRequest, actualRequest)
	}
	if !proto.Equal(actualResponse, expectedResponse) {
		t.Fatalf("Expecting server call to return [%v], but got [%v]", expectedResponse, actualResponse)
	}

	mockServer.ErrorToReturn = errors.New("expected")
	_, err = functionCall()
	if err == nil {
		t.Fatalf("Expecting error, got nothing")
	}
}
