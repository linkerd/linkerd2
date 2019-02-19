package destination

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	discoveryPb "github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	"github.com/linkerd/linkerd2/controller/k8s"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

type mockDestinationServer struct {
	errorToReturn   error
	contextToReturn context.Context
}

type mockDestinationGetServer struct {
	mockDestinationServer
	updatesReceived []*pb.Update
}

type mockDestinationGetProfileServer struct {
	mockDestinationServer
	profilesReceived []*pb.DestinationProfile
}

func (m *mockDestinationGetServer) Send(update *pb.Update) error {
	m.updatesReceived = append(m.updatesReceived, update)
	return m.errorToReturn
}

func (m *mockDestinationGetProfileServer) Send(profile *pb.DestinationProfile) error {
	m.profilesReceived = append(m.profilesReceived, profile)
	return m.errorToReturn
}

func (m *mockDestinationServer) SetHeader(metadata.MD) error  { return m.errorToReturn }
func (m *mockDestinationServer) SendHeader(metadata.MD) error { return m.errorToReturn }
func (m *mockDestinationServer) SetTrailer(metadata.MD)       {}
func (m *mockDestinationServer) Context() context.Context     { return m.contextToReturn }
func (m *mockDestinationServer) SendMsg(x interface{}) error  { return m.errorToReturn }
func (m *mockDestinationServer) RecvMsg(x interface{}) error  { return m.errorToReturn }

func TestBuildResolver(t *testing.T) {
	k8sAPI, err := k8s.NewFakeAPI("")
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}

	t.Run("Doesn't build a resolver if Kubernetes DNS zone isnt valid", func(t *testing.T) {
		invalidK8sDNSZones := []string{"1", "-a", "a-", "-"}
		for _, dsnZone := range invalidK8sDNSZones {
			resolver, err := buildResolver(dsnZone, "linkerd", k8sAPI, false)
			if err == nil {
				t.Fatalf("Expecting error when k8s zone is [%s], got nothing. Resolver: %v", dsnZone, resolver)
			}
		}
	})
}

// implements the streamingDestinationResolver interface
type mockStreamingDestinationResolver struct {
	hostReceived             string
	portReceived             int
	listenerReceived         endpointUpdateListener
	canResolveToReturn       bool
	errToReturnForCanResolve error
	errToReturnForResolution error
}

func (m *mockStreamingDestinationResolver) canResolve(host string, port int) (bool, error) {
	return m.canResolveToReturn, m.errToReturnForCanResolve
}

func (m *mockStreamingDestinationResolver) streamResolution(host string, port int, listener endpointUpdateListener) error {
	m.hostReceived = host
	m.portReceived = port
	m.listenerReceived = listener
	return m.errToReturnForResolution
}

func (m *mockStreamingDestinationResolver) streamProfiles(host string, clientNs string, listener profileUpdateListener) error {
	return nil
}

func (m *mockStreamingDestinationResolver) getState() servicePorts {
	return servicePorts{}
}

func (m *mockStreamingDestinationResolver) stop() {}

func TestStreamResolutionUsingCorrectResolverFor(t *testing.T) {
	stream := &mockDestinationGetServer{}
	host := "something"
	port := 666
	k8sAPI, err := k8s.NewFakeAPI("")
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}

	t.Run("Returns error if no resolver can resolve", func(t *testing.T) {
		no := &mockStreamingDestinationResolver{canResolveToReturn: false}

		server := server{
			k8sAPI:   k8sAPI,
			resolver: no,
		}

		err := server.streamResolution(host, port, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})

	t.Run("Returns error if the resolver returned an error on canResolve", func(t *testing.T) {
		resolver := &mockStreamingDestinationResolver{canResolveToReturn: true, errToReturnForCanResolve: errors.New("expected for can resolve")}

		server := server{
			k8sAPI:   k8sAPI,
			resolver: resolver,
		}

		err := server.streamResolution(host, port, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})

	t.Run("Returns error if the resolver returned an error on streamResolution", func(t *testing.T) {
		resolver := &mockStreamingDestinationResolver{canResolveToReturn: true, errToReturnForResolution: errors.New("expected for resolving")}

		server := server{
			k8sAPI:   k8sAPI,
			resolver: resolver,
		}

		err := server.streamResolution(host, port, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})
}

func TestEndpoints(t *testing.T) {
	k8sAPI, err := k8s.NewFakeAPI("")
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}
	k8sAPI.Sync()

	lis := bufconn.Listen(1024 * 1024)
	gRPCServer, err := NewServer(
		"fake-addr", "", "controller-ns",
		false, false, false, k8sAPI, nil,
	)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	defer gRPCServer.GracefulStop()
	go func() { gRPCServer.Serve(lis) }()

	destinationAPIConn, err := grpc.Dial(
		"fake-buf-addr",
		grpc.WithDialer(
			func(string, time.Duration) (net.Conn, error) {
				return lis.Dial()
			},
		),
		grpc.WithInsecure(),
	)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	defer destinationAPIConn.Close()

	discoveryClient := discoveryPb.NewDiscoveryClient(destinationAPIConn)

	t.Run("Implements the Discovery interface", func(t *testing.T) {
		resp, err := discoveryClient.Endpoints(context.Background(), &discoveryPb.EndpointsParams{})
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		expectedResp := &discoveryPb.EndpointsResponse{
			ServicePorts: make(map[string]*discoveryPb.ServicePort),
		}

		if !proto.Equal(resp, expectedResp) {
			t.Fatalf("Unexpected response: %+v", resp)
		}
	})
}
