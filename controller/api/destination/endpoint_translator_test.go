package destination

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	pkgk8s "github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	"github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/discovery/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	pod1 = watcher.Address{
		IP:   "1.1.1.1",
		Port: 1,
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "ns",
				Labels: map[string]string{
					k8s.ControllerNSLabel:    "linkerd",
					k8s.ProxyDeploymentLabel: "deployment-name",
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: "serviceaccount-name",
			},
		},
		OwnerKind: "replicationcontroller",
		OwnerName: "rc-name",
	}

	pod2 = watcher.Address{
		IP:   "1.1.1.2",
		Port: 2,
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: "ns",
				Labels: map[string]string{
					k8s.ControllerNSLabel:    "linkerd",
					k8s.ProxyDeploymentLabel: "deployment-name",
				},
			},
		},
	}

	pod3 = watcher.Address{
		IP:   "1.1.1.3",
		Port: 3,
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod3",
				Namespace: "ns",
				Labels: map[string]string{
					k8s.ControllerNSLabel:    "linkerd",
					k8s.ProxyDeploymentLabel: "deployment-name",
				},
			},
		},
	}

	remoteGateway1 = watcher.Address{
		IP:   "1.1.1.1",
		Port: 1,
	}

	remoteGateway2 = watcher.Address{
		IP:       "1.1.1.2",
		Port:     2,
		Identity: "some-identity",
	}

	remoteGatewayAuthOverride = watcher.Address{
		IP:                "1.1.1.2",
		Port:              2,
		Identity:          "some-identity",
		AuthorityOverride: "some-auth.com:2",
	}

	west1aAddress = watcher.Address{
		IP:   "1.1.1.1",
		Port: 1,
		ForZones: []v1beta1.ForZone{
			{Name: "west-1a"},
		},
	}
	west1bAddress = watcher.Address{
		IP:   "1.1.1.1",
		Port: 2,
		ForZones: []v1beta1.ForZone{
			{Name: "west-1b"},
		},
	}
)

func makeEndpointTranslator(t *testing.T) (*mockDestinationGetServer, *endpointTranslator) {
	k8sAPI, err := pkgk8s.NewFakeAPI(`
apiVersion: v1
kind: Node
metadata:
  annotations:
    kubeadm.alpha.kubernetes.io/cri-socket: /run/containerd/containerd.sock
    node.alpha.kubernetes.io/ttl: "0"
  labels:
    beta.kubernetes.io/arch: amd64
    kubernetes.io/os: linux
    kubernetes.io/arch: amd64
    kubernetes.io/hostname: kind-worker
    kubernetes.io/os: linux
    topology.kubernetes.io/region: west
    topology.kubernetes.io/zone: west-1a
  name: test-123
`,
	)
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}
	k8sAPI.Sync(nil)

	mockGetServer := &mockDestinationGetServer{updatesReceived: []*pb.Update{}}
	translator := newEndpointTranslator(
		"linkerd",
		"trust.domain",
		true,
		"service-name.service-ns",
		"test-123",
		map[uint32]struct{}{},
		k8sAPI.Node(),
		mockGetServer,
		logging.WithField("test", t.Name()),
	)
	return mockGetServer, translator
}

