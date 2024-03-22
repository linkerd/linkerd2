package destination

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/go-test/deep"
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	ewv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	"github.com/linkerd/linkerd2/pkg/addr"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/discovery/v1"
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
		IP:   "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
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

	podOpaque = watcher.Address{
		IP:   "1.1.1.4",
		Port: 4,
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod4",
				Namespace: "ns",
				Labels: map[string]string{
					k8s.ControllerNSLabel:    "linkerd",
					k8s.ProxyDeploymentLabel: "deployment-name",
				},
				Annotations: map[string]string{
					k8s.ProxyOpaquePortsAnnotation: "4",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: k8s.ProxyContainerName,
						Env: []corev1.EnvVar{
							{
								Name:  envInboundListenAddr,
								Value: "0.0.0.0:4143",
							},
						},
					},
				},
			},
		},
		OpaqueProtocol: true,
	}

	ew1 = watcher.Address{
		IP:   "1.1.1.1",
		Port: 1,
		ExternalWorkload: &ewv1beta1.ExternalWorkload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ew-1",
				Namespace: "ns",
			},
			Spec: ewv1beta1.ExternalWorkloadSpec{
				MeshTLS: ewv1beta1.MeshTLS{
					Identity:   "spiffe://some-domain/ew-1",
					ServerName: "server.local",
				},
			},
		},
		OwnerKind: "workloadgroup",
		OwnerName: "wg-name",
	}

	ew2 = watcher.Address{
		IP:   "1.1.1.2",
		Port: 2,
		ExternalWorkload: &ewv1beta1.ExternalWorkload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ew-2",
				Namespace: "ns",
				Labels: map[string]string{
					k8s.ControllerNSLabel:    "linkerd",
					k8s.ProxyDeploymentLabel: "deployment-name",
				},
			},
			Spec: ewv1beta1.ExternalWorkloadSpec{
				MeshTLS: ewv1beta1.MeshTLS{
					Identity:   "spiffe://some-domain/ew-2",
					ServerName: "server.local",
				},
			},
		},
	}

	ew3 = watcher.Address{
		IP:   "1.1.1.3",
		Port: 3,
		ExternalWorkload: &ewv1beta1.ExternalWorkload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ew-3",
				Namespace: "ns",
				Labels: map[string]string{
					k8s.ControllerNSLabel:    "linkerd",
					k8s.ProxyDeploymentLabel: "deployment-name",
				},
			},
			Spec: ewv1beta1.ExternalWorkloadSpec{
				MeshTLS: ewv1beta1.MeshTLS{
					Identity:   "spiffe://some-domain/ew-3",
					ServerName: "server.local",
				},
			},
		},
	}

	ewOpaque = watcher.Address{
		IP:   "1.1.1.4",
		Port: 4,
		ExternalWorkload: &ewv1beta1.ExternalWorkload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod4",
				Namespace: "ns",
				Annotations: map[string]string{
					k8s.ProxyOpaquePortsAnnotation: "4",
				},
			},
			Spec: ewv1beta1.ExternalWorkloadSpec{
				MeshTLS: ewv1beta1.MeshTLS{
					Identity:   "spiffe://some-domain/ew-opaque",
					ServerName: "server.local",
				},

				Ports: []ewv1beta1.PortSpec{
					{
						Port: 4143,
						Name: "linkerd-proxy",
					},
				},
			},
		},
		OpaqueProtocol: true,
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
		ForZones: []v1.ForZone{
			{Name: "west-1a"},
		},
	}
	west1bAddress = watcher.Address{
		IP:   "1.1.1.1",
		Port: 2,
		ForZones: []v1.ForZone{
			{Name: "west-1b"},
		},
	}
	AddressOnTest123Node = watcher.Address{
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
				NodeName: "test-123",
			},
		},
	}
	AddressNotOnTest123Node = watcher.Address{
		IP:   "1.1.1.2",
		Port: 2,
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
				NodeName: "test-234",
			},
		},
	}
)

