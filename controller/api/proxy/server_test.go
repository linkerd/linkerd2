package proxy

import (
	"context"
	"errors"
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/k8s"
	"google.golang.org/grpc/metadata"
)

type mockDestination_Server struct {
	errorToReturn   error
	contextToReturn context.Context
}

type mockDestination_GetServer struct {
	mockDestination_Server
	updatesReceived []*pb.Update
}

type mockDestination_GetProfileServer struct {
	mockDestination_Server
	profilesReceived []*pb.DestinationProfile
}

func (m *mockDestination_GetServer) Send(update *pb.Update) error {
	m.updatesReceived = append(m.updatesReceived, update)
	return m.errorToReturn
}

func (m *mockDestination_GetProfileServer) Send(profile *pb.DestinationProfile) error {
	m.profilesReceived = append(m.profilesReceived, profile)
	return m.errorToReturn
}

func (m *mockDestination_Server) SetHeader(metadata.MD) error  { return m.errorToReturn }
func (m *mockDestination_Server) SendHeader(metadata.MD) error { return m.errorToReturn }
func (m *mockDestination_Server) SetTrailer(metadata.MD)       {}
func (m *mockDestination_Server) Context() context.Context     { return m.contextToReturn }
func (m *mockDestination_Server) SendMsg(x interface{}) error  { return m.errorToReturn }
func (m *mockDestination_Server) RecvMsg(x interface{}) error  { return m.errorToReturn }

func TestBuildResolver(t *testing.T) {
	k8sAPI, err := k8s.NewFakeAPI("")
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}

	t.Run("Doesn't build a resolver if Kubernetes DNS zone isnt valid", func(t *testing.T) {
		invalidK8sDNSZones := []string{"1", "-a", "a-", "-"}
		for _, dsnZone := range invalidK8sDNSZones {
			resolver, err := buildResolver(dsnZone, "linkerd", k8sAPI)
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

func (m *mockStreamingDestinationResolver) streamProfiles(host string, listener profileUpdateListener) error {
	return nil
}

func (m *mockStreamingDestinationResolver) stop() {}

func TestStreamResolutionUsingCorrectResolverFor(t *testing.T) {
	stream := &mockDestination_GetServer{}
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