func TestEndpointTranslatorForRemoteGateways(t *testing.T) {
	t.Run("Sends one update for add and another for remove", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)

		translator.Add(mkAddressSetForServices(remoteGateway1, remoteGateway2))
		translator.Remove(mkAddressSetForServices(remoteGateway2))

		expectedNumUpdates := 2
		actualNumUpdates := len(mockGetServer.updatesReceived)
		if actualNumUpdates != expectedNumUpdates {
			t.Fatalf("Expecting [%d] updates, got [%d]. Updates: %v", expectedNumUpdates, actualNumUpdates, mockGetServer.updatesReceived)
		}
	})

	t.Run("Sends TlsIdentity when enabled", func(t *testing.T) {
		expectedTLSIdentity := &pb.TlsIdentity_DnsLikeIdentity{
			Name: "some-identity",
		}

		expectedProtocolHint := &pb.ProtocolHint{
			Protocol: &pb.ProtocolHint_H2_{
				H2: &pb.ProtocolHint_H2{},
			},
		}

		mockGetServer, translator := makeEndpointTranslator(t)

		translator.Add(mkAddressSetForServices(remoteGateway2))

		addrs := mockGetServer.updatesReceived[0].GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		actualTLSIdentity := addrs[0].GetTlsIdentity().GetDnsLikeIdentity()
		if !reflect.DeepEqual(actualTLSIdentity, expectedTLSIdentity) {
			t.Fatalf("Expected TlsIdentity to be [%v] but was [%v]", expectedTLSIdentity, actualTLSIdentity)
		}

		actualProtocolHint := addrs[0].GetProtocolHint()
		if !reflect.DeepEqual(actualProtocolHint, expectedProtocolHint) {
			t.Fatalf("Expected ProtocolHint to be [%v] but was [%v]", expectedProtocolHint, actualProtocolHint)
		}
	})

	t.Run("Sends TlsIdentity and Auth override when present", func(t *testing.T) {
		expectedTLSIdentity := &pb.TlsIdentity_DnsLikeIdentity{
			Name: "some-identity",
		}

		expectedProtocolHint := &pb.ProtocolHint{
			Protocol: &pb.ProtocolHint_H2_{
				H2: &pb.ProtocolHint_H2{},
			},
		}

		expectedAuthOverride := &pb.AuthorityOverride{
			AuthorityOverride: "some-auth.com:2",
		}

		mockGetServer, translator := makeEndpointTranslator(t)

		translator.Add(mkAddressSetForServices(remoteGatewayAuthOverride))

		addrs := mockGetServer.updatesReceived[0].GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		actualTLSIdentity := addrs[0].GetTlsIdentity().GetDnsLikeIdentity()
		if !reflect.DeepEqual(actualTLSIdentity, expectedTLSIdentity) {
			t.Fatalf("Expected TlsIdentity to be [%v] but was [%v]", expectedTLSIdentity, actualTLSIdentity)
		}

		actualProtocolHint := addrs[0].GetProtocolHint()
		if !reflect.DeepEqual(actualProtocolHint, expectedProtocolHint) {
			t.Fatalf("Expected ProtocolHint to be [%v] but was [%v]", expectedProtocolHint, actualProtocolHint)
		}

		actualAuthOverride := addrs[0].GetAuthorityOverride()
		if !reflect.DeepEqual(actualProtocolHint, expectedProtocolHint) {
			t.Fatalf("Expected AuthOverride to be [%v] but was [%v]", expectedAuthOverride, actualAuthOverride)
		}
	})

	t.Run("Does not send TlsIdentity when not present", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)

		translator.Add(mkAddressSetForServices(remoteGateway1))

		addrs := mockGetServer.updatesReceived[0].GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		if addrs[0].TlsIdentity != nil {
			t.Fatalf("Expected no TlsIdentity to be sent, but got [%v]", addrs[0].TlsIdentity)
		}
		if addrs[0].ProtocolHint != nil {
			t.Fatalf("Expected no ProtocolHint to be sent, but got [%v]", addrs[0].TlsIdentity)
		}
	})

}

func TestEndpointTranslatorForPods(t *testing.T) {
	t.Run("Sends one update for add and another for remove", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)

		translator.Add(mkAddressSetForPods(pod1, pod2))
		translator.Remove(mkAddressSetForPods(pod2))

		expectedNumUpdates := 2
		actualNumUpdates := len(mockGetServer.updatesReceived)
		if actualNumUpdates != expectedNumUpdates {
			t.Fatalf("Expecting [%d] updates, got [%d]. Updates: %v", expectedNumUpdates, actualNumUpdates, mockGetServer.updatesReceived)
		}
	})

	t.Run("Sends addresses as removed or added", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)

		translator.Add(mkAddressSetForPods(pod1, pod2, pod3))
		translator.Remove(mkAddressSetForPods(pod3))

		addressesAdded := mockGetServer.updatesReceived[0].GetAdd().Addrs
		actualNumberOfAdded := len(addressesAdded)
		expectedNumberOfAdded := 3
		if actualNumberOfAdded != expectedNumberOfAdded {
			t.Fatalf("Expecting [%d] addresses to be added, got [%d]: %v", expectedNumberOfAdded, actualNumberOfAdded, addressesAdded)
		}

		addressesRemoved := mockGetServer.updatesReceived[1].GetRemove().Addrs
		actualNumberOfRemoved := len(addressesRemoved)
		expectedNumberOfRemoved := 1
		if actualNumberOfRemoved != expectedNumberOfRemoved {
			t.Fatalf("Expecting [%d] addresses to be removed, got [%d]: %v", expectedNumberOfRemoved, actualNumberOfRemoved, addressesRemoved)
		}

		sort.Slice(addressesAdded, func(i, j int) bool {
			return addressesAdded[i].GetAddr().Port < addressesAdded[j].GetAddr().Port
		})
		checkAddressAndWeight(t, addressesAdded[0], pod1)
		checkAddressAndWeight(t, addressesAdded[1], pod2)
		checkAddress(t, addressesRemoved[0], pod3)
	})

	t.Run("Sends metric labels with added addresses", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)

		translator.Add(mkAddressSetForPods(pod1))

		actualGlobalMetricLabels := mockGetServer.updatesReceived[0].GetAdd().MetricLabels
		expectedGlobalMetricLabels := map[string]string{"namespace": "service-ns", "service": "service-name"}
		if !reflect.DeepEqual(actualGlobalMetricLabels, expectedGlobalMetricLabels) {
			t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", expectedGlobalMetricLabels, actualGlobalMetricLabels)
		}

		actualAddedAddress1MetricLabels := mockGetServer.updatesReceived[0].GetAdd().Addrs[0].MetricLabels
		expectedAddedAddress1MetricLabels := map[string]string{
			"pod":                   "pod1",
			"replicationcontroller": "rc-name",
			"serviceaccount":        "serviceaccount-name",
			"control_plane_ns":      "linkerd",
		}
		if !reflect.DeepEqual(actualAddedAddress1MetricLabels, expectedAddedAddress1MetricLabels) {
			t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", expectedAddedAddress1MetricLabels, actualAddedAddress1MetricLabels)
		}
	})

	t.Run("Sends TlsIdentity when enabled", func(t *testing.T) {
		expectedTLSIdentity := &pb.TlsIdentity_DnsLikeIdentity{
			Name: "serviceaccount-name.ns.serviceaccount.identity.linkerd.trust.domain",
		}

		mockGetServer, translator := makeEndpointTranslator(t)

		translator.Add(mkAddressSetForPods(pod1))

		addrs := mockGetServer.updatesReceived[0].GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		actualTLSIdentity := addrs[0].GetTlsIdentity().GetDnsLikeIdentity()
		if !reflect.DeepEqual(actualTLSIdentity, expectedTLSIdentity) {
			t.Fatalf("Expected TlsIdentity to be [%v] but was [%v]", expectedTLSIdentity, actualTLSIdentity)
		}
	})
}

