package public

import (
	"context"
	"errors"
	"net"
	"net/http"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/proto"
	common "github.com/runconduit/conduit/controller/gen/common"
	healcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	pb "github.com/runconduit/conduit/controller/gen/public"
)

type mockGrpcServer struct {
	LastRequestReceived proto.Message
	ResponseToReturn    proto.Message
	TapStreamsToReturn  []*common.TapEvent
	ErrorToReturn       error
}

func (m *mockGrpcServer) StatSummary(ctx context.Context, req *pb.StatSummaryRequest) (*pb.StatSummaryResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.StatSummaryResponse), m.ErrorToReturn
}

func (m *mockGrpcServer) Version(ctx context.Context, req *pb.Empty) (*pb.VersionInfo, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.VersionInfo), m.ErrorToReturn
}

func (m *mockGrpcServer) ListPods(ctx context.Context, req *pb.Empty) (*pb.ListPodsResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*pb.ListPodsResponse), m.ErrorToReturn
}

func (m *mockGrpcServer) SelfCheck(ctx context.Context, req *healcheckPb.SelfCheckRequest) (*healcheckPb.SelfCheckResponse, error) {
	m.LastRequestReceived = req
	return m.ResponseToReturn.(*healcheckPb.SelfCheckResponse), m.ErrorToReturn
}

func (m *mockGrpcServer) Tap(req *pb.TapRequest, tapServer pb.Api_TapServer) error {
	m.LastRequestReceived = req
	if m.ErrorToReturn == nil {
		for _, msg := range m.TapStreamsToReturn {
			tapServer.Send(msg)
		}
	}

	return m.ErrorToReturn
}

func (m *mockGrpcServer) TapByResource(req *pb.TapByResourceRequest, tapServer pb.Api_TapByResourceServer) error {
	m.LastRequestReceived = req
	if m.ErrorToReturn == nil {
		for _, msg := range m.TapStreamsToReturn {
			tapServer.Send(msg)
		}
	}

	return m.ErrorToReturn
}

type grpcCallTestCase struct {
	expectedRequest  proto.Message
	expectedResponse proto.Message
	functionCall     func() (proto.Message, error)
}

func TestServer(t *testing.T) {
	t.Run("Delegates all non-streaming RPC messages to the underlying grpc server", func(t *testing.T) {
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

		client, err := NewInternalClient(listener.Addr().String())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		listPodsReq := &pb.Empty{}
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

		selfCheckReq := &healcheckPb.SelfCheckRequest{}
		testSelfCheck := grpcCallTestCase{
			expectedRequest: selfCheckReq,
			expectedResponse: &healcheckPb.SelfCheckResponse{
				Results: []*healcheckPb.CheckResult{
					{
						SubsystemName: "banana",
					},
				},
			},
			functionCall: func() (proto.Message, error) { return client.SelfCheck(context.TODO(), selfCheckReq) },
		}

		for _, testCase := range []grpcCallTestCase{testListPods, testStatSummary, testVersion, testSelfCheck} {
			assertCallWasForwarded(t, mockGrpcServer, testCase.expectedRequest, testCase.expectedResponse, testCase.functionCall)
		}
	})

	t.Run("Delegates all streaming tap RPC messages to the underlying grpc server", func(t *testing.T) {
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

		client, err := NewInternalClient(listener.Addr().String())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedTapResponses := []*common.TapEvent{
			{
				Destination: &common.TcpAddress{
					Port: 9999,
				},
				Source: &common.TcpAddress{
					Port: 6666,
				},
			}, {
				Destination: &common.TcpAddress{
					Port: 2102,
				},
				Source: &common.TcpAddress{
					Port: 1983,
				},
			},
		}
		mockGrpcServer.TapStreamsToReturn = expectedTapResponses
		mockGrpcServer.ErrorToReturn = nil

		tapClient, err := client.TapByResource(context.TODO(), &pb.TapByResourceRequest{})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		for _, expectedTapEvent := range expectedTapResponses {
			actualTapEvent, err := tapClient.Recv()
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !reflect.DeepEqual(actualTapEvent, expectedTapEvent) {
				t.Fatalf("Expecting tap event to be [%v], but was [%v]", expectedTapEvent, actualTapEvent)
			}
		}
	})

	t.Run("Handles errors before opening keep-alive response", func(t *testing.T) {
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

		client, err := NewInternalClient(listener.Addr().String())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		mockGrpcServer.ErrorToReturn = errors.New("expected error")

		_, err = client.Tap(context.TODO(), &pb.TapRequest{})
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})
}

func assertCallWasForwarded(t *testing.T, mockGrpcServer *mockGrpcServer, expectedRequest proto.Message, expectedResponse proto.Message, functionCall func() (proto.Message, error)) {
	mockGrpcServer.ErrorToReturn = nil
	mockGrpcServer.ResponseToReturn = expectedResponse
	actualResponse, err := functionCall()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	actualRequest := mockGrpcServer.LastRequestReceived
	if !reflect.DeepEqual(actualRequest, expectedRequest) {
		t.Fatalf("Expecting server call to return [%v], but got [%v]", expectedRequest, actualRequest)
	}
	if !reflect.DeepEqual(actualResponse, expectedResponse) {
		t.Fatalf("Expecting server call to return [%v], but got [%v]", expectedResponse, actualResponse)
	}

	mockGrpcServer.ErrorToReturn = errors.New("expected")
	_, err = functionCall()
	if err == nil {
		t.Fatalf("Expecting error, got nothing")
	}
}
