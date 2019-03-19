package destination

import (
	"context"
	"reflect"
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	pkgAddr "github.com/linkerd/linkerd2/pkg/addr"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	deploymentKind = "deployment"
	podDeployment  = "pod-deployment"
	thisNS         = "this-namespace"
)

var (
	addedAddress1 = &net.TcpAddress{
		Ip:   &net.IPAddress{Ip: &net.IPAddress_Ipv4{Ipv4: 1}},
		Port: 1,
	}

	addedAddress2 = &net.TcpAddress{
		Ip:   &net.IPAddress{Ip: &net.IPAddress_Ipv4{Ipv4: 2}},
		Port: 2,
	}

	removedAddress1 = &net.TcpAddress{
		Ip:   &net.IPAddress{Ip: &net.IPAddress_Ipv4{Ipv4: 100}},
		Port: 100,
	}

	pod1 = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "ns",
		},
	}

	pod2 = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "ns",
		},
	}

	add = []*updateAddress{
		{address: addedAddress1, pod: pod1},
		{address: addedAddress2, pod: pod2},
	}

	remove = []*updateAddress{
		{address: removedAddress1},
	}
)

func defaultOwnerKindAndName(pod *corev1.Pod) (string, string) {
	return "", ""
}

func TestEndpointListener(t *testing.T) {
	t.Run("Sends one update for add and another for remove", func(t *testing.T) {
		mockGetServer := &mockDestinationGetServer{updatesReceived: []*pb.Update{}}
		listener := newEndpointListener(
			mockGetServer,
			defaultOwnerKindAndName,
			false,
			"linkerd",
		)

		listener.Update(add, remove)

		expectedNumUpdates := 2
		actualNumUpdates := len(mockGetServer.updatesReceived)
		if actualNumUpdates != expectedNumUpdates {
			t.Fatalf("Expecting [%d] updates, got [%d]. Updates: %v", expectedNumUpdates, actualNumUpdates, mockGetServer.updatesReceived)
		}
	})

	t.Run("Sends addresses as removed or added", func(t *testing.T) {
		mockGetServer := &mockDestinationGetServer{updatesReceived: []*pb.Update{}}
		listener := newEndpointListener(
			mockGetServer,
			defaultOwnerKindAndName,
			false,
			"linkerd",
		)

		listener.Update(add, remove)

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

		checkAddress(t, addressesAdded[0], addedAddress1)
		checkAddress(t, addressesAdded[1], addedAddress2)

		actualAddressRemoved := addressesRemoved[0]
		expectedAddressRemoved := removedAddress1
		if !reflect.DeepEqual(actualAddressRemoved, expectedAddressRemoved) {
			t.Fatalf("Expected remove address to be [%s], but it was [%s]", expectedAddressRemoved, actualAddressRemoved)
		}
	})

	t.Run("It returns when the underlying context is done", func(t *testing.T) {
		context, cancelFn := context.WithCancel(context.Background())
		mockGetServer := &mockDestinationGetServer{
			updatesReceived: []*pb.Update{},
			mockDestinationServer: mockDestinationServer{
				contextToReturn: context,
			},
		}
		listener := newEndpointListener(
			mockGetServer,
			defaultOwnerKindAndName,
			false,
			"linkerd",
		)

		completed := make(chan bool)
		go func() {
			<-listener.ClientClose()
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
		expectedPodName := pod1.Name
		expectedNamespace := thisNS
		expectedReplicationControllerName := "rc-name"

		podForAddedAddress1 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedPodName,
				Namespace: expectedNamespace,
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		}

		ownerKindAndName := func(pod *corev1.Pod) (string, string) {
			return "replicationcontroller", expectedReplicationControllerName
		}

		mockGetServer := &mockDestinationGetServer{updatesReceived: []*pb.Update{}}
		listener := newEndpointListener(
			mockGetServer,
			ownerKindAndName,
			false,
			"linkerd",
		)
		listener.labels = map[string]string{
			"service":   expectedServiceName,
			"namespace": expectedNamespace,
		}

		add := []*updateAddress{
			{address: addedAddress1, pod: podForAddedAddress1},
		}
		listener.Update(add, nil)

		actualGlobalMetricLabels := mockGetServer.updatesReceived[0].GetAdd().MetricLabels
		expectedGlobalMetricLabels := map[string]string{"namespace": expectedNamespace, "service": expectedServiceName}
		if !reflect.DeepEqual(actualGlobalMetricLabels, expectedGlobalMetricLabels) {
			t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", expectedGlobalMetricLabels, actualGlobalMetricLabels)
		}

		actualAddedAddress1MetricLabels := mockGetServer.updatesReceived[0].GetAdd().Addrs[0].MetricLabels
		expectedAddedAddress1MetricLabels := map[string]string{
			"pod":                   expectedPodName,
			"replicationcontroller": expectedReplicationControllerName,
		}
		if !reflect.DeepEqual(actualAddedAddress1MetricLabels, expectedAddedAddress1MetricLabels) {
			t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", expectedAddedAddress1MetricLabels, actualAddedAddress1MetricLabels)
		}
	})

	t.Run("Sends TlsIdentity when enabled", func(t *testing.T) {
		t.Skip("TLS is currently disabled")
		expectedPodName := pod1.Name
		expectedPodNamespace := thisNS
		expectedControllerNamespace := "linkerd-namespace"
		expectedPodDeployment := podDeployment
		//expectedTLSIdentity := nil

		podForAddedAddress1 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedPodName,
				Namespace: expectedPodNamespace,
				Annotations: map[string]string{
					pkgK8s.IdentityModeAnnotation: "FIXME",
				},
				Labels: map[string]string{
					pkgK8s.ControllerNSLabel:    expectedControllerNamespace,
					pkgK8s.ProxyDeploymentLabel: expectedPodDeployment,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		}

		ownerKindAndName := func(pod *corev1.Pod) (string, string) {
			return deploymentKind, expectedPodDeployment
		}

		mockGetServer := &mockDestinationGetServer{updatesReceived: []*pb.Update{}}
		listener := newEndpointListener(
			mockGetServer,
			ownerKindAndName,
			false,
			expectedControllerNamespace,
		)

		add := []*updateAddress{
			{address: addedAddress1, pod: podForAddedAddress1},
		}
		listener.Update(add, nil)

		addrs := mockGetServer.updatesReceived[0].GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		// actualTLSIdentity := addrs[0].GetTlsIdentity().GetK8SPodIdentity()
		// if !reflect.DeepEqual(actualTLSIdentity, expectedTLSIdentity) {
		// 	t.Fatalf("Expected TlsIdentity to be [%v] but was [%v]", expectedTLSIdentity, actualTLSIdentity)
		// }
	})

	t.Run("Does not send TlsIdentity for other meshes", func(t *testing.T) {
		t.Skip("TLS is currently disabled")
		expectedPodName := "pod1"
		expectedPodNamespace := thisNS
		expectedControllerNamespace := "other-linkerd-namespace"
		expectedPodDeployment := podDeployment

		podForAddedAddress1 := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedPodName,
				Namespace: expectedPodNamespace,
				Annotations: map[string]string{
					pkgK8s.IdentityModeAnnotation: "FIXME",
				},
				Labels: map[string]string{
					pkgK8s.ControllerNSLabel:    expectedControllerNamespace,
					pkgK8s.ProxyDeploymentLabel: expectedPodDeployment,
				},
			},
			Status: v1.PodStatus{
				Phase: v1.PodRunning,
			},
		}

		ownerKindAndName := func(pod *v1.Pod) (string, string) {
			return deploymentKind, expectedPodDeployment
		}

		mockGetServer := &mockDestinationGetServer{updatesReceived: []*pb.Update{}}
		listener := newEndpointListener(
			mockGetServer,
			ownerKindAndName,
			false,
			"linkerd-namespace",
		)

		add := []*updateAddress{
			{address: addedAddress1, pod: podForAddedAddress1},
		}
		listener.Update(add, nil)

		addrs := mockGetServer.updatesReceived[0].GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		if addrs[0].TlsIdentity != nil {
			t.Fatalf("Expected no TlsIdentity to be sent, but got [%v]", addrs[0].TlsIdentity)
		}
	})

	t.Run("Does not send TlsIdentity when not enabled", func(t *testing.T) {
		expectedPodName := pod1.Name
		expectedPodNamespace := thisNS
		expectedControllerNamespace := "linkerd-namespace"
		expectedPodDeployment := podDeployment

		podForAddedAddress1 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedPodName,
				Namespace: expectedPodNamespace,
				Labels: map[string]string{
					pkgK8s.ControllerNSLabel:    expectedControllerNamespace,
					pkgK8s.ProxyDeploymentLabel: expectedPodDeployment,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		}

		ownerKindAndName := func(pod *corev1.Pod) (string, string) {
			return deploymentKind, expectedPodDeployment
		}

		mockGetServer := &mockDestinationGetServer{updatesReceived: []*pb.Update{}}
		listener := newEndpointListener(
			mockGetServer,
			ownerKindAndName,
			false,
			"linkerd",
		)

		add := []*updateAddress{
			{address: addedAddress1, pod: podForAddedAddress1},
		}
		listener.Update(add, nil)

		addrs := mockGetServer.updatesReceived[0].GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		if addrs[0].TlsIdentity != nil {
			t.Fatalf("Expected no TlsIdentity to be sent, but got [%v]", addrs[0].TlsIdentity)
		}
	})
}

func TestUpdateAddress(t *testing.T) {
	t.Run("Correctly clones an update address", func(t *testing.T) {
		ua := updateAddress{address: addedAddress1, pod: pod1}
		ua2 := ua.clone()
		if !reflect.DeepEqual(ua, *ua2) {
			t.Fatalf("Clone failed, original: %+v, clone: %+v", ua, ua2)
		}
	})
}

func checkAddress(t *testing.T, addr *pb.WeightedAddr, expectedAddress *net.TcpAddress) {
	actualAddress := addr.Addr
	actualWeight := addr.Weight
	expectedWeight := uint32(pkgAddr.DefaultWeight)

	if !reflect.DeepEqual(actualAddress, expectedAddress) || actualWeight != expectedWeight {
		t.Fatalf("Expected added address to be [%+v] and weight to be [%d], but it was [%+v] and [%d]", expectedAddress, expectedWeight, actualAddress, actualWeight)
	}
}