func TestEndpointTranslatorForZonedAddresses(t *testing.T) {
	t.Run("Sends one update for add and none for remove", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)

		translator.Add(mkAddressSetForServices(west1aAddress, west1bAddress))
		translator.Remove(mkAddressSetForServices(west1bAddress))

		// Only the address meant for west-1a should be added, which means
		// that when we try to remove the address meant for west-1b there
		// should be no remove update.
		expectedNumUpdates := 1
		actualNumUpdates := len(mockGetServer.updatesReceived)
		if actualNumUpdates != expectedNumUpdates {
			t.Fatalf("Expecting [%d] updates, got [%d]. Updates: %v", expectedNumUpdates, actualNumUpdates, mockGetServer.updatesReceived)
		}
	})
}

func mkAddressSetForServices(gatewayAddresses ...watcher.Address) watcher.AddressSet {
	set := watcher.AddressSet{
		Addresses: make(map[watcher.ServiceID]watcher.Address),
		Labels:    map[string]string{"service": "service-name", "namespace": "service-ns"},
	}
	for _, a := range gatewayAddresses {
		a := a // pin

		id := watcher.ServiceID{
			Name: strings.Join([]string{
				a.IP,
				fmt.Sprint(a.Port),
			}, "-"),
		}
		set.Addresses[id] = a
	}
	return set
}

func mkAddressSetForPods(podAddresses ...watcher.Address) watcher.AddressSet {
	set := watcher.AddressSet{
		Addresses: make(map[watcher.PodID]watcher.Address),
		Labels:    map[string]string{"service": "service-name", "namespace": "service-ns"},
	}
	for _, p := range podAddresses {
		id := watcher.PodID{Name: p.Pod.Name, Namespace: p.Pod.Namespace}
		set.Addresses[id] = p
	}
	return set
}

func checkAddressAndWeight(t *testing.T, actual *pb.WeightedAddr, expected watcher.Address) {
	checkAddress(t, actual.GetAddr(), expected)
	if actual.GetWeight() != defaultWeight {
		t.Fatalf("Expected weight [%+v] but got [%+v]", defaultWeight, actual.GetWeight())
	}
}

func checkAddress(t *testing.T, actual *net.TcpAddress, expected watcher.Address) {
	expectedAddr, err := addr.ParseProxyIPV4(expected.IP)
	expectedTCP := net.TcpAddress{
		Ip:   expectedAddr,
		Port: expected.Port,
	}
	if err != nil {
		t.Fatalf("Failed to parse expected IP [%s]: %s", expected.IP, err)
	}
	if actual.Ip.GetIpv4() != expectedTCP.Ip.GetIpv4() {
		t.Fatalf("Expected IP [%+v] but got [%+v]", expectedTCP.Ip, actual.Ip)
	}
	if actual.Ip.GetIpv6() != expectedTCP.Ip.GetIpv6() {
		t.Fatalf("Expected IP [%+v] but got [%+v]", expectedTCP.Ip, actual.Ip)
	}
	if actual.Port != expectedTCP.Port {
		t.Fatalf("Expected port [%+v] but got [%+v]", expectedTCP.Port, actual.Port)
	}
}
