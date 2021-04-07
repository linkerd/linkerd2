package public

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/golang/protobuf/proto"
	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
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

func TestServer(t *testing.T) {
	t.Run("Delegates all streaming Destination RPC messages to the underlying grpc server", func(t *testing.T) {
		mockGrpcServer, client := getServerPublicClient(t)

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
}

func getServerPublicClient(t *testing.T) (*mockGrpcServer, Client) {
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
