package destination

import (
	"context"
	"fmt"
	"testing"
	"time"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta1"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgk8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/testutil"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const fullyQualifiedName = "name1.ns.svc.mycluster.local"
const fullyQualifiedNameOpaque = "name3.ns.svc.mycluster.local"
const fullyQualifiedNameOpaqueService = "name4.ns.svc.mycluster.local"
const fullyQualifiedNameSkipped = "name5.ns.svc.mycluster.local"
const fullyQualifiedPodDNS = "pod-0.statefulset-svc.ns.svc.mycluster.local"
const clusterIP = "172.17.12.0"
const clusterIPOpaque = "172.17.12.1"
const podIP1 = "172.17.0.12"
const podIP2 = "172.17.0.13"
const podIPOpaque = "172.17.0.14"
const podIPSkipped = "172.17.0.15"
const podIPPolicy = "172.17.0.16"
const podIPStatefulSet = "172.17.13.15"
const externalIP = "192.168.1.20"
const port uint32 = 8989
const opaquePort uint32 = 4242
const skippedPort uint32 = 24224

func TestGet(t *testing.T) {
	t.Run("Returns error if not valid service name", func(t *testing.T) {
		server := makeServer(t)

		stream := &bufferingGetStream{
			updates:          []*pb.Update{},
			MockServerStream: util.NewMockServerStream(),
		}

		err := server.Get(&pb.GetDestination{Scheme: "k8s", Path: "linkerd.io"}, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Returns endpoints", func(t *testing.T) {
		server := makeServer(t)

		stream := &bufferingGetStream{
			updates:          []*pb.Update{},
			MockServerStream: util.NewMockServerStream(),
		}

		// We cancel the stream before even sending the request so that we don't
		// need to call server.Get in a separate goroutine.  By preemptively
		// cancelling, the behavior of Get becomes effectively synchronous and
		// we will get only the initial update, which is what we want for this
		// test.
		stream.Cancel()

		err := server.Get(&pb.GetDestination{Scheme: "k8s", Path: fmt.Sprintf("%s:%d", fullyQualifiedName, port)}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		if len(stream.updates) != 1 {
			t.Fatalf("Expected 1 update but got %d: %v", len(stream.updates), stream.updates)
		}

		if updateAddAddress(t, stream.updates[0])[0] != fmt.Sprintf("%s:%d", podIP1, port) {
			t.Fatalf("Expected %s but got %s", fmt.Sprintf("%s:%d", podIP1, port), updateAddAddress(t, stream.updates[0])[0])
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return endpoint with unknown protocol hint and identity when service name contains skipped inbound port", func(t *testing.T) {
		server := makeServer(t)
		stream := &bufferingGetStream{
			updates:          []*pb.Update{},
			MockServerStream: util.NewMockServerStream(),
		}
		stream.Cancel()

		path := fmt.Sprintf("%s:%d", fullyQualifiedNameSkipped, skippedPort)
		err := server.Get(&pb.GetDestination{
			Scheme: "k8s",
			Path:   path,
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		update := assertSingleUpdate(t, stream.updates)
		addrs := update.GetAdd().Addrs
		if len(addrs) == 0 {
			t.Fatalf("Expected len(addrs) to be > 0")
		}

		if addrs[0].GetProtocolHint().GetProtocol() != nil || addrs[0].GetProtocolHint().GetOpaqueTransport() != nil {
			t.Fatalf("Expected protocol hint for %s to be nil but got %+v", path, addrs[0].ProtocolHint)
		}

		if addrs[0].TlsIdentity != nil {
			t.Fatalf("Expected TLS identity for %s to be nil but got %+v", path, addrs[0].TlsIdentity)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Remote discovery", func(t *testing.T) {
		server := makeServer(t)

		// Wait for cluster store to be synced.
		time.Sleep(50 * time.Millisecond)

		stream := &bufferingGetStream{
			updates:          []*pb.Update{},
			MockServerStream: util.NewMockServerStream(),
		}

		// We cancel the stream before even sending the request so that we don't
		// need to call server.Get in a separate goroutine.  By preemptively
		// cancelling, the behavior of Get becomes effectively synchronous and
		// we will get only the initial update, which is what we want for this
		// test.
		stream.Cancel()

		err := server.Get(&pb.GetDestination{Scheme: "k8s", Path: fmt.Sprintf("%s:%d", "foo-target.ns.svc.mycluster.local", 80)}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		if len(stream.updates) != 1 {
			t.Fatalf("Expected 1 update but got %d: %v", len(stream.updates), stream.updates)
		}

		if updateAddAddress(t, stream.updates[0])[0] != fmt.Sprintf("%s:%d", "172.17.55.1", 80) {
			t.Fatalf("Expected %s but got %s", fmt.Sprintf("%s:%d", podIP1, port), updateAddAddress(t, stream.updates[0])[0])
		}

		server.clusterStore.UnregisterGauges()
	})
}

func TestGetProfiles(t *testing.T) {
	t.Run("Returns error if not valid service name", func(t *testing.T) {
		server := makeServer(t)
		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}
		defer stream.Cancel()
		err := server.GetProfile(&pb.GetDestination{Scheme: "k8s", Path: "linkerd.io"}, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Returns server profile", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, fullyQualifiedName, port, "ns:other")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.FullyQualifiedName != fullyQualifiedName {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'",
				fullyQualifiedName, profile.FullyQualifiedName)
		}
		if profile.OpaqueProtocol {
			t.Fatalf("Expected port %d to not be an opaque protocol, but it was", port)
		}
		routes := profile.GetRoutes()
		if len(routes) != 1 {
			t.Fatalf("Expected 0 routes but got %d: %v", len(routes), routes)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return service profile when using json token", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, fullyQualifiedName, port, `{"ns":"other"}`)
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.FullyQualifiedName != fullyQualifiedName {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'", fullyQualifiedName, profile.FullyQualifiedName)
		}
		routes := profile.GetRoutes()
		if len(routes) != 1 {
			t.Fatalf("Expected 1 route got %d: %v", len(routes), routes)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Returns client profile", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, fullyQualifiedName, port, `{"ns":"client-ns"}`)
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		routes := profile.GetRoutes()
		if len(routes) != 1 {
			t.Fatalf("Expected 1 route but got %d: %v", len(routes), routes)
		}
		if !routes[0].GetIsRetryable() {
			t.Fatalf("Expected route to be retryable, but it was not")
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return profile when using cluster IP", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, clusterIP, port, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.FullyQualifiedName != fullyQualifiedName {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'", fullyQualifiedName, profile.FullyQualifiedName)
		}
		if profile.OpaqueProtocol {
			t.Fatalf("Expected port %d to not be an opaque protocol, but it was", port)
		}
		routes := profile.GetRoutes()
		if len(routes) != 1 {
			t.Fatalf("Expected 1 route but got %d: %v", len(routes), routes)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return profile with endpoint when using pod DNS", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, fullyQualifiedPodDNS, port, "ns:ns")
		defer stream.Cancel()

		epAddr, err := toAddress(podIPStatefulSet, port)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// An explanation for why we expect 1 to 3 updates is in test cases
		// above
		updates := stream.Updates()
		if len(updates) == 0 || len(updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(updates), updates)
		}

		first := updates[0]
		if first.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if first.OpaqueProtocol {
			t.Fatalf("Expected port %d to not be an opaque protocol, but it was", port)
		}
		_, exists := first.Endpoint.MetricLabels["namespace"]
		if !exists {
			t.Fatalf("Expected 'namespace' metric label to exist but it did not")
		}
		if first.GetEndpoint().GetProtocolHint() == nil {
			t.Fatalf("Expected protocol hint but found none")
		}
		if first.GetEndpoint().GetProtocolHint().GetOpaqueTransport() != nil {
			t.Fatalf("Expected pod to not support opaque traffic on port %d", port)
		}
		if first.Endpoint.Addr.String() != epAddr.String() {
			t.Fatalf("Expected endpoint IP to be %s, but it was %s", epAddr.Ip, first.Endpoint.Addr.Ip)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return profile with endpoint when using pod IP", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, podIP1, port, "ns:ns")
		defer stream.Cancel()

		epAddr, err := toAddress(podIP1, port)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// An explanation for why we expect 1 to 3 updates is in test cases
		// above
		updates := stream.Updates()
		if len(updates) == 0 || len(updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(updates), updates)
		}

		first := updates[0]
		if first.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if first.OpaqueProtocol {
			t.Fatalf("Expected port %d to not be an opaque protocol, but it was", port)
		}
		_, exists := first.Endpoint.MetricLabels["namespace"]
		if !exists {
			t.Fatalf("Expected 'namespace' metric label to exist but it did not")
		}
		if first.GetEndpoint().GetProtocolHint() == nil {
			t.Fatalf("Expected protocol hint but found none")
		}
		if first.GetEndpoint().GetProtocolHint().GetOpaqueTransport() != nil {
			t.Fatalf("Expected pod to not support opaque traffic on port %d", port)
		}
		if first.Endpoint.Addr.String() != epAddr.String() {
			t.Fatalf("Expected endpoint IP to be %s, but it was %s", epAddr.Ip, first.Endpoint.Addr.Ip)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return default profile when IP does not map to service or pod", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, "172.0.0.0", 1234, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.RetryBudget == nil {
			t.Fatalf("Expected default profile to have a retry budget")
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return profile with no protocol hint when pod does not have label", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, podIP2, port, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if profile.Endpoint.GetProtocolHint().GetProtocol() != nil || profile.Endpoint.GetProtocolHint().GetOpaqueTransport() != nil {
			t.Fatalf("Expected no protocol hint but found one")
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return non-opaque protocol profile when using cluster IP and opaque protocol port", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, clusterIPOpaque, opaquePort, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.FullyQualifiedName != fullyQualifiedNameOpaque {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'", fullyQualifiedNameOpaque, profile.FullyQualifiedName)
		}
		if profile.OpaqueProtocol {
			t.Fatalf("Expected port %d to not be an opaque protocol, but it was", opaquePort)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return opaque protocol profile with endpoint when using pod IP and opaque protocol port", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, podIPOpaque, opaquePort, "")
		defer stream.Cancel()

		epAddr, err := toAddress(podIPOpaque, opaquePort)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// An explanation for why we expect 1 to 3 updates is in test cases
		// above
		updates := stream.Updates()
		if len(updates) == 0 || len(updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(updates), updates)
		}

		profile := assertSingleProfile(t, updates)
		if profile.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if !profile.OpaqueProtocol {
			t.Fatalf("Expected port %d to be an opaque protocol, but it was not", opaquePort)
		}
		_, exists := profile.Endpoint.MetricLabels["namespace"]
		if !exists {
			t.Fatalf("Expected 'namespace' metric label to exist but it did not")
		}
		if profile.Endpoint.ProtocolHint == nil {
			t.Fatalf("Expected protocol hint but found none")
		}
		if profile.Endpoint.ProtocolHint.GetOpaqueTransport().GetInboundPort() != 4143 {
			t.Fatalf("Expected pod to support opaque traffic on port 4143")
		}
		if profile.Endpoint.Addr.String() != epAddr.String() {
			t.Fatalf("Expected endpoint IP port to be %d, but it was %d", epAddr.Port, profile.Endpoint.Addr.Port)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return opaque protocol profile when using service name with opaque port annotation", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, fullyQualifiedNameOpaqueService, opaquePort, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.FullyQualifiedName != fullyQualifiedNameOpaqueService {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'", fullyQualifiedNameOpaqueService, profile.FullyQualifiedName)
		}
		if !profile.OpaqueProtocol {
			t.Fatalf("Expected port %d to be an opaque protocol, but it was not", opaquePort)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return profile with unknown protocol hint and identity when pod contains skipped inbound port", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, podIPSkipped, skippedPort, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		addr := profile.GetEndpoint()
		if addr == nil {
			t.Fatalf("Expected to not be nil")
		}
		if addr.GetProtocolHint().GetProtocol() != nil || addr.GetProtocolHint().GetOpaqueTransport() != nil {
			t.Fatalf("Expected protocol hint for %s to be nil but got %+v", podIPSkipped, addr.ProtocolHint)
		}
		if addr.TlsIdentity != nil {
			t.Fatalf("Expected TLS identity for %s to be nil but got %+v", podIPSkipped, addr.TlsIdentity)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return profile with opaque protocol when using Pod IP selected by a Server", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, podIPPolicy, 80, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if !profile.OpaqueProtocol {
			t.Fatalf("Expected port %d to be an opaque protocol, but it was not", 80)
		}
		if profile.Endpoint.ProtocolHint == nil {
			t.Fatalf("Expected protocol hint but found none")
		}
		if profile.Endpoint.ProtocolHint.GetOpaqueTransport().GetInboundPort() != 4143 {
			t.Fatalf("Expected pod to support opaque traffic on port 4143")
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return profile with opaque protocol when using an opaque port with an external IP", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, externalIP, 3306, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if !profile.OpaqueProtocol {
			t.Fatalf("Expected port %d to be an opaque protocol, but it was not", 3306)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return profile with non-opaque protocol when using an arbitrary port with an external IP", func(t *testing.T) {
		server := makeServer(t)
		stream := profileStream(t, server, externalIP, 80, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.OpaqueProtocol {
			t.Fatalf("Expected port %d to be a non-opaque protocol, but it was opaque", 80)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("Return profile for host port pods", func(t *testing.T) {
		hostPort := uint32(7777)
		containerPort := uint32(80)
		server, l5dClient := getServerWithClient(t)
		stream := profileStream(t, server, externalIP, hostPort, "")
		defer stream.Cancel()

		// HostPort maps to pod.
		profile := assertSingleProfile(t, stream.Updates())
		dstPod := profile.Endpoint.MetricLabels["pod"]
		if dstPod != "hostport-mapping" {
			t.Fatalf("Expected dst_pod to be %s got %s", "hostport-mapping", dstPod)
		}

		ip, err := addr.ParseProxyIPV4(externalIP)
		if err != nil {
			t.Fatalf("Error parsing IP: %s", err)
		}
		addr := profile.Endpoint.Addr
		if addr.Ip.String() != ip.String() && addr.Port != hostPort {
			t.Fatalf("Expected endpoint addr to be %s port:%d got %s", ip, hostPort, addr)
		}

		// HostPort pod is deleted.
		err = server.k8sAPI.Client.CoreV1().Pods("ns").Delete(context.Background(), "hostport-mapping", metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("Failed to delete pod: %s", err)
		}
		err = testutil.RetryFor(time.Second*10, func() error {
			updates := stream.Updates()
			if len(updates) < 2 {
				return fmt.Errorf("expected 2 updates, got %d", len(updates))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		profile = stream.Updates()[1]
		dstPod = profile.Endpoint.MetricLabels["pod"]
		if dstPod != "" {
			t.Fatalf("Expected no dst_pod but got %s", dstPod)
		}

		// New HostPort pod is created.
		_, err = server.k8sAPI.Client.CoreV1().Pods("ns").Create(context.Background(), &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "hostport-mapping-2",
				Namespace: "ns",
				Labels: map[string]string{
					"app": "hostport-mapping-2",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: pkgk8s.ProxyContainerName,
						Env: []corev1.EnvVar{
							{
								Name:  "LINKERD2_PROXY_INBOUND_LISTEN_ADDR",
								Value: "0.0.0.0:4143",
							},
						},
					},
					{
						Name:  "nginx",
						Image: "nginx",
						Ports: []corev1.ContainerPort{
							{
								Name:          "nginx-7777",
								ContainerPort: (int32)(containerPort),
								HostPort:      (int32)(hostPort),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: "Running",
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				},
				HostIP: externalIP,
				PodIP:  "172.17.0.55",
				PodIPs: []corev1.PodIP{{IP: "172.17.0.55"}},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create pod: %s", err)
		}

		err = testutil.RetryFor(time.Second*10, func() error {
			updates := stream.Updates()
			if len(updates) < 3 {
				return fmt.Errorf("expected 3 updates, got %d", len(updates))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		profile = stream.Updates()[2]
		dstPod = profile.Endpoint.MetricLabels["pod"]
		if dstPod != "hostport-mapping-2" {
			t.Fatalf("Expected dst_pod to be %s got %s", "hostport-mapping-2", dstPod)
		}
		if profile.OpaqueProtocol {
			t.Fatal("Expected OpaqueProtocol=false")
		}

		// Server is created, setting the port to opaque
		(*l5dClient).ServerV1beta1().Servers("ns").Create(context.Background(), &v1beta1.Server{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "srv-hostport-mapping-2",
				Namespace: "ns",
			},
			Spec: v1beta1.ServerSpec{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "hostport-mapping-2",
					},
				},
				Port: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "nginx-7777",
				},
				ProxyProtocol: "opaque",
			},
		}, metav1.CreateOptions{})

		var updates []*pb.DestinationProfile
		err = testutil.RetryFor(time.Second*10, func() error {
			updates = stream.Updates()
			if len(updates) < 4 {
				return fmt.Errorf("expected 4 updates, got %d", len(updates))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		profile = stream.Updates()[3]
		if !profile.OpaqueProtocol {
			t.Fatal("Expected OpaqueProtocol=true")
		}

		server.clusterStore.UnregisterGauges()
	})
}

func TestTokenStructure(t *testing.T) {
	t.Run("when JSON is valid", func(t *testing.T) {
		server := makeServer(t)
		dest := &pb.GetDestination{ContextToken: "{\"ns\":\"ns-1\",\"nodeName\":\"node-1\"}\n"}
		token := server.parseContextToken(dest.ContextToken)

		if token.Ns != "ns-1" {
			t.Fatalf("Expected token namespace to be %s got %s", "ns-1", token.Ns)
		}

		if token.NodeName != "node-1" {
			t.Fatalf("Expected token nodeName to be %s got %s", "node-1", token.NodeName)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("when JSON is invalid and old token format used", func(t *testing.T) {
		server := makeServer(t)
		dest := &pb.GetDestination{ContextToken: "ns:ns-2"}
		token := server.parseContextToken(dest.ContextToken)
		if token.Ns != "ns-2" {
			t.Fatalf("Expected %s got %s", "ns-2", token.Ns)
		}

		server.clusterStore.UnregisterGauges()
	})

	t.Run("when invalid JSON and invalid old format", func(t *testing.T) {
		server := makeServer(t)
		dest := &pb.GetDestination{ContextToken: "123fa-test"}
		token := server.parseContextToken(dest.ContextToken)
		if token.Ns != "" || token.NodeName != "" {
			t.Fatalf("Expected context token to be empty, got %v", token)
		}

		server.clusterStore.UnregisterGauges()
	})
}

func updateAddAddress(t *testing.T, update *pb.Update) []string {
	add, ok := update.GetUpdate().(*pb.Update_Add)
	if !ok {
		t.Fatalf("Update expected to be an add, but was %+v", update)
	}
	ips := []string{}
	for _, ip := range add.Add.Addrs {
		ips = append(ips, addr.ProxyAddressToString(ip.GetAddr()))
	}
	return ips
}

func toAddress(path string, port uint32) (*net.TcpAddress, error) {
	ip, err := addr.ParseProxyIPV4(path)
	if err != nil {
		return nil, err
	}
	return &net.TcpAddress{
		Ip:   ip,
		Port: port,
	}, nil
}

func TestIpWatcherGetSvcID(t *testing.T) {
	name := "service"
	namespace := "test"
	clusterIP := "10.256.0.1"
	var port uint32 = 1234
	k8sConfigs := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
spec:
  type: ClusterIP
  clusterIP: %s
  ports:
  - port: %d`, name, namespace, clusterIP, port)

	t.Run("get services IDs by IP address", func(t *testing.T) {
		k8sAPI, err := k8s.NewFakeAPI(k8sConfigs)
		if err != nil {
			t.Fatalf("NewFakeAPI returned an error: %s", err)
		}

		err = watcher.InitializeIndexers(k8sAPI)
		if err != nil {
			t.Fatalf("InitializeIndexers returned an error: %s", err)
		}

		k8sAPI.Sync(nil)

		svc, err := getSvcID(k8sAPI, clusterIP, logging.WithFields(nil))
		if err != nil {
			t.Fatalf("Error getting service: %s", err)
		}
		if svc == nil {
			t.Fatalf("Expected to find service mapped to [%s]", clusterIP)
		}
		if svc.Name != name {
			t.Fatalf("Expected service name to be [%s], but got [%s]", name, svc.Name)
		}
		if svc.Namespace != namespace {
			t.Fatalf("Expected service namespace to be [%s], but got [%s]", namespace, svc.Namespace)
		}

		badClusterIP := "10.256.0.2"
		svc, err = getSvcID(k8sAPI, badClusterIP, logging.WithFields(nil))
		if err != nil {
			t.Fatalf("Error getting service: %s", err)
		}
		if svc != nil {
			t.Fatalf("Expected not to find service mapped to [%s]", badClusterIP)
		}
	})
}

func assertSingleProfile(t *testing.T, updates []*pb.DestinationProfile) *pb.DestinationProfile {
	t.Helper()
	// Under normal conditions the creation of resources by the fake API will
	// generate notifications that are discarded after the stream.Cancel() call,
	// but very rarely those notifications might come after, in which case we'll
	// get a second update.
	if len(updates) != 1 {
		t.Fatalf("Expected 1 profile update but got %d: %v", len(updates), updates)
	}
	return updates[0]
}

func assertSingleUpdate(t *testing.T, updates []*pb.Update) *pb.Update {
	t.Helper()
	// Under normal conditions the creation of resources by the fake API will
	// generate notifications that are discarded after the stream.Cancel() call,
	// but very rarely those notifications might come after, in which case we'll
	// get a second update.
	if len(updates) == 0 || len(updates) > 2 {
		t.Fatalf("Expected 1 or 2 updates but got %d: %v", len(updates), updates)
	}
	return updates[0]
}

func profileStream(t *testing.T, server *server, host string, port uint32, token string) *bufferingGetProfileStream {
	t.Helper()

	stream := &bufferingGetProfileStream{
		updates:          []*pb.DestinationProfile{},
		MockServerStream: util.NewMockServerStream(),
	}

	go func() {
		err := server.GetProfile(&pb.GetDestination{
			Scheme:       "k8s",
			Path:         fmt.Sprintf("%s:%d", host, port),
			ContextToken: token,
		}, stream)
		if err != nil {
			logging.Fatalf("Got error: %s", err)
		}
	}()
	// Give GetProfile some slack
	time.Sleep(50 * time.Millisecond)

	return stream
}