func TestEndpointTranslatorForRemoteGateways(t *testing.T) {
	t.Run("Sends one update for add and another for remove", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForServices(remoteGateway1, remoteGateway2))
		translator.Remove(mkAddressSetForServices(remoteGateway2))

		expectedNumUpdates := 2
		<-mockGetServer.updatesReceived // Add
		<-mockGetServer.updatesReceived // Remove

		if len(mockGetServer.updatesReceived) != 0 {
			t.Fatalf("Expecting [%d] updates, got [%d].", expectedNumUpdates, expectedNumUpdates+len(mockGetServer.updatesReceived))
		}
	})

	t.Run("Recovers after emptying address et", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForServices(remoteGateway1))
		translator.Remove(mkAddressSetForServices(remoteGateway1))
		translator.Add(mkAddressSetForServices(remoteGateway1))

		expectedNumUpdates := 3
		<-mockGetServer.updatesReceived // Add
		<-mockGetServer.updatesReceived // Remove
		<-mockGetServer.updatesReceived // Add

		if len(mockGetServer.updatesReceived) != 0 {
			t.Fatalf("Expecting [%d] updates, got [%d].", expectedNumUpdates, expectedNumUpdates+len(mockGetServer.updatesReceived))
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
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForServices(remoteGateway2))

		addrs := (<-mockGetServer.updatesReceived).GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		actualTLSIdentity := addrs[0].GetTlsIdentity().GetDnsLikeIdentity()
		if diff := deep.Equal(actualTLSIdentity, expectedTLSIdentity); diff != nil {
			t.Fatalf("TlsIdentity: %v", diff)
		}

		actualProtocolHint := addrs[0].GetProtocolHint()
		if diff := deep.Equal(actualProtocolHint, expectedProtocolHint); diff != nil {
			t.Fatalf("ProtocolHint: %v", diff)
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
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForServices(remoteGatewayAuthOverride))

		addrs := (<-mockGetServer.updatesReceived).GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		actualTLSIdentity := addrs[0].GetTlsIdentity().GetDnsLikeIdentity()
		if diff := deep.Equal(actualTLSIdentity, expectedTLSIdentity); diff != nil {
			t.Fatalf("TlsIdentity %v", diff)
		}

		actualProtocolHint := addrs[0].GetProtocolHint()
		if diff := deep.Equal(actualProtocolHint, expectedProtocolHint); diff != nil {
			t.Fatalf("ProtocolHint %v", diff)
		}

		actualAuthOverride := addrs[0].GetAuthorityOverride()
		if diff := deep.Equal(actualAuthOverride, expectedAuthOverride); diff != nil {
			t.Fatalf("AuthOverride %v", diff)
		}
	})

	t.Run("Does not send TlsIdentity when not present", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForServices(remoteGateway1))

		addrs := (<-mockGetServer.updatesReceived).GetAdd().GetAddrs()
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
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForPods(pod1, pod2))
		translator.Remove(mkAddressSetForPods(pod2))

		expectedNumUpdates := 2
		<-mockGetServer.updatesReceived // Add
		<-mockGetServer.updatesReceived // Remove

		if len(mockGetServer.updatesReceived) != 0 {
			t.Fatalf("Expecting [%d] updates, got [%d].", expectedNumUpdates, expectedNumUpdates+len(mockGetServer.updatesReceived))
		}
	})

	t.Run("Sends addresses as removed or added", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForPods(pod1, pod2, pod3))
		translator.Remove(mkAddressSetForPods(pod3))

		addressesAdded := (<-mockGetServer.updatesReceived).GetAdd().Addrs
		actualNumberOfAdded := len(addressesAdded)
		expectedNumberOfAdded := 3
		if actualNumberOfAdded != expectedNumberOfAdded {
			t.Fatalf("Expecting [%d] addresses to be added, got [%d]: %v", expectedNumberOfAdded, actualNumberOfAdded, addressesAdded)
		}

		addressesRemoved := (<-mockGetServer.updatesReceived).GetRemove().Addrs
		actualNumberOfRemoved := len(addressesRemoved)
		expectedNumberOfRemoved := 1
		if actualNumberOfRemoved != expectedNumberOfRemoved {
			t.Fatalf("Expecting [%d] addresses to be removed, got [%d]: %v", expectedNumberOfRemoved, actualNumberOfRemoved, addressesRemoved)
		}

		sort.Slice(addressesAdded, func(i, j int) bool {
			return addressesAdded[i].GetAddr().Port < addressesAdded[j].GetAddr().Port
		})
		checkAddressAndWeight(t, addressesAdded[0], pod1, defaultWeight)
		checkAddressAndWeight(t, addressesAdded[1], pod2, defaultWeight)
		checkAddress(t, addressesRemoved[0], pod3)
	})

	t.Run("Sends metric labels with added addresses", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForPods(pod1))

		update := <-mockGetServer.updatesReceived

		actualGlobalMetricLabels := update.GetAdd().MetricLabels
		expectedGlobalMetricLabels := map[string]string{"namespace": "service-ns", "service": "service-name"}
		if diff := deep.Equal(actualGlobalMetricLabels, expectedGlobalMetricLabels); diff != nil {
			t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", expectedGlobalMetricLabels, actualGlobalMetricLabels)
		}

		actualAddedAddress1MetricLabels := update.GetAdd().Addrs[0].MetricLabels
		expectedAddedAddress1MetricLabels := map[string]string{
			"pod":                   "pod1",
			"replicationcontroller": "rc-name",
			"serviceaccount":        "serviceaccount-name",
			"control_plane_ns":      "linkerd",
			"zone":                  "",
		}
		if diff := deep.Equal(actualAddedAddress1MetricLabels, expectedAddedAddress1MetricLabels); diff != nil {
			t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", expectedAddedAddress1MetricLabels, actualAddedAddress1MetricLabels)
		}
	})

	t.Run("Sends TlsIdentity when enabled", func(t *testing.T) {
		expectedTLSIdentity := &pb.TlsIdentity_DnsLikeIdentity{
			Name: "serviceaccount-name.ns.serviceaccount.identity.linkerd.trust.domain",
		}

		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForPods(pod1))

		addrs := (<-mockGetServer.updatesReceived).GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		actualTLSIdentity := addrs[0].GetTlsIdentity().GetDnsLikeIdentity()
		if diff := deep.Equal(actualTLSIdentity, expectedTLSIdentity); diff != nil {
			t.Fatalf("Expected TlsIdentity to be [%v] but was [%v]", expectedTLSIdentity, actualTLSIdentity)
		}
	})

	t.Run("Sends Opaque ProtocolHint for opaque ports", func(t *testing.T) {
		expectedProtocolHint := &pb.ProtocolHint{
			Protocol: &pb.ProtocolHint_Opaque_{
				Opaque: &pb.ProtocolHint_Opaque{},
			},
			OpaqueTransport: &pb.ProtocolHint_OpaqueTransport{
				InboundPort: 4143,
			},
		}

		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForServices(podOpaque))

		addrs := (<-mockGetServer.updatesReceived).GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		actualProtocolHint := addrs[0].GetProtocolHint()
		if diff := deep.Equal(actualProtocolHint, expectedProtocolHint); diff != nil {
			t.Fatalf("ProtocolHint: %v", diff)
		}
	})
}

