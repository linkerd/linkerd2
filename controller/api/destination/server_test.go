package destination

import (
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	logging "github.com/sirupsen/logrus"
)

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
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
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
  name: name1-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
  status:
    phase: Running
    podIP: 172.17.0.12`,
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
	log := logging.WithField("test", t.Name)

	k8sAPI.Sync()

	endpoints := watcher.NewEndpointsWatcher(k8sAPI, log)
	profiles := watcher.NewProfileWatcher(k8sAPI, log)
	trafficSplits := watcher.NewTrafficSplitWatcher(k8sAPI, log)
	ips := watcher.NewIPWatcher(k8sAPI, endpoints, log)

	return &server{
		endpoints,
		profiles,
		trafficSplits,
		ips,
		false,
		"linkerd",
		"trust.domain",
		"mycluster.local",
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
		// need to call server.Get in a separate goroutine.  By pre-emptively
		// cancelling, the behavior of Get becomes effectively synchronous and
		// we will get only the initial update, which is what we want for this
		// test.
		stream.Cancel()

		err := server.Get(&pb.GetDestination{Scheme: "k8s", Path: "name1.ns.svc.mycluster.local:8989"}, stream)
		if err != nil {
			t.Fatalf("Got error: %s", err)
		}

		if len(stream.updates) != 1 {
			t.Fatalf("Expected 1 update but got %d: %v", len(stream.updates), stream.updates)
		}

		if updateAddAddress(t, stream.updates[0])[0] != "172.17.0.12:8989" {
			t.Fatalf("Expected 172.17.0.12:8989 but got %s", updateAddAddress(t, stream.updates[0])[0])
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
			Path:         "name1.ns.svc.mycluster.local:8989",
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
		if len(stream.updates) != 1 && len(stream.updates) != 2 {
			t.Fatalf("Expected 1 or 2 updates but got %d: %v", len(stream.updates), stream.updates)
		}
		lastUpdate := stream.updates[len(stream.updates)-1]
		routes := lastUpdate.GetRoutes()
		if len(routes) != 1 {
			t.Fatalf("Expected 1 route but got %d: %v", len(routes), routes)
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

		// See note above on pre-emptive cancellation.
		stream.Cancel()
		err := server.GetProfile(&pb.GetDestination{
			Scheme:       "k8s",
			Path:         "name1.ns.svc.mycluster.local:8989",
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
