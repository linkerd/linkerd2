package public

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/golang/protobuf/proto"
	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	healcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	configPb "github.com/linkerd/linkerd2/controller/gen/config"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
)

type mockServer struct {
	LastRequestReceived        proto.Message
	ResponseToReturn           proto.Message
	DestinationStreamsToReturn []*destinationPb.Update
	ErrorToReturn              error
}

type mockGrpcServer struct {
	mockServer
	DestinationStreamsToReturn []*destinationPb.Update
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

func (m *mockGrpcServer) Version(ctx context.Context, req *pb.Empty) (*pb.VersionInfo, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.VersionInfo), m.ErrorToReturn
}

func (m *mockGrpcServer) ListPods(ctx context.Context, req *pb.ListPodsRequest) (*pb.ListPodsResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.ListPodsResponse), m.ErrorToReturn
}

func (m *mockGrpcServer) ListServices(ctx context.Context, req *pb.ListServicesRequest) (*pb.ListServicesResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.ListServicesResponse), m.ErrorToReturn
}

func (m *mockGrpcServer) SelfCheck(ctx context.Context, req *healcheckPb.SelfCheckRequest) (*healcheckPb.SelfCheckResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*healcheckPb.SelfCheckResponse), m.ErrorToReturn
}

func (m *mockGrpcServer) Config(ctx context.Context, req *pb.Empty) (*configPb.All, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*configPb.All), m.ErrorToReturn
}

func (m *mockGrpcServer) Tap(req *pb.TapRequest, tapServer pb.Api_TapServer) error {
	m.LastRequestReceived = req
	if m.ErrorToReturn != nil {
		// Not implemented in public API. Instead, use tap APIServer.
		return errors.New("Not implemented")
	}

	return m.ErrorToReturn
}

func (m *mockGrpcServer) TapByResource(req *pb.TapByResourceRequest, tapServer pb.Api_TapByResourceServer) error {
	m.LastRequestReceived = req
	if m.ErrorToReturn != nil {
		// Not implemented in public API. Instead, use tap APIServer.
		return errors.New("Not implemented")
	}

	return m.ErrorToReturn
}

func (m *mockGrpcServer) Get(req *destinationPb.GetDestination, destinationServer destinationPb.Destination_GetServer) error {
	m.LastRequestReceived = req
	if m.ErrorToReturn == nil {
		for _, msg := range m.DestinationStreamsToReturn {
			destinationServer.Send(msg)
		}
	}

	return m.ErrorToReturn
}

func (m *mockGrpcServer) GetProfile(_ *destinationPb.GetDestination, _ destinationPb.Destination_GetProfileServer) error {
	// Not implemented in the Public API. Instead, the proxies should reach the Destination gRPC server directly.
	return errors.New("Not implemented")
}

type grpcCallTestCase struct {
	expectedRequest  proto.Message
	expectedResponse proto.Message
	functionCall     func() (proto.Message, error)
}

func TestServer(t *testing.T) {
	t.Run("Delegates all non-streaming RPC messages to the underlying grpc server", func(t *testing.T) {
		mockGrpcServer, client := getServerClient(t)

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

		versionReq := &pb.Empty{}
		testVersion := grpcCallTestCase{
			expectedRequest: versionReq,
			expectedResponse: &pb.VersionInfo{
				BuildDate: "02/21/1983",
			},
			functionCall: func() (proto.Message, error) { return client.Version(context.TODO(), versionReq) },
		}

		for _, testCase := range []grpcCallTestCase{testListPods, testStatSummary, testVersion} {
			assertCallWasForwarded(t, &mockGrpcServer.mockServer, testCase.expectedRequest, testCase.expectedResponse, testCase.functionCall)
		}
	})

	t.Run("Delegates all streaming Destination RPC messages to the underlying grpc server", func(t *testing.T) {
		mockGrpcServer, client := getServerClient(t)

		expectedDestinationGetResponses := []*destinationPb.Update{
			{
				Update: &destinationPb.Update_Add{
					Add: BuildAddrSet(
						AuthorityEndpoints{
							Namespace: "emojivoto",
							ServiceID: "emoji-svc",
							Pods: []PodDetails{
								{
									Name: "emoji-6bf9f47bd5-jjcrl",
									IP:   16909060,
									Port: 8080,
								},
							},
						},
					),
				},
			},
			{
				Update: &destinationPb.Update_Add{
					Add: BuildAddrSet(
						AuthorityEndpoints{
							Namespace: "emojivoto",
							ServiceID: "voting-svc",
							Pods: []PodDetails{
								{
									Name: "voting-7bf9f47bd5-jjdrl",
									IP:   84281096,
									Port: 8080,
								},
							},
						},
					),
				},
			},
		}
		mockGrpcServer.DestinationStreamsToReturn = expectedDestinationGetResponses
		mockGrpcServer.ErrorToReturn = nil

		destinationGetClient, err := client.Get(context.TODO(), &destinationPb.GetDestination{})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		for _, expectedDestinationGetEvent := range expectedDestinationGetResponses {
			actualDestinationGetEvent, err := destinationGetClient.Recv()
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !proto.Equal(actualDestinationGetEvent, expectedDestinationGetEvent) {
				t.Fatalf("Expecting destination.get event to be [%v], but was [%v]", expectedDestinationGetEvent, actualDestinationGetEvent)
			}
		}
	})

	t.Run("Handles Tap route errors before opening keep-alive response", func(t *testing.T) {
		mockGrpcServer, client := getServerClient(t)

		mockGrpcServer.ErrorToReturn = errors.New("expected error")

		_, err := client.Tap(context.TODO(), &pb.TapRequest{})
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})

	t.Run("Handles TapByResource route errors before opening keep-alive response", func(t *testing.T) {
		mockGrpcServer, client := getServerClient(t)

		mockGrpcServer.ErrorToReturn = errors.New("expected error")

		_, err := client.TapByResource(context.TODO(), &pb.TapByResourceRequest{})
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})

}

func getServerClient(t *testing.T) (*mockGrpcServer, APIClient) {
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

	client, err := NewInternalClient("linkerd", listener.Addr().String())
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