func TestEndpointTranslatorExternalWorkloads(t *testing.T) {
	t.Run("Sends one update for add and another for remove", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForExternalWorkloads(ew1, ew2))
		translator.Remove(mkAddressSetForExternalWorkloads(ew2))

		expectedNumUpdates := 2
		<-mockGetServer.updatesReceived // Add
		<-mockGetServer.updatesReceived // Remove

		if len(mockGetServer.updatesReceived) != 0 {
			t.Fatalf("Expecting [%d] updates, got [%d].", expectedNumUpdates, expectedNumUpdates+len(mockGetServer.updatesReceived))
		}
	})

	t.Run("Sends addresses as removed or added", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForExternalWorkloads(ew1, ew2, ew3))
		translator.Remove(mkAddressSetForExternalWorkloads(ew3))

		addressesAdded := (<-mockGetServer.updatesReceived).GetAdd().Addrs
		actualNumberOfAdded := len(addressesAdded)
		expectedNumberOfAdded := 3
		if actualNumberOfAdded != expectedNumberOfAdded {
			t.Fatalf("Expecting [%d] addresses to be added, got [%d]: %v", expectedNumberOfAdded, actualNumberOfAdded, addressesAdded)
		}

		addressesRemoved := (<-mockGetServer.updatesReceived).GetRemove().Addrs
		actualNumberOfRemoved := len(addressesRemoved)
		expectedNumberOfRemoved := 1
		if actualNumberOfRemoved != expectedNumberOfRemoved {
			t.Fatalf("Expecting [%d] addresses to be removed, got [%d]: %v", expectedNumberOfRemoved, actualNumberOfRemoved, addressesRemoved)
		}

		sort.Slice(addressesAdded, func(i, j int) bool {
			return addressesAdded[i].GetAddr().Port < addressesAdded[j].GetAddr().Port
		})
		checkAddressAndWeight(t, addressesAdded[0], ew1, defaultWeight)
		checkAddressAndWeight(t, addressesAdded[1], ew2, defaultWeight)
		checkAddress(t, addressesRemoved[0], ew3)
	})

	t.Run("Sends metric labels with added addresses", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForExternalWorkloads(ew1))

		update := <-mockGetServer.updatesReceived

		actualGlobalMetricLabels := update.GetAdd().MetricLabels
		expectedGlobalMetricLabels := map[string]string{"namespace": "service-ns", "service": "service-name"}
		if diff := deep.Equal(actualGlobalMetricLabels, expectedGlobalMetricLabels); diff != nil {
			t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", expectedGlobalMetricLabels, actualGlobalMetricLabels)
		}

		actualAddedAddress1MetricLabels := update.GetAdd().Addrs[0].MetricLabels
		expectedAddedAddress1MetricLabels := map[string]string{
			"external_workload": "ew-1",
			"zone":              "",
			"workloadgroup":     "wg-name",
		}
		if diff := deep.Equal(actualAddedAddress1MetricLabels, expectedAddedAddress1MetricLabels); diff != nil {
			t.Fatalf("Expected global metric labels sent to be [%v] but was [%v]", expectedAddedAddress1MetricLabels, actualAddedAddress1MetricLabels)
		}
	})

	t.Run("Sends TlsIdentity and Server Name when enabled", func(t *testing.T) {
		expectedTLSIdentity := &pb.TlsIdentity{
			Strategy: &pb.TlsIdentity_UriLikeIdentity_{
				UriLikeIdentity: &pb.TlsIdentity_UriLikeIdentity{
					Uri: "spiffe://some-domain/ew-1",
				},
			},
			ServerName: &pb.TlsIdentity_DnsLikeIdentity{
				Name: "server.local",
			},
		}

		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForExternalWorkloads(ew1))
		addrs := (<-mockGetServer.updatesReceived).GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		actualTLSIdentity := addrs[0].GetTlsIdentity()
		if diff := deep.Equal(actualTLSIdentity, expectedTLSIdentity); diff != nil {
			t.Fatalf("Expected TlsIdentity to be [%v] but was [%v]", expectedTLSIdentity, actualTLSIdentity)
		}
	})

	t.Run("Sends Opaque ProtocolHint for opaque ports", func(t *testing.T) {
		expectedProtocolHint := &pb.ProtocolHint{
			Protocol: &pb.ProtocolHint_Opaque_{
				Opaque: &pb.ProtocolHint_Opaque{},
			},
			OpaqueTransport: &pb.ProtocolHint_OpaqueTransport{
				InboundPort: 4143,
			},
		}

		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForExternalWorkloads(ewOpaque))

		addrs := (<-mockGetServer.updatesReceived).GetAdd().GetAddrs()
		if len(addrs) != 1 {
			t.Fatalf("Expected [1] address returned, got %v", addrs)
		}

		actualProtocolHint := addrs[0].GetProtocolHint()
		if diff := deep.Equal(actualProtocolHint, expectedProtocolHint); diff != nil {
			t.Fatalf("ProtocolHint: %v", diff)
		}
	})
}

