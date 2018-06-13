package destination

import (
	"context"
	"reflect"
	"testing"

	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/proxy/destination"
	"github.com/runconduit/conduit/controller/util"
	pkgK8s "github.com/runconduit/conduit/pkg/k8s"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type podExpected struct {
	pod                   string
	namespace             string
	replicationController string
	phase                 v1.PodPhase
}

type listenerExpected struct {
	pods           []podExpected
	address        common.TcpAddress
	listenerLabels map[string]string
	addressLabels  map[string]string
}

func noPodsByIp(ip string) ([]*v1.Pod, error) {
	return make([]*v1.Pod, 0), nil
}

func TestEndpointListener(t *testing.T) {
	t.Run("Sends one update for add and another for remove", func(t *testing.T) {
		mockGetServer := &mockDestination_GetServer{updatesReceived: []*pb.Update{}}

		listener := &endpointListener{stream: mockGetServer, podsByIp: noPodsByIp}

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

		listener := &endpointListener{stream: mockGetServer, podsByIp: noPodsByIp}

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
		listener := &endpointListener{stream: mockGetServer, podsByIp: noPodsByIp}

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
		expectedReplicationControllerName := "rc-name"

		addedAddress1 := common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 666}}, Port: 1}
		ipForAddr1 := util.IPToString(addedAddress1.Ip)
		podForAddedAddress1 := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedPodName,
				Namespace: expectedNamespace,
				Labels: map[string]string{
					pkgK8s.ProxyReplicationControllerLabel: expectedReplicationControllerName,
				},
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
			},
		}
		addedAddress2 := common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 222}}, Port: 22}
		podIndex := func(ip string) ([]*v1.Pod, error) {
			return map[string][]*v1.Pod{ipForAddr1: []*v1.Pod{podForAddedAddress1}}[ip], nil
		}

		mockGetServer := &mockDestination_GetServer{updatesReceived: []*pb.Update{}}
		listener := &endpointListener{
			podsByIp: podIndex,
			labels: map[string]string{
				"service":   expectedServiceName,
				"namespace": expectedNamespace,
			},
			stream: mockGetServer,
		}

		listener.Update([]common.TcpAddress{addedAddress1, addedAddress2}, nil)

		actualGlobalMetricLabels := mockGetServer.updatesReceived[0].GetAdd().MetricLabels
		expectedGlobalMetricLabels := map[string]string{"namespace": expectedNamespace, "service": expectedServiceName}
		if !reflect.DeepEqual(actualGlobalMetricLabels, expectedGlobalMetricLabels) {
			t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", expectedGlobalMetricLabels, actualGlobalMetricLabels)
		}

		actualAddedAddress1MetricLabels := mockGetServer.updatesReceived[0].GetAdd().Addrs[0].MetricLabels
		expectedAddedAddress1MetricLabels := map[string]string{
			"pod": expectedPodName,
			"replication_controller": expectedReplicationControllerName,
		}
		if !reflect.DeepEqual(actualAddedAddress1MetricLabels, expectedAddedAddress1MetricLabels) {
			t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", expectedAddedAddress1MetricLabels, actualAddedAddress1MetricLabels)
		}
	})

	t.Run("Sends TlsIdentity when enabled", func(t *testing.T) {
		expectedPodName := "pod1"
		expectedPodNamespace := "this-namespace"
		expectedConduitNamespace := "conduit-namespace"
		expectedTlsIdentity := &pb.TlsIdentity_K8SPodNamespace{
			PodName:      expectedPodName,
			PodNs:        expectedPodNamespace,
			ControllerNs: expectedConduitNamespace,
		}

		addedAddress := common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 666}}, Port: 1}
		ipForAddr := util.IPToString(addedAddress.Ip)
		podForAddedAddress := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedPodName,
				Namespace: expectedPodNamespace,
				Labels: map[string]string{
					pkgK8s.ControllerNSLabel: expectedConduitNamespace,
				},
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
			},
		}

		podIndex := func(ip string) ([]*v1.Pod, error) {
			return map[string][]*v1.Pod{ipForAddr: []*v1.Pod{podForAddedAddress}}[ip], nil
		}

		mockGetServer := &mockDestination_GetServer{updatesReceived: []*pb.Update{}}
		listener := &endpointListener{
			podsByIp:  podIndex,
			stream:    mockGetServer,
			enableTLS: true,
		}

		listener.Update([]common.TcpAddress{addedAddress}, nil)

		addrs := mockGetServer.updatesReceived[0].GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		actualTlsIdentity := addrs[0].GetTlsIdentity().GetK8SPodNamespace()
		if !reflect.DeepEqual(actualTlsIdentity, expectedTlsIdentity) {
			t.Fatalf("Expected TlsIdentity to be [%v] but was [%v]", expectedTlsIdentity, actualTlsIdentity)
		}
	})

	t.Run("Does not sent TlsIdentity when not enabled", func(t *testing.T) {
		expectedPodName := "pod1"
		expectedPodNamespace := "this-namespace"
		expectedConduitNamespace := "conduit-namespace"

		addedAddress := common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 666}}, Port: 1}
		ipForAddr := util.IPToString(addedAddress.Ip)
		podForAddedAddress := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedPodName,
				Namespace: expectedPodNamespace,
				Labels: map[string]string{
					pkgK8s.ControllerNSLabel: expectedConduitNamespace,
				},
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
			},
		}

		podIndex := func(ip string) ([]*v1.Pod, error) {
			return map[string][]*v1.Pod{ipForAddr: []*v1.Pod{podForAddedAddress}}[ip], nil
		}

		mockGetServer := &mockDestination_GetServer{updatesReceived: []*pb.Update{}}
		listener := &endpointListener{
			podsByIp: podIndex,
			stream:   mockGetServer,
		}

		listener.Update([]common.TcpAddress{addedAddress}, nil)

		addrs := mockGetServer.updatesReceived[0].GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		if addrs[0].TlsIdentity != nil {
			t.Fatalf("Expected no TlsIdentity to be sent, but got [%v]", addrs[0].TlsIdentity)
		}
	})

	t.Run("It only returns pods in a running state", func(t *testing.T) {
		expectations := []listenerExpected{
			listenerExpected{
				pods: []podExpected{
					podExpected{
						pod:                   "pod1",
						namespace:             "this-namespace",
						replicationController: "rc-name",
						phase: v1.PodPending,
					},
				},
				address: common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 666}}, Port: 1},
				listenerLabels: map[string]string{
					"service":   "service-name",
					"namespace": "this-namespace",
				},
				addressLabels: map[string]string{},
			},
			listenerExpected{
				pods: []podExpected{
					podExpected{
						pod:                   "pod1",
						namespace:             "this-namespace",
						replicationController: "rc-name",
						phase: v1.PodPending,
					},
					podExpected{
						pod:                   "pod2",
						namespace:             "this-namespace",
						replicationController: "rc-name",
						phase: v1.PodRunning,
					},
					podExpected{
						pod:                   "pod3",
						namespace:             "this-namespace",
						replicationController: "rc-name",
						phase: v1.PodSucceeded,
					},
				},
				address: common.TcpAddress{Ip: &common.IPAddress{Ip: &common.IPAddress_Ipv4{Ipv4: 666}}, Port: 1},
				listenerLabels: map[string]string{
					"service":   "service-name",
					"namespace": "this-namespace",
				},
				addressLabels: map[string]string{
					"pod": "pod2",
					"replication_controller": "rc-name",
				},
			},
		}

		for _, exp := range expectations {
			backingMap := map[string][]*v1.Pod{}

			for _, pod := range exp.pods {
				ipForAddr := util.IPToString(exp.address.Ip)
				podForAddedAddress := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      pod.pod,
						Namespace: pod.namespace,
						Labels: map[string]string{
							pkgK8s.ProxyReplicationControllerLabel: pod.replicationController,
						},
					},
					Status: v1.PodStatus{
						Phase: pod.phase,
					},
				}

				backingMap[ipForAddr] = append(backingMap[ipForAddr], podForAddedAddress)
			}
			podIndex := func(ip string) ([]*v1.Pod, error) {
				return backingMap[ip], nil
			}

			mockGetServer := &mockDestination_GetServer{updatesReceived: []*pb.Update{}}
			listener := &endpointListener{
				podsByIp: podIndex,
				labels:   exp.listenerLabels,
				stream:   mockGetServer,
			}

			listener.Update([]common.TcpAddress{exp.address}, nil)

			actualGlobalMetricLabels := mockGetServer.updatesReceived[0].GetAdd().MetricLabels
			if !reflect.DeepEqual(actualGlobalMetricLabels, exp.listenerLabels) {
				t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", exp.listenerLabels, actualGlobalMetricLabels)
			}

			actualAddedAddressMetricLabels := mockGetServer.updatesReceived[0].GetAdd().Addrs[0].MetricLabels
			if !reflect.DeepEqual(actualAddedAddressMetricLabels, exp.addressLabels) {
				t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", exp.addressLabels, actualAddedAddressMetricLabels)
			}
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
