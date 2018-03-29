package destination

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/proxy/destination"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/controller/util"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEndpointListener(t *testing.T) {
	t.Run("Sends one update for add and another for remove", func(t *testing.T) {
		mockGetServer := &mockDestination_GetServer{updatesReceived: []*pb.Update{}}

		listener := &endpointListener{stream: mockGetServer, podsByIp: k8s.NewEmptyPodIndex()}

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

		listener := &endpointListener{stream: mockGetServer, podsByIp: k8s.NewEmptyPodIndex()}

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

	t.Run("It returns when the underlying context is done", func(t *testing.T) {
		context, cancelFn := context.WithCancel(context.Background())
		mockGetServer := &mockDestination_GetServer{updatesReceived: []*pb.Update{}, contextToReturn: context}
		listener := &endpointListener{stream: mockGetServer, podsByIp: k8s.NewEmptyPodIndex()}

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

	t.Run("Sends metric labels with added addresses", func(t *testing.T) {
		expectedServiceName := "service-name"
		expectedPodName := "pod1"
		expectedNamespace := "this-namespace"

		addedAddress1 := common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 666}}, Port: 1}
		ipForAddr1 := util.IPToString(addedAddress1.Ip)
		podForAddedAddress1 := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedPodName,
				Namespace: expectedNamespace,
			},
		}
		addedAddress2 := common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 222}}, Port: 22}
		fmt.Println(ipForAddr1)
		podIndex := &k8s.InMemoryPodIndex{BackingMap: map[string]*v1.Pod{ipForAddr1: podForAddedAddress1}}

		mockGetServer := &mockDestination_GetServer{updatesReceived: []*pb.Update{}}
		listener := &endpointListener{
			podsByIp:    podIndex,
			serviceName: expectedServiceName,
			stream:      mockGetServer,
		}

		listener.Update([]common.TcpAddress{addedAddress1, addedAddress2}, nil)

		actualGlobalMetricLabels := mockGetServer.updatesReceived[0].GetAdd().MetricLabels
		expectedGlobalMetricLabels := map[string]string{"k8s_namespace": expectedNamespace, "k8s_service": expectedServiceName}
		if !reflect.DeepEqual(actualGlobalMetricLabels, expectedGlobalMetricLabels) {
			t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", expectedGlobalMetricLabels, actualGlobalMetricLabels)
		}

		actualAddedAddress1MetricLabels := mockGetServer.updatesReceived[0].GetAdd().Addrs[0].MetricLabels
		expectedAddedAddress1MetricLabels := map[string]string{"k8s_pod": expectedPodName}
		if !reflect.DeepEqual(actualAddedAddress1MetricLabels, expectedAddedAddress1MetricLabels) {
			t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", expectedAddedAddress1MetricLabels, actualAddedAddress1MetricLabels)
		}
	})

	t.Run("It returns when the underlying context is done", func(t *testing.T) {
		context, cancelFn := context.WithCancel(context.Background())
		mockGetServer := &mockDestination_GetServer{updatesReceived: []*pb.Update{}, contextToReturn: context}
		listener := &endpointListener{stream: mockGetServer, podsByIp: k8s.NewEmptyPodIndex()}

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