func TestEndpointTranslatorTopologyAwareFilter(t *testing.T) {
	t.Run("Sends one update for add and none for remove", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForServices(west1aAddress, west1bAddress))
		translator.Remove(mkAddressSetForServices(west1bAddress))

		// Only the address meant for west-1a should be added, which means
		// that when we try to remove the address meant for west-1b there
		// should be no remove update.
		expectedNumUpdates := 1
		<-mockGetServer.updatesReceived // Add

		if len(mockGetServer.updatesReceived) != 0 {
			t.Fatalf("Expecting [%d] updates, got [%d].", expectedNumUpdates, expectedNumUpdates+len(mockGetServer.updatesReceived))
		}
	})
}

func TestEndpointTranslatorExperimentalZoneWeights(t *testing.T) {
	zoneA := "west-1a"
	zoneB := "west-1b"
	addrA := watcher.Address{
		IP:   "7.9.7.9",
		Port: 7979,
		Zone: &zoneA,
	}
	addrB := watcher.Address{
		IP:   "9.7.9.7",
		Port: 9797,
		Zone: &zoneB,
	}

	t.Run("Disabled", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.extEndpointZoneWeights = false
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForServices(addrA, addrB))

		addrs := (<-mockGetServer.updatesReceived).GetAdd().GetAddrs()
		if len(addrs) != 2 {
			t.Fatalf("Expected [2] addresses returned, got %v", addrs)
		}
		sort.Slice(addrs, func(i, j int) bool {
			return addrs[i].GetAddr().Port < addrs[j].GetAddr().Port
		})
		checkAddressAndWeight(t, addrs[0], addrA, defaultWeight)
		checkAddressAndWeight(t, addrs[1], addrB, defaultWeight)
	})

	t.Run("Applies weights", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.extEndpointZoneWeights = true
		translator.Start()
		defer translator.Stop()

		translator.Add(mkAddressSetForServices(addrA, addrB))

		addrs := (<-mockGetServer.updatesReceived).GetAdd().GetAddrs()
		if len(addrs) != 2 {
			t.Fatalf("Expected [2] addresses returned, got %v", addrs)
		}
		sort.Slice(addrs, func(i, j int) bool {
			return addrs[i].GetAddr().Port < addrs[j].GetAddr().Port
		})
		checkAddressAndWeight(t, addrs[0], addrA, defaultWeight*10)
		checkAddressAndWeight(t, addrs[1], addrB, defaultWeight)
	})
}

