package destination

import (
	"fmt"
	"strings"
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	logging "github.com/sirupsen/logrus"
)

const fullyQualifiedName = "name1.ns.svc.mycluster.local"
const fullyQualifiedNameOpaque = "name3.ns.svc.mycluster.local"
const fullyQualifiedNameOpaqueService = "name4.ns.svc.mycluster.local"
const fullyQualifiedNameSkipped = "name5.ns.svc.mycluster.local"
const clusterIP = "172.17.12.0"
const clusterIPOpaque = "172.17.12.1"
const podIP1 = "172.17.0.12"
const podIP2 = "172.17.0.13"
const podIPOpaque = "172.17.0.14"
const podIPSkipped = "172.17.0.15"
const port uint32 = 8989
const opaquePort uint32 = 4242
const skippedPort uint32 = 24224

type mockDestinationGetServer struct {
	util.MockServerStream
	updatesReceived []*pb.Update
}

func (m *mockDestinationGetServer) Send(update *pb.Update) error {
	m.updatesReceived = append(m.updatesReceived, update)
	return nil
}

type mockDestinationGetProfileServer struct {
	util.MockServerStream
	profilesReceived []*pb.DestinationProfile
}

func (m *mockDestinationGetProfileServer) Send(profile *pb.DestinationProfile) error {
	m.profilesReceived = append(m.profilesReceived, profile)
	return nil
}

func makeServer(t *testing.T) *server {
	meshedPodResources := []string{`
apiVersion: v1
kind: Namespace
metadata:
  name: ns`,
		`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 172.17.12.0
  ports:
  - port: 8989`,
		`
apiVersion: v1
kind: Endpoints
metadata:
  name: name1
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.0.12
    targetRef:
      kind: Pod
      name: name1-1
      namespace: ns
  ports:
  - port: 8989`,
		`
apiVersion: v1
kind: Pod
metadata:
  labels:
    linkerd.io/control-plane-ns: linkerd
  name: name1-1
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.12
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      name: linkerd-proxy`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: name2-2
  namespace: ns
status:
  phase: Succeeded
  podIP: 172.17.0.13`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: name2-3
  namespace: ns
status:
  phase: Failed
  podIP: 172.17.0.13`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: name2-4
  namespace: ns
  deletionTimestamp: 2021-01-01T00:00:00Z
status:
  podIP: 172.17.0.13`,
		`
apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name1.ns.svc.mycluster.local
  namespace: ns
spec:
  routes:
  - name: route1
    isRetryable: false
    condition:
      pathRegex: "/a/b/c"`,
	}

	clientSP := []string{
		`
apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: name1.ns.svc.mycluster.local
  namespace: client-ns
spec:
  routes:
  - name: route2
    isRetryable: true
    condition:
      pathRegex: "/x/y/z"`,
	}

	unmeshedPod := `
apiVersion: v1
kind: Pod
metadata:
  name: name2
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.13`

	meshedOpaquePodResources := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: name3
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 172.17.12.1
  ports:
  - port: 4242`,
		`
apiVersion: v1
kind: Endpoints
metadata:
  name: name3
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.0.14
    targetRef:
      kind: Pod
      name: name3
      namespace: ns
  ports:
  - port: 4242`,
		`
apiVersion: v1
kind: Pod
metadata:
  labels:
    linkerd.io/control-plane-ns: linkerd
  annotations:
    config.linkerd.io/opaque-ports: "4242"
  name: name3
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.14
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      name: linkerd-proxy`,
	}

	meshedOpaqueServiceResources := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: name4
  namespace: ns
  annotations:
    config.linkerd.io/opaque-ports: "4242"`,
	}

	meshedSkippedPodResource := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: name5
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 172.17.13.1
  ports:
  - port: 24224`,
		`
apiVersion: v1
kind: Endpoints
metadata:
  name: name5
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.0.15
    targetRef:
      kind: Pod
      name: name5
      namespace: ns
  ports:
  - port: 24224`,
		`
apiVersion: v1
kind: Pod
metadata:
  labels:
    linkerd.io/control-plane-ns: linkerd
  annotations:
    config.linkerd.io/skip-inbound-ports: "24224"
  name: name5
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.15
spec:
  containers:
    - env:
      - name: LINKERD2_PROXY_INBOUND_LISTEN_ADDR
        value: 0.0.0.0:4143
      name: linkerd-proxy`,
	}
	res := append(meshedPodResources, clientSP...)
	res = append(res, unmeshedPod)
	res = append(res, meshedOpaquePodResources...)
	res = append(res, meshedOpaqueServiceResources...)
	res = append(res, meshedSkippedPodResource...)
	k8sAPI, err := k8s.NewFakeAPI(res...)
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}
	log := logging.WithField("test", t.Name())
	defaultOpaquePorts := map[uint32]struct{}{
		25:    {},
		443:   {},
		587:   {},
		3306:  {},
		5432:  {},
		11211: {},
	}

	err = watcher.InitializeIndexers(k8sAPI)
	if err != nil {
		t.Fatalf("initializeIndexers returned an error: %s", err)
	}

	endpoints := watcher.NewEndpointsWatcher(k8sAPI, log, false)
	opaquePorts := watcher.NewOpaquePortsWatcher(k8sAPI, log, defaultOpaquePorts)
	profiles := watcher.NewProfileWatcher(k8sAPI, log)
	trafficSplits := watcher.NewTrafficSplitWatcher(k8sAPI, log)

	// Sync after creating watchers so that the the indexers added get updated
	// properly
	k8sAPI.Sync(nil)

	return &server{
		endpoints,
		opaquePorts,
		profiles,
		trafficSplits,
		k8sAPI.Node(),
		true,
		"linkerd",
		"trust.domain",
		"mycluster.local",
		defaultOpaquePorts,
		k8sAPI,
		log,
		make(<-chan struct{}),
	}
}

