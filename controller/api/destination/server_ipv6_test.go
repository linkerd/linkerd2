package destination

import (
	"context"
	"testing"
	"time"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/util"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestIPv6(t *testing.T) {
	port := int32(port)
	protocol := corev1.ProtocolTCP

	server := makeServer(t)

	stream := &bufferingGetStream{
		updates:          make(chan *pb.Update, 50),
		MockServerStream: util.NewMockServerStream(),
	}
	defer stream.Cancel()

	t.Run("Return only IPv6 endpoint for dual-stack service", func(t *testing.T) {
		testReturnEndpointsForServer(t, server, stream, fullyQualifiedNameDual, podIPv6Dual, uint32(port))
	})

	t.Run("Returns only IPv4 endpoint when service becomes single-stack IPv4", func(t *testing.T) {
		patch := []byte(`{"spec":{"clusterIPs": ["172.17.13.0"], "ipFamilies":["IPv4"]}}`)
		_, err := server.k8sAPI.Client.CoreV1().Services("ns").Patch(context.Background(), "name-ds", types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			t.Fatalf("Failed patching name-ds service: %s", err)
		}
		if err = server.k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Delete(context.Background(), "name-ds-ipv6", metav1.DeleteOptions{}); err != nil {
			t.Fatalf("Failed deleting name-ds-ipv6 ES: %s", err)
		}

		update := <-stream.updates
		if updateAddAddress(t, update)[0] != "172.17.0.19:8989" {
			t.Fatalf("Expected %s but got %s", "172.17.0.19:8989", updateAddAddress(t, update)[0])
		}

		update = <-stream.updates
		if updateRemoveAddress(t, update)[0] != "[2001:db8::94]:8989" {
			t.Fatalf("Expected %s but got %s", "[2001:db8::94]:8989", updateRemoveAddress(t, update)[0])
		}
	})

	t.Run("Returns only IPv6 endpoint when service becomes dual-stack again", func(t *testing.T) {
		// We patch the service to become dual-stack again and we add the IPv6
		// ES. We should receive the events for the removal of the IPv4 ES and
		// the addition of the IPv6 one.
		patch := []byte(`{"spec":{"clusterIPs": ["172.17.13.0","2001:db8::88"], "ipFamilies":["IPv4","IPv6"]}}`)
		_, err := server.k8sAPI.Client.CoreV1().Services("ns").Patch(context.Background(), "name-ds", types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			t.Fatalf("Failed patching name-ds service: %s", err)
		}

		es := &discovery.EndpointSlice{
			TypeMeta: metav1.TypeMeta{
				Kind:       "EndpointSlice",
				APIVersion: "discovery.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "name-ds-ipv6",
				Namespace: "ns",
				Labels: map[string]string{
					"kubernetes.io/service-name": "name-ds",
				},
			},
			AddressType: discovery.AddressTypeIPv6,
			Ports: []discovery.EndpointPort{
				{
					Port:     &port,
					Protocol: &protocol,
				},
			},
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{"2001:db8::94"},
					TargetRef: &corev1.ObjectReference{
						Kind:      "Pod",
						Namespace: "ns",
						Name:      "name-ds",
					},
				},
			},
		}
		if _, err := server.k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Create(context.Background(), es, metav1.CreateOptions{}); err != nil {
			t.Fatalf("Failed creating name-ds-ipv6 ES: %s", err)
		}

		update := <-stream.updates
		if updateAddAddress(t, update)[0] != "[2001:db8::94]:8989" {
			t.Fatalf("Expected %s but got %s", "[2001:db8::94]:8989", updateAddAddress(t, update)[0])
		}

		update = <-stream.updates
		if updateRemoveAddress(t, update)[0] != "172.17.0.19:8989" {
			t.Fatalf("Expected %s but got %s", "172.17.0.19:8989", updateRemoveAddress(t, update)[0])
		}
	})

	t.Run("Doesn't return anything when adding an IPv4 to the dual-stack service", func(t *testing.T) {
		es := &discovery.EndpointSlice{
			TypeMeta: metav1.TypeMeta{
				Kind:       "EndpointSlice",
				APIVersion: "discovery.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "name-ds-ipv4-2",
				Namespace: "ns",
				Labels: map[string]string{
					"kubernetes.io/service-name": "name-ds",
				},
			},
			AddressType: discovery.AddressTypeIPv4,
			Ports: []discovery.EndpointPort{
				{
					Port:     &port,
					Protocol: &protocol,
				},
			},
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{"172.17.0.20"},
					TargetRef: &corev1.ObjectReference{
						Kind:      "Pod",
						Namespace: "ns",
						Name:      "name-ds",
					},
				},
			},
		}
		if _, err := server.k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Create(context.Background(), es, metav1.CreateOptions{}); err != nil {
			t.Fatalf("Failed creating name-ds-ipv4-2 ES: %s", err)
		}

		time.Sleep(50 * time.Millisecond)

		if len(stream.updates) != 0 {
			t.Fatalf("Expected no events but got %#v", stream.updates)
		}
	})

	t.Run("Doesn't return anything when removing an IPv4 ES from the dual-stack service", func(t *testing.T) {
		if err := server.k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Delete(context.Background(), "name-ds-ipv4-2", metav1.DeleteOptions{}); err != nil {
			t.Fatalf("Failed deleting name-ds-ipv4-2 ES: %s", err)
		}

		time.Sleep(50 * time.Millisecond)

		if len(stream.updates) != 0 {
			t.Fatalf("Expected no events but got %#v", stream.updates)
		}
	})

	t.Run("Doesn't return anything when the service becomes single-stack IPv6", func(t *testing.T) {
		patch := []byte(`{"spec":{"clusterIPs": ["2001:db8::88"], "ipFamilies":["IPv6"]}}`)
		_, err := server.k8sAPI.Client.CoreV1().Services("ns").Patch(context.Background(), "name-ds", types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			t.Fatalf("Failed patching name-ds service: %s", err)
		}
		if err := server.k8sAPI.Client.DiscoveryV1().EndpointSlices("ns").Delete(context.Background(), "name-ds-ipv4", metav1.DeleteOptions{}); err != nil {
			t.Fatalf("Failed deleting name-ds-ipv4 ES: %s", err)
		}

		time.Sleep(50 * time.Millisecond)

		if len(stream.updates) != 0 {
			t.Fatalf("Expected no events but got %#v", stream.updates)
		}
	})
}
