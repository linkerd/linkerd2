package destination

import (
	"context"
	"errors"
	"reflect"
	"testing"

	common "github.com/runconduit/conduit/controller/gen/common"
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
	dnsWatcher := &mockDnsWatcher{}

	t.Run("Doesn't build a list if Kubernetes DNS zone isnt valid", func(t *testing.T) {
		invalidK8sDNSZones := []string{"1", "-a", "a-", "-"}
		for _, dsnZone := range invalidK8sDNSZones {
			resolvers, err := buildResolversList(dsnZone, endpointsWatcher, dnsWatcher)
			if err == nil {
				t.Fatalf("Expecting error when k8s zone is [%s], got nothing. Resolvers: %v", dsnZone, resolvers)
			}
		}
	})

	t.Run("Builds list with echo IP first, then K8s resolver", func(t *testing.T) {
		resolvers, err := buildResolversList("some.zone", endpointsWatcher, dnsWatcher)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		actualNumResolvers := len(resolvers)
		expectedNumResolvers := 2
		if actualNumResolvers != expectedNumResolvers {
			t.Fatalf("Expecting [%d] resolvers, got [%d]: %v", expectedNumResolvers, actualNumResolvers, resolvers)
		}

		if _, ok := resolvers[0].(*echoIpV4Resolver); !ok {
			t.Fatalf("Expecting first resolver to be echo IP, got [%+v]. List: %v", resolvers[0], resolvers)
		}

		if _, ok := resolvers[1].(*k8sResolver); !ok {
			t.Fatalf("Expecting second resolver to be k8s, got [%+v]. List: %v", resolvers[0], resolvers)
		}
	})
}

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

	t.Run("Uses first resolver that is able to resolve the host and port", func(t *testing.T) {
		no := &mockStreamingDestinationResolver{canResolveToReturn: false}
		yes := &mockStreamingDestinationResolver{canResolveToReturn: true}
		otherYes := &mockStreamingDestinationResolver{canResolveToReturn: true}

		resolvers := []streamingDestinationResolver{no, no, yes, no, no, otherYes}

		err := streamResolutionUsingCorrectResolverFor(k8s.NewEmptyPodIndex(), resolvers, host, port, stream)
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

		resolvers := []streamingDestinationResolver{no, no, no, no}

		err := streamResolutionUsingCorrectResolverFor(k8s.NewEmptyPodIndex(), resolvers, host, port, stream)

		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})

	t.Run("Returns error if the resolver returned an error", func(t *testing.T) {
		errorOnCanResolve := &mockStreamingDestinationResolver{canResolveToReturn: true, errToReturnForCanResolve: errors.New("expected for can resolve")}
		errorOnResolving := &mockStreamingDestinationResolver{canResolveToReturn: true, errToReturnForResolution: errors.New("expected for resolving")}

		err := streamResolutionUsingCorrectResolverFor(k8s.NewEmptyPodIndex(), []streamingDestinationResolver{errorOnCanResolve}, host, port, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}

		err = streamResolutionUsingCorrectResolverFor(k8s.NewEmptyPodIndex(), []streamingDestinationResolver{errorOnResolving}, host, port, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})
}

func checkAddress(t *testing.T, addr *pb.WeightedAddr, expectedAddress *common.TcpAddress) {
	actualAddress := addr.Addr
	actualWeight := addr.Weight
	expectedWeight := uint32(1)

	if !reflect.DeepEqual(actualAddress, expectedAddress) || actualWeight != expectedWeight {
		t.Fatalf("Expected added address to be [%+v] and weight to be [%d], but it was [%+v] and [%d]", expectedAddress, expectedWeight, actualAddress, actualWeight)
	}
}