func TestEndpointTranslatorForLocalTrafficPolicy(t *testing.T) {
	t.Run("Sends one update for add and none for remove", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()
		addressSet := mkAddressSetForServices(AddressOnTest123Node, AddressNotOnTest123Node)
		addressSet.LocalTrafficPolicy = true
		translator.Add(addressSet)
		translator.Remove(mkAddressSetForServices(AddressNotOnTest123Node))

		// Only the address meant for AddressOnTest123Node should be added, which means
		// that when we try to remove the address meant for AddressNotOnTest123Node there
		// should be no remove update.
		expectedNumUpdates := 1
		<-mockGetServer.updatesReceived // Add

		if len(mockGetServer.updatesReceived) != 0 {
			t.Fatalf("Expecting [%d] updates, got [%d].", expectedNumUpdates, expectedNumUpdates+len(mockGetServer.updatesReceived))
		}
	})

	t.Run("Removes cannot change LocalTrafficPolicy", func(t *testing.T) {
		mockGetServer, translator := makeEndpointTranslator(t)
		translator.Start()
		defer translator.Stop()
		addressSet := mkAddressSetForServices(AddressOnTest123Node, AddressNotOnTest123Node)
		addressSet.LocalTrafficPolicy = true
		translator.Add(addressSet)
		set := watcher.AddressSet{
			Addresses:          make(map[watcher.ServiceID]watcher.Address),
			Labels:             map[string]string{"service": "service-name", "namespace": "service-ns"},
			LocalTrafficPolicy: false,
		}
		translator.Remove(set)

		// Only the address meant for AddressOnTest123Node should be added.
		// The remove with no addresses should not change the LocalTrafficPolicy
		// and should be a noop that does not send an update.
		expectedNumUpdates := 1
		<-mockGetServer.updatesReceived // Add

		if len(mockGetServer.updatesReceived) != 0 {
			t.Fatalf("Expecting [%d] updates, got [%d].", expectedNumUpdates, expectedNumUpdates+len(mockGetServer.updatesReceived))
		}
	})
}

