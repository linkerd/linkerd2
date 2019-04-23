package destination

import (
	"testing"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
)

type mockDestinationGetServer struct {
	mockServerStream
	updatesReceived []*pb.Update
}

type mockDestinationGetProfileServer struct {
	mockServerStream
	profilesReceived []*pb.DestinationProfile
}

func (m *mockDestinationGetServer) Send(update *pb.Update) error {
	m.updatesReceived = append(m.updatesReceived, update)
	return nil
}

func (m *mockDestinationGetProfileServer) Send(profile *pb.DestinationProfile) error {
	m.profilesReceived = append(m.profilesReceived, profile)
	return nil
}

func makeServer(t *testing.T) *server {
	k8sAPI, err := k8s.NewFakeAPI()
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}
	log := logging.WithField("test", t.Name)

	endpoints := watcher.NewEndpointsWatcher(k8sAPI, log)
	profiles := watcher.NewProfileWatcher(k8sAPI, log)

	return &server{
		endpoints,
		profiles,
		false,
		"linkerd",
		"trust.domain",
		log,
		make(<-chan struct{}),
	}
}

type bufferingGetStream struct {
	updates []*pb.Update
	mockServerStream
}

func (bgs bufferingGetStream) Send(update *pb.Update) error {
	bgs.updates = append(bgs.updates, update)
	return nil
}

func TestStreamResolutionUsingCorrectResolverFor(t *testing.T) {
	t.Run("Returns error if not valid service name", func(t *testing.T) {
		server := makeServer(t)

		stream := bufferingGetStream{
			updates:          []*pb.Update{},
			mockServerStream: mockServerStream{},
		}

		err := server.Get(&pb.GetDestination{Scheme: "k8s", Path: "linkerd.io"}, stream)
		if err == nil {
			t.Fatalf("Expecting error, got nothing")
		}
	})
}