type bufferingGetStream struct {
	updates []*pb.Update
	util.MockServerStream
}

func (bgs *bufferingGetStream) Send(update *pb.Update) error {
	bgs.updates = append(bgs.updates, update)
	return nil
}

type bufferingGetProfileStream struct {
	updates []*pb.DestinationProfile
	util.MockServerStream
}

func (bgps *bufferingGetProfileStream) Send(profile *pb.DestinationProfile) error {
	bgps.updates = append(bgps.updates, profile)
	return nil
}

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

		if len(stream.updates) == 0 || len(stream.updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(stream.updates), stream.updates)
		}

		last := stream.updates[len(stream.updates)-1]

		addrs := last.GetAdd().Addrs
		if len(addrs) == 0 {
			t.Fatalf("Expected len(addrs) to be > 0")
		}

		if addrs[0].ProtocolHint != nil {
			t.Fatalf("Expected protocol hint for %s to be nil but got %+v", path, addrs[0].ProtocolHint)
		}

		if addrs[0].TlsIdentity != nil {
			t.Fatalf("Expected protocol hint for %s to be nil but got %+v", path, addrs[0].TlsIdentity)
		}
	})
}

func TestGetProfiles(t *testing.T) {
	t.Run("Returns error if not valid service name", func(t *testing.T) {
		server := makeServer(t)

		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}

		err := server.GetProfile(&pb.GetDestination{Scheme: "k8s", Path: "linkerd.io"}, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})

	t.Run("Returns server profile", func(t *testing.T) {
		server := makeServer(t)

		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}

		stream.Cancel() // See note above on pre-emptive cancellation.
		err := server.GetProfile(&pb.GetDestination{
			Scheme:       "k8s",
			Path:         fmt.Sprintf("%s:%d", fullyQualifiedName, port),
			ContextToken: "ns:other",
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// The number of updates we get depends on the order that the watcher
		// gets updates about the server profile and the client profile.  The
		// client profile takes priority so if we get that update first, it
		// will only trigger one update to the stream.  However, if the watcher
		// gets the server profile first, it will send an update with that
		// profile to the stream and then a second update when it gets the
		// client profile.
		// Additionally, under normal conditions the creation of resources by
		// the fake API will generate notifications that are discarded after the
		// stream.Cancel() call, but very rarely those notifications might come
		// after, in which case we'll get a third update.
		if len(stream.updates) == 0 || len(stream.updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(stream.updates), stream.updates)
		}

		firstUpdate := stream.updates[0]
		if firstUpdate.FullyQualifiedName != fullyQualifiedName {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'", fullyQualifiedName, firstUpdate.FullyQualifiedName)
		}

		lastUpdate := stream.updates[len(stream.updates)-1]
		if lastUpdate.OpaqueProtocol {
			t.Fatalf("Expected port %d to not be an opaque protocol, but it was", port)
		}
		routes := lastUpdate.GetRoutes()
		if len(routes) != 1 {
			t.Fatalf("Expected 1 route but got %d: %v", len(routes), routes)
		}
		if routes[0].GetIsRetryable() {
			t.Fatalf("Expected route to not be retryable, but it was")
		}
	})

	t.Run("Return service profile when using json token", func(t *testing.T) {
		server := makeServer(t)

		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}

		stream.Cancel() // see note above on pre-emptive cancelling
		err := server.GetProfile(&pb.GetDestination{
			Scheme:       "k8s",
			Path:         fmt.Sprintf("%s:%d", fullyQualifiedName, port),
			ContextToken: "{\"ns\":\"other\"}",
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// The number of updates we get depends on the order that the watcher
		// gets updates about the server profile and the client profile.  The
		// client profile takes priority so if we get that update first, it
		// will only trigger one update to the stream.  However, if the watcher
		// gets the server profile first, it will send an update with that
		// profile to the stream and then a second update when it gets the
		// client profile.
		// Additionally, under normal conditions the creation of resources by
		// the fake API will generate notifications that are discarded after the
		// stream.Cancel() call, but very rarely those notifications might come
		// after, in which case we'll get a third update.
		if len(stream.updates) == 0 || len(stream.updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got: %d: %v", len(stream.updates), stream.updates)
		}

		firstUpdate := stream.updates[0]
		if firstUpdate.FullyQualifiedName != fullyQualifiedName {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'", fullyQualifiedName, firstUpdate.FullyQualifiedName)
		}

		lastUpdate := stream.updates[len(stream.updates)-1]
		routes := lastUpdate.GetRoutes()
		if len(routes) != 1 {
			t.Fatalf("Expected 1 route got %d: %v", len(routes), routes)
		}
		if routes[0].GetIsRetryable() {
			t.Fatalf("Expected route to not be retryable, but it was")
		}
	})

	t.Run("Returns client profile", func(t *testing.T) {
		server := makeServer(t)

		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}

		// See note about pre-emptive cancellation
		stream.Cancel()
		err := server.GetProfile(&pb.GetDestination{
			Scheme:       "k8s",
			Path:         fmt.Sprintf("%s:%d", fullyQualifiedName, port),
			ContextToken: "{\"ns\":\"client-ns\"}",
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// The number of updates we get depends on if the watcher gets an update
		// about the profile before or after the subscription.  If the subscription
		// happens first, then we get a profile update during the subscription and
		// then a second update when the watcher receives the update about that
		// profile.  If the watcher event happens first, then we only get the
		// update during subscription.
		if len(stream.updates) != 1 && len(stream.updates) != 2 {
			t.Fatalf("Expected 1 or 2 updates but got %d: %v", len(stream.updates), stream.updates)
		}
		routes := stream.updates[len(stream.updates)-1].GetRoutes()
		if len(routes) != 1 {
			t.Fatalf("Expected 1 route but got %d: %v", len(routes), routes)
		}
		if !routes[0].GetIsRetryable() {
			t.Fatalf("Expected route to be retryable, but it was not")
		}
	})
	t.Run("Returns client profile", func(t *testing.T) {
		server := makeServer(t)

		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}

		// See note above on pre-emptive cancellation.
		stream.Cancel()
		err := server.GetProfile(&pb.GetDestination{
			Scheme:       "k8s",
			Path:         fmt.Sprintf("%s:%d", fullyQualifiedName, port),
			ContextToken: "ns:client-ns",
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// The number of updates we get depends on if the watcher gets an update
		// about the profile before or after the subscription.  If the subscription
		// happens first, then we get a profile update during the subscription and
		// then a second update when the watcher receives the update about that
		// profile.  If the watcher event happens first, then we only get the
		// update during subscription.
		if len(stream.updates) != 1 && len(stream.updates) != 2 {
			t.Fatalf("Expected 1 or 2 updates but got %d: %v", len(stream.updates), stream.updates)
		}
		routes := stream.updates[len(stream.updates)-1].GetRoutes()
		if len(routes) != 1 {
			t.Fatalf("Expected 1 route but got %d: %v", len(routes), routes)
		}
		if !routes[0].GetIsRetryable() {
			t.Fatalf("Expected route to be retryable, but it was not")
		}
	})

	t.Run("Return profile when using cluster IP", func(t *testing.T) {
		server := makeServer(t)
		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}
		stream.Cancel()
		err := server.GetProfile(&pb.GetDestination{
			Scheme: "k8s",
			Path:   fmt.Sprintf("%s:%d", clusterIP, port),
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// An explanation for why we expect 1 to 3 updates is in test cases
		// above
		if len(stream.updates) == 0 || len(stream.updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(stream.updates), stream.updates)
		}

		last := stream.updates[len(stream.updates)-1]
		if last.FullyQualifiedName != fullyQualifiedName {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'", fullyQualifiedName, last.FullyQualifiedName)
		}
		if last.OpaqueProtocol {
			t.Fatalf("Expected port %d to not be an opaque protocol, but it was", port)
		}
		routes := last.GetRoutes()
		if len(routes) != 1 {
			t.Fatalf("Expected 1 route but got %d: %v", len(routes), routes)
		}
	})

	t.Run("Return profile with endpoint when using pod IP", func(t *testing.T) {
		server := makeServer(t)
		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}
		stream.Cancel()

		epAddr, err := toAddress(podIP1, port)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		err = server.GetProfile(&pb.GetDestination{
			Scheme: "k8s",
			Path:   fmt.Sprintf("%s:%d", podIP1, port),
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// An explanation for why we expect 1 to 3 updates is in test cases
		// above
		if len(stream.updates) == 0 || len(stream.updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(stream.updates), stream.updates)
		}

		first := stream.updates[0]
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
		if first.Endpoint.ProtocolHint == nil {
			t.Fatalf("Expected protocol hint but found none")
		}
		if first.Endpoint.ProtocolHint.GetOpaqueTransport() != nil {
			t.Fatalf("Expected pod to not support opaque traffic on port %d", port)
		}
		if first.Endpoint.Addr.String() != epAddr.String() {
			t.Fatalf("Expected endpoint IP to be %s, but it was %s", epAddr.Ip, first.Endpoint.Addr.Ip)
		}
	})

	t.Run("Return default profile when IP does not map to service or pod", func(t *testing.T) {
		server := makeServer(t)
		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}
		stream.Cancel()
		err := server.GetProfile(&pb.GetDestination{
			Scheme: "k8s",
			Path:   "172.0.0.0:1234",
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// An explanation for why we expect 1 to 3 updates is in test cases
		// above
		if len(stream.updates) == 0 || len(stream.updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(stream.updates), stream.updates)
		}

		first := stream.updates[0]
		if first.RetryBudget == nil {
			t.Fatalf("Expected default profile to have a retry budget")
		}
	})

	t.Run("Return profile with no protocol hint when pod does not have label", func(t *testing.T) {
		server := makeServer(t)
		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}
		stream.Cancel()
		err := server.GetProfile(&pb.GetDestination{
			Scheme: "k8s",
			Path:   podIP2,
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// An explanation for why we expect 1 to 3 updates is in test cases
		// above
		if len(stream.updates) == 0 || len(stream.updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(stream.updates), stream.updates)
		}

		first := stream.updates[0]
		if first.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if first.Endpoint.ProtocolHint != nil {
			t.Fatalf("Expected no protocol hint but found one")
		}
	})

	t.Run("Return non-opaque protocol profile when using cluster IP and opaque protocol port", func(t *testing.T) {
		server := makeServer(t)
		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}
		stream.Cancel()
		err := server.GetProfile(&pb.GetDestination{
			Scheme: "k8s",
			Path:   fmt.Sprintf("%s:%d", clusterIPOpaque, opaquePort),
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// An explanation for why we expect 1 to 3 updates is in test cases
		// above
		if len(stream.updates) == 0 || len(stream.updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(stream.updates), stream.updates)
		}

		last := stream.updates[len(stream.updates)-1]
		if last.FullyQualifiedName != fullyQualifiedNameOpaque {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'", fullyQualifiedNameOpaque, last.FullyQualifiedName)
		}
		if last.OpaqueProtocol {
			t.Fatalf("Expected port %d to not be an opaque protocol, but it was", opaquePort)
		}
	})

	t.Run("Return opaque protocol profile with endpoint when using pod IP and opaque protocol port", func(t *testing.T) {
		server := makeServer(t)
		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}
		stream.Cancel()

		epAddr, err := toAddress(podIPOpaque, opaquePort)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		err = server.GetProfile(&pb.GetDestination{
			Scheme: "k8s",
			Path:   fmt.Sprintf("%s:%d", podIPOpaque, opaquePort),
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		// An explanation for why we expect 1 to 3 updates is in test cases
		// above
		if len(stream.updates) == 0 || len(stream.updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(stream.updates), stream.updates)
		}

		first := stream.updates[0]
		if first.Endpoint == nil {
			t.Fatalf("Expected response to have endpoint field")
		}
		if !first.OpaqueProtocol {
			t.Fatalf("Expected port %d to be an opaque protocol, but it was not", opaquePort)
		}
		_, exists := first.Endpoint.MetricLabels["namespace"]
		if !exists {
			t.Fatalf("Expected 'namespace' metric label to exist but it did not")
		}
		if first.Endpoint.ProtocolHint == nil {
			t.Fatalf("Expected protocol hint but found none")
		}
		if first.Endpoint.ProtocolHint.GetOpaqueTransport().InboundPort != 4143 {
			t.Fatalf("Expected pod to support opaque traffic on port 4143")
		}
		if first.Endpoint.Addr.String() != epAddr.String() {
			t.Fatalf("Expected endpoint IP port to be %d, but it was %d", epAddr.Port, first.Endpoint.Addr.Port)
		}
	})

	t.Run("Return opaque protocol profile when using service name with opaque port annotation", func(t *testing.T) {
		server := makeServer(t)
		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}
		stream.Cancel()
		err := server.GetProfile(&pb.GetDestination{
			Scheme: "k8s",
			Path:   fmt.Sprintf("%s:%d", fullyQualifiedNameOpaqueService, opaquePort),
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}
		if len(stream.updates) == 0 || len(stream.updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(stream.updates), stream.updates)
		}
		last := stream.updates[len(stream.updates)-1]
		if last.FullyQualifiedName != fullyQualifiedNameOpaqueService {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'", fullyQualifiedNameOpaqueService, last.FullyQualifiedName)
		}
		if !last.OpaqueProtocol {
			t.Fatalf("Expected port %d to be an opaque protocol, but it was not", opaquePort)
		}
	})

	t.Run("Return profile with unknown protocol hint and identity when pod contains skipped inbound port", func(t *testing.T) {
		server := makeServer(t)
		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}

		stream.Cancel()

		path := fmt.Sprintf("%s:%d", podIPSkipped, skippedPort)
		err := server.GetProfile(&pb.GetDestination{
			Scheme: "k8s",
			Path:   path,
		}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		if len(stream.updates) == 0 || len(stream.updates) > 3 {
			t.Fatalf("Expected 1 to 3 updates but got %d: %v", len(stream.updates), stream.updates)
		}

		last := stream.updates[len(stream.updates)-1]

		addr := last.GetEndpoint()
		if addr == nil {
			t.Fatalf("Expected to not be nil")
		}

		if addr.ProtocolHint != nil {
			t.Fatalf("Expected protocol hint for %s to be nil but got %+v", path, addr.ProtocolHint)
		}

		if addr.TlsIdentity != nil {
			t.Fatalf("Expected protocol hint for %s to be nil but got %+v", path, addr.TlsIdentity)
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

func TestIpWatcherGetPod(t *testing.T) {
	podIP := "10.255.0.1"
	hostIP := "172.0.0.1"
	var hostPort1 uint32 = 12345
	var hostPort2 uint32 = 12346
	expectedPodName := "hostPortPod1"
	k8sConfigs := []string{`
apiVersion: v1
kind: Pod
metadata:
  name: hostPortPod1
  namespace: ns
spec:
  containers:
  - image: test
    name: hostPortContainer1
    ports:
    - containerPort: 12345
      hostIP: 172.0.0.1
      hostPort: 12345
  - image: test
    name: hostPortContainer2
    ports:
    - containerPort: 12346
      hostIP: 172.0.0.1
      hostPort: 12346
status:
  phase: Running
  podIP: 10.255.0.1
  hostIP: 172.0.0.1`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: pod
  namespace: ns
status:
  phase: Running
  podIP: 10.255.0.1`,
	}
	t.Run("get pod by host IP and host port", func(t *testing.T) {
		k8sAPI, err := k8s.NewFakeAPI(k8sConfigs...)
		if err != nil {
			t.Fatalf("failed to create new fake API: %s", err)
		}

		err = watcher.InitializeIndexers(k8sAPI)
		if err != nil {
			t.Fatalf("initializeIndexers returned an error: %s", err)
		}

		k8sAPI.Sync(nil)
		// Get host IP pod that is mapped to the port `hostPort1`
		pod, err := getPod(k8sAPI, hostIP, hostPort1, logging.WithFields(nil))
		if err != nil {
			t.Fatalf("failed to get pod: %s", err)
		}
		if pod == nil {
			t.Fatalf("failed to find pod mapped to %s:%d", hostIP, hostPort1)
		}
		if pod.Name != expectedPodName {
			t.Fatalf("expected pod name to be %s, but got %s", expectedPodName, pod.Name)
		}
		// Get host IP pod that is mapped to the port `hostPort2`; this tests
		// that the indexer properly adds multiple containers from a single
		// pod.
		pod, err = getPod(k8sAPI, hostIP, hostPort2, logging.WithFields(nil))
		if err != nil {
			t.Fatalf("failed to get pod: %s", err)
		}
		if pod == nil {
			t.Fatalf("failed to find pod mapped to %s:%d", hostIP, hostPort2)
		}
		if pod.Name != expectedPodName {
			t.Fatalf("expected pod name to be %s, but got %s", expectedPodName, pod.Name)
		}
		// Get host IP pod with unmapped host port
		pod, err = getPod(k8sAPI, hostIP, 12347, logging.WithFields(nil))
		if err != nil {
			t.Fatalf("expected no error when getting host IP pod with unmapped host port, but got: %s", err)
		}
		if pod != nil {
			t.Fatal("expected no pod to be found with unmapped host port")
		}
		// Get pod IP pod and expect an error
		_, err = getPod(k8sAPI, podIP, 12346, logging.WithFields(nil))
		if err == nil {
			t.Fatal("expected error when getting by pod IP and unmapped host port, but got none")
		}
		if !strings.Contains(err.Error(), "pods with a conflicting pod network IP") {
			t.Fatalf("expected error to be pod IP address conflict, but got: %s", err)
		}
	})
}
