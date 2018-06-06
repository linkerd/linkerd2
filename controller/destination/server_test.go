package destination

import (
	"context"
	"errors"
	"testing"

	pb "github.com/runconduit/conduit/controller/gen/proxy/destination"
	"github.com/runconduit/conduit/controller/k8s"
	"google.golang.org/grpc/metadata"
)

type mockDestination_GetServer struct {
	errorToReturn   error
	contextToReturn context.Context
	updatesReceived []*pb.Update
}

func (m *mockDestination_GetServer) Send(update *pb.Update) error {
	m.updatesReceived = append(m.updatesReceived, update)
	return m.errorToReturn
}

func (m *mockDestination_GetServer) SetHeader(metadata.MD) error  { return m.errorToReturn }
func (m *mockDestination_GetServer) SendHeader(metadata.MD) error { return m.errorToReturn }
func (m *mockDestination_GetServer) SetTrailer(metadata.MD)       {}
func (m *mockDestination_GetServer) Context() context.Context     { return m.contextToReturn }
func (m *mockDestination_GetServer) SendMsg(x interface{}) error  { return m.errorToReturn }
func (m *mockDestination_GetServer) RecvMsg(x interface{}) error  { return m.errorToReturn }

func TestBuildResolversList(t *testing.T) {
	endpointsWatcher := &k8s.MockEndpointsWatcher{}

	t.Run("Doesn't build a list if Kubernetes DNS zone isnt valid", func(t *testing.T) {
		invalidK8sDNSZones := []string{"1", "-a", "a-", "-"}
		for _, dsnZone := range invalidK8sDNSZones {
			resolvers, err := buildResolversList(dsnZone, endpointsWatcher)
			if err == nil {
				t.Fatalf("Expecting error when k8s zone is [%s], got nothing. Resolvers: %v", dsnZone, resolvers)
			}
		}
	})

	t.Run("Builds list with echo IP first, then K8s resolver", func(t *testing.T) {
		resolvers, err := buildResolversList("some.zone", endpointsWatcher)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		actualNumResolvers := len(resolvers)
		expectedNumResolvers := 1
		if actualNumResolvers != expectedNumResolvers {
			t.Fatalf("Expecting [%d] resolvers, got [%d]: %v", expectedNumResolvers, actualNumResolvers, resolvers)
		}

		if _, ok := resolvers[0].(*k8sResolver); !ok {
			t.Fatalf("Expecting second resolver to be k8s, got [%+v]. List: %v", resolvers[0], resolvers)
		}
	})
}

// implements the streamingDestinationResolver interface
type mockStreamingDestinationResolver struct {
	hostReceived             string
	portReceived             int
	listenerReceived         updateListener
	canResolveToReturn       bool
	errToReturnForCanResolve error
	errToReturnForResolution error
}

func (m *mockStreamingDestinationResolver) canResolve(host string, port int) (bool, error) {
	return m.canResolveToReturn, m.errToReturnForCanResolve
}

func (m *mockStreamingDestinationResolver) streamResolution(host string, port int, listener updateListener) error {
	m.hostReceived = host
	m.portReceived = port
	m.listenerReceived = listener
	return m.errToReturnForResolution
}

func TestStreamResolutionUsingCorrectResolverFor(t *testing.T) {
	stream := &mockDestination_GetServer{}
	host := "something"
	port := 666
	k8sAPI, err := k8s.NewFakeAPI()
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}

	t.Run("Uses first resolver that is able to resolve the host and port", func(t *testing.T) {
		no := &mockStreamingDestinationResolver{canResolveToReturn: false}
		yes := &mockStreamingDestinationResolver{canResolveToReturn: true}
		otherYes := &mockStreamingDestinationResolver{canResolveToReturn: true}

		server := server{
			k8sAPI:    k8sAPI,
			resolvers: []streamingDestinationResolver{no, no, yes, no, no, otherYes},
		}

		err := server.streamResolutionUsingCorrectResolverFor(host, port, stream)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if no.listenerReceived != nil {
			t.Fatalf("Expected handler [%+v] to not be called, but it was", no)
		}

		if otherYes.listenerReceived != nil {
			t.Fatalf("Expected handler [%+v] to not be called, but it was", otherYes)
		}

		if yes.listenerReceived == nil || yes.portReceived != port || yes.hostReceived != host {
			t.Fatalf("Expected resolved [%+v] to have been called with stream [%v] host [%s] and port [%d]", yes, stream, host, port)
		}
	})

	t.Run("Returns error if no resolver can resolve", func(t *testing.T) {
		no := &mockStreamingDestinationResolver{canResolveToReturn: false}

		server := server{
			k8sAPI:    k8sAPI,
			resolvers: []streamingDestinationResolver{no, no, no, no},
		}

		err := server.streamResolutionUsingCorrectResolverFor(host, port, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})

	t.Run("Returns error if the resolver returned an error on canResolve", func(t *testing.T) {
		resolver := &mockStreamingDestinationResolver{canResolveToReturn: true, errToReturnForCanResolve: errors.New("expected for can resolve")}

		server := server{
			k8sAPI:    k8sAPI,
			resolvers: []streamingDestinationResolver{resolver},
		}

		err := server.streamResolutionUsingCorrectResolverFor(host, port, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})

	t.Run("Returns error if the resolver returned an error on streamResolution", func(t *testing.T) {
		resolver := &mockStreamingDestinationResolver{canResolveToReturn: true, errToReturnForResolution: errors.New("expected for resolving")}

		server := server{
			k8sAPI:    k8sAPI,
			resolvers: []streamingDestinationResolver{resolver},
		}

		err := server.streamResolutionUsingCorrectResolverFor(host, port, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})
}
