package destination

import (
	"fmt"
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
const clusterIP = "172.17.12.0"
const podIP1 = "172.17.0.12"
const podIP2 = "172.17.0.13"
const port uint32 = 8989
const opaquePort uint32 = 4242

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
	k8sAPI, err := k8s.NewFakeAPI(`
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
  annotations:
    config.linkerd.io/opaque-ports: "4242"
  name: name1-1
  namespace: ns
status:
  phase: Running
  podIP: 172.17.0.12`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: name2-1
  namespace: ns
status:
  phase: Running
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
	)
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}
	log := logging.WithField("test", t.Name())

	endpoints := watcher.NewEndpointsWatcher(k8sAPI, log, false)
	profiles := watcher.NewProfileWatcher(k8sAPI, log)
	trafficSplits := watcher.NewTrafficSplitWatcher(k8sAPI, log)
	ips := watcher.NewIPWatcher(k8sAPI, endpoints, log)

	// Sync after creating watchers so that the the indexers added get updated
	// properly
	k8sAPI.Sync(nil)

	return &server{
		endpoints,
		profiles,
		trafficSplits,
		ips,
		true,
		"linkerd",
		"trust.domain",
		"mycluster.local",
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

		first := stream.updates[0]
		if first.FullyQualifiedName != fullyQualifiedName {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'", fullyQualifiedName, first.FullyQualifiedName)
		}
		if first.OpaqueProtocol {
			t.Fatalf("Expected port %d to not be an opaque protocol, but it was", port)
		}
		routes := first.GetRoutes()
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

	t.Run("Return opaque protocol profile when using cluster IP and opaque protocol port", func(t *testing.T) {
		server := makeServer(t)
		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}
		stream.Cancel()
		err := server.GetProfile(&pb.GetDestination{
			Scheme: "k8s",
			Path:   fmt.Sprintf("%s:%d", clusterIP, opaquePort),
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
		if first.FullyQualifiedName != fullyQualifiedName {
			t.Fatalf("Expected fully qualified name '%s', but got '%s'", fullyQualifiedName, first.FullyQualifiedName)
		}
		if !first.OpaqueProtocol {
			t.Fatalf("Expected port %d to be an opaque protocol, but it was not", opaquePort)
		}
		routes := first.GetRoutes()
		if len(routes) != 1 {
			t.Fatalf("Expected 1 route but got %d: %v", len(routes), routes)
		}
	})

	t.Run("Return opaque protocol profile with endpoint when using pod IP and opaque protocol port", func(t *testing.T) {
		server := makeServer(t)
		stream := &bufferingGetProfileStream{
			updates:          []*pb.DestinationProfile{},
			MockServerStream: util.NewMockServerStream(),
		}
		stream.Cancel()

		epAddr, err := toAddress(podIP1, opaquePort)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		err = server.GetProfile(&pb.GetDestination{
			Scheme: "k8s",
			Path:   fmt.Sprintf("%s:%d", podIP1, opaquePort),
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
		if !first.OpaqueProtocol {
			t.Fatalf("Expected protocol to be opaque but it was not")
		}
		if first.Endpoint.Addr.String() != epAddr.String() {
			// t.Fatalf("Expected endpoint IP to be %s, but it was %s", epAddr.Ip, first.Endpoint.Addr.Ip)
			t.Fatalf("Expected endpoint IP port to be %d, but it was %d", epAddr.Port, first.Endpoint.Addr.Port)
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