// TestConcurrency, to be triggered with `go test -race`, shouldn't report a race condition
func TestConcurrency(t *testing.T) {
	_, translator := makeEndpointTranslator(t)
	translator.Start()
	defer translator.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			translator.Add(mkAddressSetForServices(west1aAddress, west1bAddress))
			translator.Remove(mkAddressSetForServices(west1bAddress))
		}()
	}

	wg.Wait()
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

func mkAddressSetForExternalWorkloads(ewAddresses ...watcher.Address) watcher.AddressSet {
	set := watcher.AddressSet{
		Addresses: make(map[watcher.PodID]watcher.Address),
		Labels:    map[string]string{"service": "service-name", "namespace": "service-ns"},
	}
	for _, ew := range ewAddresses {
		id := watcher.ExternalWorkloadID{Name: ew.ExternalWorkload.Name, Namespace: ew.ExternalWorkload.Namespace}
		set.Addresses[id] = ew
	}
	return set
}

func checkAddressAndWeight(t *testing.T, actual *pb.WeightedAddr, expected watcher.Address, weight uint32) {
	t.Helper()

	checkAddress(t, actual.GetAddr(), expected)
	if actual.GetWeight() != weight {
		t.Fatalf("Expected weight [%+v] but got [%+v]", weight, actual.GetWeight())
	}
}

func checkAddress(t *testing.T, actual *net.TcpAddress, expected watcher.Address) {
	t.Helper()

	expectedAddr, err := addr.ParseProxyIP(expected.IP)
	expectedTCP := net.TcpAddress{
		Ip:   expectedAddr,
		Port: expected.Port,
	}
	if err != nil {
		t.Fatalf("Failed to parse expected IP [%s]: %s", expected.IP, err)
	}
	if actual.Ip.GetIpv4() == 0 && actual.Ip.GetIpv6() == nil {
		t.Fatal("Actual IP is empty")
	}
	if actual.Ip.GetIpv4() != expectedTCP.Ip.GetIpv4() {
		t.Fatalf("Expected IPv4 [%+v] but got [%+v]", expectedTCP.Ip, actual.Ip)
	}
	if !proto.Equal(actual.Ip.GetIpv6(), expectedTCP.Ip.GetIpv6()) {
		t.Fatalf("Expected IPv6 [%+v] but got [%+v]", expectedTCP.Ip, actual.Ip)
	}
	if actual.Port != expectedTCP.Port {
		t.Fatalf("Expected port [%+v] but got [%+v]", expectedTCP.Port, actual.Port)
	}
}
