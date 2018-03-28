package destination

import (
	"context"
	"reflect"
	"testing"

	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/proxy/destination"
	"github.com/runconduit/conduit/controller/k8s"
	"google.golang.org/grpc/metadata"
)

func TestBuildResolversList(t *testing.T) {
	endpointsWatcher := &k8s.EndpointsWatcher{}
	dnsWatcher := &DnsWatcher{}

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

		if _, ok := resolvers[0].(*echoIpResolver); !ok {
			t.Fatalf("Expecting first resolver to be echo IP, got [%+v]. List: %v", resolvers[0], resolvers)
		}

		if _, ok := resolvers[1].(*k8sResolver); !ok {
			t.Fatalf("Expecting second resolver to be k8s, got [%+v]. List: %v", resolvers[0], resolvers)
		}
	})
}

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

func TestEndpointListener(t *testing.T) {

	t.Run("Sends one update for add and another for remove", func(t *testing.T) {
		mockGetServer := &mockDestination_GetServer{updatesReceived: []*pb.Update{}}

		listener := &endpointListener{stream: mockGetServer}

		addedAddress1 := common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 1}}, Port: 1}
		addedAddress2 := common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 2}}, Port: 2}
		removedAddress1 := common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 100}}, Port: 100}

		listener.Update([]common.TcpAddress{addedAddress1, addedAddress2}, []common.TcpAddress{removedAddress1})

		expectedNumUpdates := 2
		actualNumUpdates := len(mockGetServer.updatesReceived)
		if actualNumUpdates != expectedNumUpdates {
			t.Fatalf("Expecting [%d] updates, got [%d]. Updates: %v", expectedNumUpdates, actualNumUpdates, mockGetServer.updatesReceived)
		}
	})

	t.Run("Sends addresses as removed or added", func(t *testing.T) {
		mockGetServer := &mockDestination_GetServer{updatesReceived: []*pb.Update{}}

		listener := &endpointListener{stream: mockGetServer}

		addedAddress1 := common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 1}}, Port: 1}
		addedAddress2 := common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 2}}, Port: 2}
		removedAddress1 := common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 100}}, Port: 100}

		listener.Update([]common.TcpAddress{addedAddress1, addedAddress2}, []common.TcpAddress{removedAddress1})

		addressesAdded := mockGetServer.updatesReceived[0].GetAdd().Addrs
		actualNumberOfAdded := len(addressesAdded)
		expectedNumberOfAdded := 2
		if actualNumberOfAdded != expectedNumberOfAdded {
			t.Fatalf("Expecting [%d] addresses to be added, got [%d]: %v", expectedNumberOfAdded, actualNumberOfAdded, addressesAdded)
		}

		addressesRemoved := mockGetServer.updatesReceived[1].GetRemove().Addrs
		actualNumberOfRemoved := len(addressesRemoved)
		expectedNumberOfRemoved := 1
		if actualNumberOfRemoved != expectedNumberOfRemoved {
			t.Fatalf("Expecting [%d] addresses to be removed, got [%d]: %v", expectedNumberOfRemoved, actualNumberOfRemoved, addressesRemoved)
		}

		checkAddress(t, addressesAdded[0], &addedAddress1)
		checkAddress(t, addressesAdded[1], &addedAddress2)

		actualAddressRemoved := addressesRemoved[0]
		expectedAddressRemoved := &removedAddress1
		if !reflect.DeepEqual(actualAddressRemoved, expectedAddressRemoved) {
			t.Fatalf("Expected remove address to be [%s], but it was [%s]", expectedAddressRemoved, actualAddressRemoved)
		}
	})

	t.Run("It'' done when the underlying context is done", func(t *testing.T) {
		context, cancelFn := context.WithCancel(context.Background())
		mockGetServer := &mockDestination_GetServer{updatesReceived: []*pb.Update{}, contextToReturn: context}
		listener := &endpointListener{stream: mockGetServer}

		completed := make(chan bool)
		go func() {
			<-listener.Done()
			completed <- true
		}()

		cancelFn()

		c := <-completed

		if !c {
			t.Fatalf("Expected function to be completed after the cancel()")
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
