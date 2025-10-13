package destination

import (
	"context"
	"errors"
	"fmt"
	gonet "net"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/duration"
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta3"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	pkgk8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const fullyQualifiedName = "name1.ns.svc.mycluster.local"
const fullyQualifiedNameIPv6 = "name-ipv6.ns.svc.mycluster.local"
const fullyQualifiedNameDual = "name-ds.ns.svc.mycluster.local"
const fullyQualifiedNameOpaque = "name3.ns.svc.mycluster.local"
const fullyQualifiedNameOpaqueService = "name4.ns.svc.mycluster.local"
const fullyQualifiedNameSkipped = "name5.ns.svc.mycluster.local"
const fullyQualifiedPodDNS = "pod-0.statefulset-svc.ns.svc.mycluster.local"
const clusterIP = "172.17.12.0"
const clusterIPv6 = "2001:db8::88"
const clusterIPOpaque = "172.17.12.1"
const podIP1 = "172.17.0.12"
const podIP1v6 = "2001:db8::68"
const podIPv6Dual = "2001:db8::94"
const podIP2 = "172.17.0.13"
const podIP3 = "172.17.0.20"
const podIP4 = "172.17.0.21"
const podIPOpaque = "172.17.0.14"
const podIPSkipped = "172.17.0.15"
const podIPPolicy = "172.17.0.16"
const podIPStatefulSet = "172.17.13.15"
const externalIP = "192.168.1.20"
const externalIPv6 = "2001:db8::78"
const externalWorkloadIP = "200.1.1.1"
const externalWorkloadIPPolicy = "200.1.1.2"
const port uint32 = 8989
const linkerdAdminPort uint32 = 4191
const opaquePort uint32 = 4242
const skippedPort uint32 = 24224
const policyPort uint32 = 80

func TestGet(t *testing.T) {
	t.Run("Returns error if not valid service name", func(t *testing.T) {
		server := makeServer(t)

		stream := &bufferingGetStream{
			updates:          make(chan *pb.Update, 50),
			MockServerStream: util.NewMockServerStream(),
		}

		err := server.Get(&pb.GetDestination{Scheme: "k8s", Path: "linkerd.io"}, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})

	t.Run("Returns InvalidArgument for ExternalName service", func(t *testing.T) {
		server := makeServer(t)

		stream := &bufferingGetStream{
			updates:          make(chan *pb.Update, 50),
			MockServerStream: util.NewMockServerStream(),
		}

		err := server.Get(&pb.GetDestination{Scheme: "k8s", Path: "externalname.ns.svc.cluster.local"}, stream)

		code := status.Code(err)
		if code != codes.InvalidArgument {
			t.Fatalf("Expected InvalidArgument, got %s", code)
		}
	})

	t.Run("Returns endpoints (IPv4)", func(t *testing.T) {
		testReturnEndpoints(t, fullyQualifiedName, podIP1, port)
	})

	t.Run("Returns endpoints (IPv6)", func(t *testing.T) {
		testReturnEndpoints(t, fullyQualifiedNameIPv6, podIP1v6, port)
	})

	t.Run("Returns endpoints (dual-stack)", func(t *testing.T) {
		testReturnEndpoints(t, fullyQualifiedNameDual, podIPv6Dual, port)
	})

	t.Run("Sets meshed HTTP/2 client params", func(t *testing.T) {
		server := makeServer(t)
		http2Params := pb.Http2ClientParams{
			KeepAlive: &pb.Http2ClientParams_KeepAlive{
				Timeout:  &duration.Duration{Seconds: 10},
				Interval: &duration.Duration{Seconds: 20},
			},
		}
		server.config.MeshedHttp2ClientParams = &http2Params

		stream := &bufferingGetStream{
			updates:          make(chan *pb.Update, 50),
			MockServerStream: util.NewMockServerStream(),
		}
		defer stream.Cancel()
		errs := make(chan error)

		// server.Get blocks until the grpc stream is complete so we call it
		// in a goroutine and watch stream.updates for updates.
		go func() {
			err := server.Get(&pb.GetDestination{Scheme: "k8s", Path: fmt.Sprintf("%s:%d", fullyQualifiedName, port)}, stream)
			if err != nil {
				errs <- err
			}
		}()

		select {
		case update := <-stream.updates:
			add, ok := update.GetUpdate().(*pb.Update_Add)
			if !ok {
				t.Fatalf("Update expected to be an add, but was %+v", update)
			}
			addr := add.Add.Addrs[0]
			if !reflect.DeepEqual(addr.GetHttp2(), &http2Params) {
				t.Fatalf("Expected HTTP/2 client params to be %v, but got %v", &http2Params, addr.GetHttp2())
			}
		case err := <-errs:
			t.Fatalf("Got error: %s", err)
		}
	})

	t.Run("Does not set unmeshed HTTP/2 client params", func(t *testing.T) {
		server := makeServer(t)
		http2Params := pb.Http2ClientParams{
			KeepAlive: &pb.Http2ClientParams_KeepAlive{
				Timeout:  &duration.Duration{Seconds: 10},
				Interval: &duration.Duration{Seconds: 20},
			},
		}
		server.config.MeshedHttp2ClientParams = &http2Params

		stream := &bufferingGetStream{
			updates:          make(chan *pb.Update, 50),
			MockServerStream: util.NewMockServerStream(),
		}
		defer stream.Cancel()
		errs := make(chan error)

		// server.Get blocks until the grpc stream is complete so we call it
		// in a goroutine and watch stream.updates for updates.
		go func() {
			err := server.Get(&pb.GetDestination{Scheme: "k8s", Path: fmt.Sprintf("%s:%d", "name2.ns.svc.mycluster.local", port)}, stream)
			if err != nil {
				errs <- err
			}
		}()

		select {
		case update := <-stream.updates:
			add, ok := update.GetUpdate().(*pb.Update_Add)
			if !ok {
				t.Fatalf("Update expected to be an add, but was %+v", update)
			}
			addr := add.Add.Addrs[0]
			if addr.GetHttp2() != nil {
				t.Fatalf("Expected HTTP/2 client params to be nil, but got %v", addr.GetHttp2())
			}
		case err := <-errs:
			t.Fatalf("Got error: %s", err)
		}
	})

	t.Run("Propagates client cancellation after first Send", func(t *testing.T) {
		server := makeServer(t)

		stream := &failingSendGetStream{
			MockServerStream: util.NewMockServerStream(),
			err:              status.Error(codes.Canceled, "client cancelled"),
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- server.Get(&pb.GetDestination{
				Scheme: "k8s",
				Path:   fmt.Sprintf("%s:%d", fullyQualifiedName, port),
			}, stream)
		}()

		select {
		case err := <-errCh:
			if !errors.Is(err, context.Canceled) && status.Code(err) != codes.Canceled {
				t.Fatalf("expected cancellation error, got %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for Get to return")
		}

		if stream.sendCalls == 0 {
			t.Fatal("expected at least one call to Send before cancellation")
		}
	})

	t.Run("Cancels stream when endpoint update queue overflows", func(t *testing.T) {
		srv := makeServer(t)

		runEndpointOverflowTest(t, endpointOverflowScenario{
			server:      srv,
			servicePath: fmt.Sprintf("%s:%d", fullyQualifiedName, port),
			metricLabels: prometheus.Labels{
				"service": fmt.Sprintf("%s:%d", fullyQualifiedName, port),
			},
			waitMessage: fmt.Sprintf("endpoint translator overflow for %s:%d", fullyQualifiedName, port),
			trigger: func(t *testing.T, srv *server, i int) {
				addEndpointToSlice(t, srv, fmt.Sprintf("name1-overflow-%d", i), fmt.Sprintf("172.17.0.%d", i))
				time.Sleep(10 * time.Millisecond)
			},
		})
	})

	t.Run("Cancels stream when federated endpoint update queue overflows", func(t *testing.T) {
		srv, remoteStore := getServerWithRemoteStore(t)
		remoteAPI, ok := remoteStore.Get("target")
		if !ok {
			t.Fatal("remote cluster API not found")
		}

		runEndpointOverflowTest(t, endpointOverflowScenario{
			server:      srv,
			servicePath: "foo-federated.ns.svc.mycluster.local:80",
			metricLabels: prometheus.Labels{
				"service": "foo.ns.svc.cluster.local:80",
			},
			waitMessage: "federated endpoint translator overflow for foo.ns.svc.cluster.local:80",
			prepare: func(t *testing.T, srv *server) {
				ctx := context.Background()
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo-federated",
						Namespace: "ns",
						Annotations: map[string]string{
							pkgk8s.RemoteDiscoveryAnnotation: "foo@target",
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{Port: 80},
						},
					},
				}

				if _, err := srv.k8sAPI.Client.CoreV1().Services("ns").Create(ctx, svc, metav1.CreateOptions{}); err != nil {
					t.Fatalf("failed to create federated service: %v", err)
				}

				id := watcher.ServiceID{Namespace: "ns", Name: "foo-federated"}
				deadline := time.Now().Add(5 * time.Second)
				for {
					srv.federatedServices.RLock()
					_, found := srv.federatedServices.services[id]
					srv.federatedServices.RUnlock()
					if found {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("federated service %s/%s not registered", "ns", "foo-federated")
					}
					time.Sleep(10 * time.Millisecond)
				}
			},
			trigger: func(t *testing.T, _ *server, i int) {
				addEndpointToRemoteSlice(t, remoteAPI, "ns", "foo", fmt.Sprintf("172.17.155.%d", i+1))
				time.Sleep(50 * time.Millisecond)
			},
		})
	})

	t.Run("Return endpoint with unknown protocol hint and identity when service name contains skipped inbound port", func(t *testing.T) {
		server := makeServer(t)

		stream := &bufferingGetStream{
			updates:          make(chan *pb.Update, 50),
			MockServerStream: util.NewMockServerStream(),
		}
		defer stream.Cancel()
		errs := make(chan error)

		path := fmt.Sprintf("%s:%d", fullyQualifiedNameSkipped, skippedPort)

		// server.Get blocks until the grpc stream is complete so we call it
		// in a goroutine and watch stream.updates for updates.
		go func() {
			err := server.Get(&pb.GetDestination{
				Scheme: "k8s",
				Path:   path,
			}, stream)
			if err != nil {
				errs <- err
			}
		}()

		select {
		case update := <-stream.updates:
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
		case err := <-errs:
			t.Fatalf("Got error: %s", err)
		}
	})

	t.Run("Return endpoint opaque protocol controlled by a server", func(t *testing.T) {
		testOpaque(t, "policy-test")
	})

	t.Run("Return endpoint opaque protocol controlled by a server (native sidecar)", func(t *testing.T) {
		testOpaque(t, "native")
	})

	t.Run("Remote discovery", func(t *testing.T) {
		server := makeServer(t)

		// Wait for cluster store to be synced.
		time.Sleep(50 * time.Millisecond)

		stream := &bufferingGetStream{
			updates:          make(chan *pb.Update, 50),
			MockServerStream: util.NewMockServerStream(),
		}
		defer stream.Cancel()
		errs := make(chan error)

		// server.Get blocks until the grpc stream is complete so we call it
		// in a goroutine and watch stream.updates for updates.
		go func() {
			err := server.Get(&pb.GetDestination{Scheme: "k8s", Path: fmt.Sprintf("%s:%d", "foo-target.ns.svc.mycluster.local", 80)}, stream)
			if err != nil {
				errs <- err
			}
		}()

		select {
		case update := <-stream.updates:
			if updateAddAddress(t, update)[0] != fmt.Sprintf("%s:%d", "172.17.55.1", 80) {
				t.Fatalf("Expected %s but got %s", fmt.Sprintf("%s:%d", podIP1, port), updateAddAddress(t, update)[0])
			}

			if len(stream.updates) != 0 {
				t.Fatalf("Expected 1 update but got %d: %v", 1+len(stream.updates), stream.updates)
			}

		case err := <-errs:
			t.Fatalf("Got error: %s", err)
		}
	})
}

func testOpaque(t *testing.T, name string) {
	server, client := getServerWithClient(t)

	stream := &bufferingGetStream{
		updates:          make(chan *pb.Update, 50),
		MockServerStream: util.NewMockServerStream(),
	}
	defer stream.Cancel()
	errs := make(chan error)

	path := fmt.Sprintf("%s.ns.svc.mycluster.local:%d", name, 80)

	// server.Get blocks until the grpc stream is complete so we call it
	// in a goroutine and watch stream.updates for updates.
	go func() {
		err := server.Get(&pb.GetDestination{
			Scheme: "k8s",
			Path:   path,
		}, stream)
		if err != nil {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		t.Fatalf("Got error: %s", err)
	case update := <-stream.updates:
		addrs := update.GetAdd().Addrs
		if len(addrs) == 0 {
			t.Fatalf("Expected len(addrs) to be > 0")
		}

		if addrs[0].GetProtocolHint().GetOpaqueTransport() == nil {
			t.Fatalf("Expected opaque transport for %s but was nil", path)
		}
	}

	// Update the Server's pod selector so that it no longer selects the
	// pod. This should result in the proxy protocol no longer being marked
	// as opaque.
	srv, err := client.ServerV1beta3().Servers("ns").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// PodSelector is updated to NOT select the pod
	srv.Spec.PodSelector.MatchLabels = map[string]string{"app": "FOOBAR"}
	_, err = client.ServerV1beta3().Servers("ns").Update(context.Background(), srv, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case update := <-stream.updates:
		addrs := update.GetAdd().Addrs
		if len(addrs) == 0 {
			t.Fatalf("Expected len(addrs) to be > 0")
		}

		if addrs[0].GetProtocolHint().GetOpaqueTransport() != nil {
			t.Fatalf("Expected opaque transport to be nil for %s but was %+v", path, *addrs[0].GetProtocolHint().GetOpaqueTransport())
		}
	case err := <-errs:
		t.Fatalf("Got error: %s", err)
	}

	// Update the Server's pod selector so that it once again selects the
	// pod. This should result in the proxy protocol once again being marked
	// as opaque.
	srv.Spec.PodSelector.MatchLabels = map[string]string{"app": name}

	_, err = client.ServerV1beta3().Servers("ns").Update(context.Background(), srv, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case update := <-stream.updates:
		addrs := update.GetAdd().Addrs
		if len(addrs) == 0 {
			t.Fatalf("Expected len(addrs) to be > 0")
		}

		if addrs[0].GetProtocolHint().GetOpaqueTransport() == nil {
			t.Fatalf("Expected opaque transport for %s but was nil", path)
		}
	case err := <-errs:
		t.Fatalf("Got error: %s", err)
	}
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
	})

	t.Run("Returns InvalidArgument for ExternalName service", func(t *testing.T) {
		server := makeServer(t)

		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}
		defer stream.Cancel()

		err := server.GetProfile(&pb.GetDestination{Scheme: "k8s", Path: "externalname.ns.svc.cluster.local"}, stream)
		code := status.Code(err)
		if code != codes.InvalidArgument {
			t.Fatalf("Expected InvalidArgument, got %s", code)
		}
	})

	t.Run("Propagates client cancellation after first Send", func(t *testing.T) {
		server := makeServer(t)

		stream := &failingSendProfileStream{
			MockServerStream: util.NewMockServerStream(),
			err:              status.Error(codes.Canceled, "client cancelled"),
		}

		errCh := make(chan error, 1)
		go func() {
			errCh <- server.GetProfile(&pb.GetDestination{
				Scheme: "k8s",
				Path:   fmt.Sprintf("%s:%d", fullyQualifiedName, port),
			}, stream)
		}()

		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for GetProfile to return")
		}

		if stream.sendCalls == 0 {
			t.Fatal("expected Send to be called at least once before cancellation")
		}
	})

	t.Run("Cancels blocked stream when translator overflows", func(t *testing.T) {
		server, client := getServerWithClient(t)

		stream := newBlockingProfileStream()
		errCh := make(chan error, 1)

		go func() {
			errCh <- server.GetProfile(&pb.GetDestination{
				Scheme: "k8s",
				Path:   fmt.Sprintf("%s:%d", fullyQualifiedName, port),
			}, stream)
		}()

		sendDeadline := time.Now().Add(5 * time.Second)
		for {
			if stream.Calls() > 0 {
				break
			}
			if time.Now().After(sendDeadline) {
				t.Fatal("waiting for initial profile send")
			}
			time.Sleep(10 * time.Millisecond)
		}

		metric, err := profileUpdatesQueueOverflowCounter.GetMetricWith(prometheus.Labels{
			"fqn":  fullyQualifiedName,
			"port": fmt.Sprintf("%d", port),
		})
		if err != nil {
			t.Fatalf("failed to get overflow counter: %v", err)
		}
		initialOverflow := counterValue(t, metric)

		// Trigger enough profile updates so the translator tries to enqueue more
		// messages than the buffered queue can hold while the stream remains
		// blocked.
		profiles := client.LinkerdV1alpha2().ServiceProfiles("ns")
		for i := 0; i < updateQueueCapacity+10; i++ {
			profile, err := profiles.Get(context.TODO(), fullyQualifiedName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to fetch service profile: %v", err)
			}
			cp := profile.DeepCopy()
			if len(cp.Spec.Routes) == 0 {
				t.Fatal("service profile missing routes")
			}
			cp.Spec.Routes[0].Name = fmt.Sprintf("route-%d", i)
			if _, err := profiles.Update(context.TODO(), cp, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("failed to update service profile: %v", err)
			}
			if counterValue(t, metric) > initialOverflow {
				break
			}
		}

		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for GetProfile to return after overflow")
		}
		if counterValue(t, metric) == initialOverflow {
			t.Fatal("profile translator should overflow")
		}
	})

	t.Run("Cancels blocked stream for endpoint profiles when translator overflows", func(t *testing.T) {
		t.Skip("TODO: fix overflow")

		server := makeServer(t)

		stream := newBlockingProfileStream()
		errCh := make(chan error, 1)

		go func() {
			errCh <- server.GetProfile(&pb.GetDestination{
				Scheme: "k8s",
				Path:   gonet.JoinHostPort(podIPPolicy, fmt.Sprintf("%d", policyPort)),
			}, stream)
		}()

		endpointSendDeadline := time.Now().Add(5 * time.Second)
		for {
			if stream.Calls() > 0 {
				break
			}
			if time.Now().After(endpointSendDeadline) {
				t.Fatal("waiting for initial profile send")
			}
			time.Sleep(10 * time.Millisecond)
		}

		initialOverflow := counterValue(t, endpointProfileUpdatesQueueOverflowCounter)
		pods := server.k8sAPI.Client.CoreV1().Pods("ns")
		original, err := pods.Get(context.TODO(), "policy-test", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to fetch pod: %v", err)
		}

		time.Sleep(50 * time.Millisecond)

		recreated := original.DeepCopy()
		recreated.ResourceVersion = ""
		recreated.UID = ""
		recreated.CreationTimestamp = metav1.Time{}
		recreated.ManagedFields = nil
		if recreated.Labels == nil {
			recreated.Labels = map[string]string{}
		}
		recreated.Labels["overflow-test"] = "true"

		if _, err := pods.Create(context.TODO(), recreated, metav1.CreateOptions{}); err != nil {
			t.Fatalf("failed to recreate pod: %v", err)
		}

		for i := 0; i < updateQueueCapacity+10; i++ {
			if counterValue(t, endpointProfileUpdatesQueueOverflowCounter) > initialOverflow {
				break
			}
			current, err := pods.Get(context.TODO(), recreated.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to fetch pod: %v", err)
			}
			cp := current.DeepCopy()
			if cp.Labels == nil {
				cp.Labels = map[string]string{}
			}
			cp.Labels["overflow-test"] = fmt.Sprintf("true-%d", i)
			if _, err := pods.Update(context.TODO(), cp, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("failed to update pod labels: %v", err)
			}
		}

		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for GetProfile to return after overflow")
		}
		if counterValue(t, endpointProfileUpdatesQueueOverflowCounter) == initialOverflow {
			t.Fatal("endpoint profile translator overflow")
		}
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
	})

	t.Run("Return profile when using secondary cluster IP", func(t *testing.T) {
		server := makeServer(t)

		stream := profileStream(t, server, clusterIPv6, port, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.FullyQualifiedName != fullyQualifiedNameDual {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'", fullyQualifiedName, profile.FullyQualifiedName)
		}
		if profile.OpaqueProtocol {
			t.Fatalf("Expected port %d to not be an opaque protocol, but it was", port)
		}
		routes := profile.GetRoutes()
		if len(routes) != 1 {
			t.Fatalf("Expected 1 route but got %d: %v", len(routes), routes)
		}
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
		if first.Endpoint.Addr.Ip.GetIpv4() == 0 && first.Endpoint.Addr.Ip.GetIpv6() == nil {
			t.Fatal("IP is empty")
		}
		if first.Endpoint.Addr.String() != epAddr.String() {
			t.Fatalf("Expected endpoint IP to be %s, but it was %s", epAddr.Ip, first.Endpoint.Addr.Ip)
		}
	})

	t.Run("Return profile with endpoint when using pod IP", func(t *testing.T) {
		server := makeServer(t)
		http2Params := pb.Http2ClientParams{
			KeepAlive: &pb.Http2ClientParams_KeepAlive{
				Timeout:  &duration.Duration{Seconds: 10},
				Interval: &duration.Duration{Seconds: 20},
			},
		}
		server.config.MeshedHttp2ClientParams = &http2Params

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
		if first.Endpoint.Addr.Ip.GetIpv4() == 0 && first.Endpoint.Addr.Ip.GetIpv6() == nil {
			t.Fatal("IP is empty")
		}
		if first.Endpoint.Addr.String() != epAddr.String() {
			t.Fatalf("Expected endpoint IP to be %s, but it was %s", epAddr.Ip, first.Endpoint.Addr.Ip)
		}
		if !reflect.DeepEqual(first.Endpoint.GetHttp2(), &http2Params) {
			t.Fatalf("Expected HTTP/2 client params to be %v, but got %v", &http2Params, first.Endpoint.GetHttp2())
		}
	})

	t.Run("Return profile with endpoint when using pod secondary IP", func(t *testing.T) {
		server := makeServer(t)

		stream := profileStream(t, server, podIPv6Dual, port, "ns:ns")
		defer stream.Cancel()

		epAddr, err := toAddress(podIPv6Dual, port)
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
		if first.Endpoint.Addr.Ip.GetIpv4() == 0 && first.Endpoint.Addr.Ip.GetIpv6() == nil {
			t.Fatal("IP is empty")
		}
		if first.Endpoint.Addr.String() != epAddr.String() {
			t.Fatalf("Expected endpoint IP to be %s, but it was %s", epAddr.Ip, first.Endpoint.Addr.Ip)
		}
	})

	t.Run("Profile gets updated after pod becomes ready", func(t *testing.T) {
		server := makeServer(t)

		// podIP3 is initially not ready
		stream := profileStream(t, server, podIP3, linkerdAdminPort, "ns:ns")
		defer stream.Cancel()

		updates := stream.Updates()
		if len(updates) != 1 {
			t.Fatalf("Expected 1 update but got %d: %v", len(updates), updates)
		}

		profile := updates[0]
		if profile.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if profile.Endpoint.TlsIdentity != nil {
			t.Fatalf("Expected endpoint TLS identity to be nil but was %v", profile.Endpoint.TlsIdentity)
		}

		pod, err := server.k8sAPI.Client.CoreV1().Pods("ns").Get(context.Background(), "name1-20", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to retrieve pod: %s", err)
		}
		pod.Status.Phase = corev1.PodRunning
		_, err = server.k8sAPI.Client.CoreV1().Pods("ns").Update(context.Background(), pod, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("Failed to update pod: %s", err)
		}

		profile = getLastProfileUpdate(t, stream, 2)
		if profile.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if profile.Endpoint.TlsIdentity == nil {
			t.Fatalf("Expected endpoint TLS identity to not be nil")
		}
	})

	t.Run("Profile gets updated after pod becomes ready (native sidecar)", func(t *testing.T) {
		server := makeServer(t)

		// podIP3 is initially not ready
		stream := profileStream(t, server, podIP4, linkerdAdminPort, "ns:ns")
		defer stream.Cancel()

		updates := stream.Updates()
		if len(updates) != 1 {
			t.Fatalf("Expected 1 update but got %d: %v", len(updates), updates)
		}

		profile := updates[0]
		if profile.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if profile.Endpoint.TlsIdentity != nil {
			t.Fatalf("Expected endpoint TLS identity to be nil but was %v", profile.Endpoint.TlsIdentity)
		}

		pod, err := server.k8sAPI.Client.CoreV1().Pods("ns").Get(context.Background(), "name1-21", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to retrieve pod: %s", err)
		}
		pod.Status.Phase = corev1.PodRunning
		_, err = server.k8sAPI.Client.CoreV1().Pods("ns").Update(context.Background(), pod, metav1.UpdateOptions{})
		if err != nil {
			t.Fatalf("Failed to update pod: %s", err)
		}

		profile = getLastProfileUpdate(t, stream, 2)
		if profile.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if profile.Endpoint.TlsIdentity == nil {
			t.Fatalf("Expected endpoint TLS identity to not be nil")
		}
	})

	t.Run("Return profile with endpoint when using externalworkload IP", func(t *testing.T) {
		server := makeServer(t)
		http2Params := pb.Http2ClientParams{
			KeepAlive: &pb.Http2ClientParams_KeepAlive{
				Timeout:  &duration.Duration{Seconds: 10},
				Interval: &duration.Duration{Seconds: 20},
			},
		}
		server.config.MeshedHttp2ClientParams = &http2Params

		stream := profileStream(t, server, externalWorkloadIP, port, "ns:ns")
		defer stream.Cancel()

		epAddr, err := toAddress(externalWorkloadIP, port)
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
			t.Fatalf("Expected 'namespace' metric label to exist but it did not %v", first.Endpoint)
		}
		if first.GetEndpoint().GetProtocolHint() == nil {
			t.Fatalf("Expected protocol hint but found none")
		}
		if first.GetEndpoint().GetProtocolHint().GetOpaqueTransport() != nil {
			t.Fatalf("Expected externalworkload to not support opaque traffic on port %d", port)
		}
		if first.Endpoint.Addr.Ip.GetIpv4() == 0 && first.Endpoint.Addr.Ip.GetIpv6() == nil {
			t.Fatal("IP is empty")
		}
		if first.Endpoint.Addr.String() != epAddr.String() {
			t.Fatalf("Expected endpoint IP to be %s, but it was %s", epAddr.Ip, first.Endpoint.Addr.Ip)
		}
		if !reflect.DeepEqual(first.Endpoint.GetHttp2(), &http2Params) {
			t.Fatalf("Expected HTTP/2 client params to be %v, but got %v", &http2Params, first.Endpoint.GetHttp2())
		}
	})

	t.Run("Return default profile when IP does not map to service or pod", func(t *testing.T) {
		server := makeServer(t)

		stream := profileStream(t, server, "172.0.0.0", 1234, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.RetryBudget == nil {
			t.Fatalf("Expected default profile to have a retry budget")
		}
	})

	t.Run("Return profile with no opaque transport when pod does not have label and port is opaque", func(t *testing.T) {
		server := makeServer(t)

		// port 3306 is in the default opaque port list
		stream := profileStream(t, server, podIP2, 3306, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}

		if profile.Endpoint.GetProtocolHint().GetOpaqueTransport() != nil {
			t.Fatalf("Expected no opaque transport but found one")
		}
		if profile.GetEndpoint().GetHttp2() != nil {
			t.Fatalf("Expected no HTTP/2 client parameters but found one")
		}
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
	})

	t.Run("Return profile with protocol hint for default opaque port when pod is unmeshed", func(t *testing.T) {
		server := makeServer(t)

		// 3306 is in the default opaque list
		stream := profileStream(t, server, podIP2, 3306, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if !profile.OpaqueProtocol {
			t.Fatal("Expected port 3306 to be an opaque protocol, but it was not")
		}
		if profile.GetEndpoint().GetProtocolHint() != nil {
			t.Fatalf("Expected protocol hint to be nil")
		}
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
		if profile.Endpoint.GetProtocolHint().GetOpaqueTransport().GetInboundPort() != 4143 {
			t.Fatalf("Expected pod to support opaque traffic on port 4143")
		}
		if profile.Endpoint.Addr.Ip.GetIpv4() == 0 && profile.Endpoint.Addr.Ip.GetIpv6() == nil {
			t.Fatal("IP is empty")
		}
		if profile.Endpoint.Addr.String() != epAddr.String() {
			t.Fatalf("Expected endpoint IP port to be %d, but it was %d", epAddr.Port, profile.Endpoint.Addr.Port)
		}
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
	})

	t.Run("Return opaque protocol profile with endpoint when using externalworkload IP and opaque protocol port", func(t *testing.T) {
		server := makeServer(t)

		stream := profileStream(t, server, externalWorkloadIP, opaquePort, "")
		defer stream.Cancel()

		epAddr, err := toAddress(externalWorkloadIP, opaquePort)
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
		if profile.Endpoint.GetProtocolHint().GetOpaqueTransport().GetInboundPort() != 4143 {
			t.Fatalf("Expected pod to support opaque traffic on port 4143")
		}
		if profile.Endpoint.Addr.Ip.GetIpv4() == 0 && profile.Endpoint.Addr.Ip.GetIpv6() == nil {
			t.Fatal("IP is empty")
		}
		if profile.Endpoint.Addr.String() != epAddr.String() {
			t.Fatalf("Expected endpoint IP port to be %d, but it was %d", epAddr.Port, profile.Endpoint.Addr.Port)
		}
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
		if profile.Endpoint.GetProtocolHint() == nil {
			t.Fatalf("Expected protocol hint but found none")
		}
		if profile.Endpoint.GetProtocolHint().GetOpaqueTransport().GetInboundPort() != 4143 {
			t.Fatalf("Expected pod to support opaque traffic on port 4143")
		}
	})

	t.Run("Return profile with opaque protocol when using externalworkload IP selected by a Server", func(t *testing.T) {
		server := makeServer(t)

		stream := profileStream(t, server, externalWorkloadIPPolicy, 80, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if !profile.OpaqueProtocol {
			t.Fatalf("Expected port %d to be an opaque protocol, but it was not", 80)
		}
		if profile.Endpoint.GetProtocolHint() == nil {
			t.Fatalf("Expected protocol hint but found none")
		}
		if profile.Endpoint.GetProtocolHint().GetOpaqueTransport().GetInboundPort() != 4143 {
			t.Fatalf("Expected pod to support opaque traffic on port 4143")
		}
	})

	t.Run("Return profile with opaque protocol when using an opaque port with an external IP", func(t *testing.T) {
		server := makeServer(t)

		stream := profileStream(t, server, externalIP, 3306, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if !profile.OpaqueProtocol {
			t.Fatalf("Expected port %d to be an opaque protocol, but it was not", 3306)
		}

	})

	t.Run("Return profile with non-opaque protocol when using an arbitrary port with an external IP", func(t *testing.T) {
		server := makeServer(t)

		stream := profileStream(t, server, externalIP, 80, "")
		defer stream.Cancel()
		profile := assertSingleProfile(t, stream.Updates())
		if profile.OpaqueProtocol {
			t.Fatalf("Expected port %d to be a non-opaque protocol, but it was opaque", 80)
		}
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

		ip, err := addr.ParseProxyIP(externalIP)
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
		profile = getLastProfileUpdate(t, stream, 2)
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
							{
								Name:  "LINKERD2_PROXY_ADMIN_LISTEN_ADDR",
								Value: "0.0.0.0:4191",
							},
							{
								Name:  "LINKERD2_PROXY_CONTROL_LISTEN_ADDR",
								Value: "0.0.0.0:4190",
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
				HostIP:  externalIP,
				HostIPs: []corev1.HostIP{{IP: externalIP}, {IP: externalIPv6}},
				PodIP:   "172.17.0.55",
				PodIPs:  []corev1.PodIP{{IP: "172.17.0.55"}},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create pod: %s", err)
		}

		profile = getLastProfileUpdate(t, stream, 3)
		dstPod = profile.Endpoint.MetricLabels["pod"]
		if dstPod != "hostport-mapping-2" {
			t.Fatalf("Expected dst_pod to be %s got %s", "hostport-mapping-2", dstPod)
		}
		if profile.OpaqueProtocol {
			t.Fatal("Expected OpaqueProtocol=false")
		}

		// Server is created, setting the port to opaque
		l5dClient.ServerV1beta3().Servers("ns").Create(context.Background(), &v1beta3.Server{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "srv-hostport-mapping-2",
				Namespace: "ns",
			},
			Spec: v1beta3.ServerSpec{
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

		profile = getLastProfileUpdate(t, stream, 4)
		if !profile.OpaqueProtocol {
			t.Fatal("Expected OpaqueProtocol=true")
		}
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
	})

	t.Run("when JSON is invalid and old token format used", func(t *testing.T) {
		server := makeServer(t)

		dest := &pb.GetDestination{ContextToken: "ns:ns-2"}
		token := server.parseContextToken(dest.ContextToken)
		if token.Ns != "ns-2" {
			t.Fatalf("Expected %s got %s", "ns-2", token.Ns)
		}
	})

	t.Run("when invalid JSON and invalid old format", func(t *testing.T) {
		server := makeServer(t)

		dest := &pb.GetDestination{ContextToken: "123fa-test"}
		token := server.parseContextToken(dest.ContextToken)
		if token.Ns != "" || token.NodeName != "" {
			t.Fatalf("Expected context token to be empty, got %v", token)
		}
	})
}

func updateAddAddress(t *testing.T, update *pb.Update) []string {
	t.Helper()
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

func updateRemoveAddress(t *testing.T, update *pb.Update) []string {
	t.Helper()
	add, ok := update.GetUpdate().(*pb.Update_Remove)
	if !ok {
		t.Fatalf("Update expected to be a remove, but was %+v", update)
	}
	ips := []string{}
	for _, ip := range add.Remove.Addrs {
		ips = append(ips, addr.ProxyAddressToString(ip))
	}
	return ips
}

func toAddress(path string, port uint32) (*net.TcpAddress, error) {
	ip, err := addr.ParseProxyIP(path)
	if err != nil {
		return nil, err
	}
	return &net.TcpAddress{
		Ip:   ip,
		Port: port,
	}, nil
}

type endpointOverflowScenario struct {
	server       *server
	servicePath  string
	metricLabels prometheus.Labels
	waitMessage  string
	prepare      func(*testing.T, *server)
	trigger      func(*testing.T, *server, int)
}

func runEndpointOverflowTest(t *testing.T, sc endpointOverflowScenario) {
	t.Helper()

	if sc.server == nil {
		t.Fatal("endpoint overflow scenario requires a server")
	}
	if sc.trigger == nil {
		t.Fatal("endpoint overflow scenario requires a trigger function")
	}
	if sc.waitMessage == "" {
		sc.waitMessage = fmt.Sprintf("endpoint translator overflow for %s", sc.servicePath)
	}

	if sc.prepare != nil {
		sc.prepare(t, sc.server)
	}

	stream := newBlockingGetStream()
	defer stream.Release()
	defer stream.Cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- sc.server.Get(&pb.GetDestination{
			Scheme: "k8s",
			Path:   sc.servicePath,
		}, stream)
	}()

	stream.WaitForSend(t, 5*time.Second)

	metric, err := updatesQueueOverflowCounter.GetMetricWith(sc.metricLabels)
	if err != nil {
		t.Fatalf("failed to get overflow counter: %v", err)
	}

	initialOverflow := counterValue(t, metric)
	sc.trigger(t, sc.server, 0)
	sc.trigger(t, sc.server, 1)

	// Wait for Get to finish without unblocking Send so we catch streams that
	// cannot terminate while a Send is blocked.
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) && status.Code(err) != codes.Canceled {
			t.Fatalf("expected Get to be end after overflow, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Get to return after overflow")
	}
	if counterValue(t, metric) == initialOverflow {
		t.Fatalf("%s", sc.waitMessage)
	}

	stream.Release()

	if got := stream.SendCount(); got == 0 {
		t.Fatal("expected at least one update before overflow")
	}

	updates := stream.Updates()
	if len(updates) == 0 || updates[0].GetAdd() == nil {
		t.Fatalf("expected initial update to be an Add, got %T", updates[0].GetUpdate())
	}
	if len(updates) > 1 && updates[1].GetAdd() == nil {
		t.Fatalf("expected buffered update to be an Add, got %T", updates[1].GetUpdate())
	}
}

type blockingGetStream struct {
	util.MockServerStream

	mu          sync.Mutex
	updates     []*pb.Update
	sendStarted chan struct{}
	startOnce   sync.Once
	release     chan struct{}
	releaseOnce sync.Once
}

func newBlockingGetStream() *blockingGetStream {
	return &blockingGetStream{
		MockServerStream: util.NewMockServerStream(),
		sendStarted:      make(chan struct{}),
		release:          make(chan struct{}),
	}
}

func (bgs *blockingGetStream) Send(update *pb.Update) error {
	bgs.mu.Lock()
	bgs.updates = append(bgs.updates, update)
	bgs.mu.Unlock()

	bgs.startOnce.Do(func() { close(bgs.sendStarted) })

	select {
	case <-bgs.release:
		return nil
	case <-bgs.Context().Done():
		return bgs.Context().Err()
	}
}

func (bgs *blockingGetStream) WaitForSend(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-bgs.sendStarted:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for first update")
	}
}

func (bgs *blockingGetStream) Release() {
	bgs.releaseOnce.Do(func() {
		close(bgs.release)
	})
}

func (bgs *blockingGetStream) SendCount() int {
	bgs.mu.Lock()
	defer bgs.mu.Unlock()
	return len(bgs.updates)
}

func (bgs *blockingGetStream) Updates() []*pb.Update {
	bgs.mu.Lock()
	defer bgs.mu.Unlock()
	cpy := make([]*pb.Update, len(bgs.updates))
	copy(cpy, bgs.updates)
	return cpy
}

func addEndpointToSlice(t *testing.T, server *server, podName, ip string) {
	t.Helper()

	createPod(t, server, podName, ip)

	sliceClient := server.k8sAPI.Client.DiscoveryV1().EndpointSlices("ns")
	ready := true

	slice, err := sliceClient.Get(context.Background(), "name1-ipv4", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get endpoint slice: %v", err)
	}
	slice.Endpoints = append(slice.Endpoints, discovery.Endpoint{
		Addresses: []string{ip},
		Conditions: discovery.EndpointConditions{
			Ready: &ready,
		},
		TargetRef: &corev1.ObjectReference{
			Kind:      "Pod",
			Name:      podName,
			Namespace: "ns",
		},
	})
	if _, err = sliceClient.Update(context.Background(), slice, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update endpoint slice: %v", err)
	}
}

func addEndpointToRemoteSlice(t *testing.T, api *k8s.API, namespace, sliceName, ip string) {
	t.Helper()

	podName := fmt.Sprintf("%s-%s", sliceName, strings.ReplaceAll(ip, ".", "-"))
	createPodWithAPI(t, api, namespace, podName, ip)

	sliceClient := api.Client.DiscoveryV1().EndpointSlices(namespace)
	ready := true

	slice, err := sliceClient.Get(context.Background(), sliceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get remote endpoint slice: %v", err)
	}
	slice.Endpoints = append(slice.Endpoints, discovery.Endpoint{
		Addresses: []string{ip},
		Conditions: discovery.EndpointConditions{
			Ready: &ready,
		},
		TargetRef: &corev1.ObjectReference{
			Kind:      "Pod",
			Name:      podName,
			Namespace: namespace,
		},
	})
	if _, err = sliceClient.Update(context.Background(), slice, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update remote endpoint slice: %v", err)
	}
}

func createPod(t *testing.T, server *server, podName, ip string) {
	t.Helper()

	createPodWithAPI(t, server.k8sAPI, "ns", podName, ip)
}

func createPodWithAPI(t *testing.T, api *k8s.API, namespace, podName, ip string) {
	t.Helper()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				pkgk8s.ControllerNSLabel: "linkerd",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: pkgk8s.ProxyContainerName,
					Env: []corev1.EnvVar{
						{Name: envInboundListenAddr, Value: "0.0.0.0:4143"},
						{Name: envAdminListenAddr, Value: "0.0.0.0:4191"},
						{Name: envControlListenAddr, Value: "0.0.0.0:4190"},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
			PodIP:  ip,
			PodIPs: []corev1.PodIP{{IP: ip}},
		},
	}

	if _, err := api.Client.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create pod %s: %v", podName, err)
	}

	_, _ = api.Client.CoreV1().Pods(namespace).UpdateStatus(context.Background(), pod, metav1.UpdateOptions{})
}

func TestIpWatcherGetSvcID(t *testing.T) {
	name := "service"
	namespace := "test"
	clusterIP := "10.245.0.1"
	k8sConfigs := `
apiVersion: v1
kind: Service
metadata:
  name: service
  namespace: test
spec:
  type: ClusterIP
  clusterIP: 10.245.0.1
  clusterIPs:
  - 10.245.0.1
  - 2001:db8::88
  ports:
  - port: 1234`

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

		svc6, err := getSvcID(k8sAPI, clusterIPv6, logging.WithFields(nil))
		if err != nil {
			t.Fatalf("Error getting service: %s", err)
		}
		if svc6 == nil {
			t.Fatalf("Expected to find service mapped to [%s]", clusterIPv6)
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

func testReturnEndpoints(t *testing.T, fqdn, ip string, port uint32) {
	t.Helper()

	server := makeServer(t)

	stream := &bufferingGetStream{
		updates:          make(chan *pb.Update, 50),
		MockServerStream: util.NewMockServerStream(),
	}
	defer stream.Cancel()

	testReturnEndpointsForServer(t, server, stream, fqdn, ip, port)
}

func testReturnEndpointsForServer(t *testing.T, server *server, stream *bufferingGetStream, fqdn, ip string, port uint32) {
	t.Helper()

	errs := make(chan error)
	// server.Get blocks until the grpc stream is complete so we call it
	// in a goroutine and watch stream.updates for updates.
	go func() {
		err := server.Get(&pb.GetDestination{Scheme: "k8s", Path: fmt.Sprintf("%s:%d", fqdn, port)}, stream)
		if err != nil {
			errs <- err
		}
	}()

	addr := fmt.Sprintf("%s:%d", ip, port)
	parsedIP, err := netip.ParseAddr(ip)
	if err != nil {
		t.Fatalf("Invalid IP [%s]: %s", ip, err)
	}
	if parsedIP.Is6() {
		addr = fmt.Sprintf("[%s]:%d", ip, port)
	}

	select {
	case update := <-stream.updates:
		if updateAddAddress(t, update)[0] != addr {
			t.Fatalf("Expected %s but got %s", addr, updateAddAddress(t, update)[0])
		}

		if len(stream.updates) != 0 {
			t.Fatalf("Expected 1 update but got %d: %v", 1+len(stream.updates), stream.updates)
		}
	case err := <-errs:
		t.Fatalf("Got error: %s", err)
	}
}

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	m := &dto.Metric{}
	if err := c.Write(m); err != nil {
		t.Fatalf("failed to read counter: %v", err)
	}
	if m.GetCounter() == nil {
		t.Fatal("counter metric missing value")
	}
	return m.GetCounter().GetValue()
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

func profileStream(t *testing.T, server *server, host string, port uint32, token string) *bufferingGetProfileStream {
	t.Helper()

	stream := &bufferingGetProfileStream{
		updates:          []*pb.DestinationProfile{},
		MockServerStream: util.NewMockServerStream(),
	}

	go func() {
		err := server.GetProfile(&pb.GetDestination{
			Scheme:       "k8s",
			Path:         gonet.JoinHostPort(host, fmt.Sprintf("%d", port)),
			ContextToken: token,
		}, stream)
		if err != nil &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) &&
			status.Code(err) != codes.Canceled &&
			status.Code(err) != codes.DeadlineExceeded {
			logging.Fatalf("Got error: %s", err)
		}
	}()
	// Give GetProfile some slack
	time.Sleep(50 * time.Millisecond)

	return stream
}

func getLastProfileUpdate(t *testing.T, stream *bufferingGetProfileStream, expectedUpdates int) *pb.DestinationProfile {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for {
		updates := stream.Updates()
		if len(updates) >= expectedUpdates {
			return updates[expectedUpdates-1]
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d updates, got %d", expectedUpdates, len(updates))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
