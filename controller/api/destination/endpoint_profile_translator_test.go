package destination

import (
	"testing"
	"time"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEndpointProfileTranslator(t *testing.T) {
	// logging.SetLevel(logging.TraceLevel)
	// defer logging.SetLevel(logging.PanicLevel)

	addr := &watcher.Address{
		IP:   "10.10.11.11",
		Port: 8080,
	}
	podAddr := &watcher.Address{
		IP:   "10.10.11.11",
		Port: 8080,
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					consts.ProxyOpaquePortsAnnotation: "8080",
				},
			},
		},
	}

	t.Run("Sends update", func(t *testing.T) {
		mockGetProfileServer := &mockDestinationGetProfileServer{
			profilesReceived: make(chan *pb.DestinationProfile, 1),
		}
		log := logging.WithField("test", t.Name())
		translator := newEndpointProfileTranslator(
			true, "cluster", "identity", make(map[uint32]struct{}),
			nil, nil,
			mockGetProfileServer,
			nil,
			log,
		)
		translator.Start()
		defer translator.Stop()

		if err := translator.Update(addr); err != nil {
			t.Fatal("Expected update")
		}
		select {
		case p := <-mockGetProfileServer.profilesReceived:
			log.Debugf("Received update: %v", p)
		case <-time.After(1 * time.Second):
			t.Fatal("No update received")
		}

		if err := translator.Update(addr); err != nil {
			t.Fatal("Unexpected update")
		}
		select {
		case p := <-mockGetProfileServer.profilesReceived:
			t.Fatalf("Duplicate update sent: %v", p)
		case <-time.After(1 * time.Second):
		}

		if err := translator.Update(podAddr); err != nil {
			t.Fatal("Expected update")
		}
		select {
		case p := <-mockGetProfileServer.profilesReceived:
			log.Debugf("Received update: %v", p)
		case <-time.After(1 * time.Second):
		}
	})

	t.Run("Handles overflow", func(t *testing.T) {
		mockGetProfileServer := &mockDestinationGetProfileServer{
			profilesReceived: make(chan *pb.DestinationProfile, 1),
		}
		log := logging.WithField("test", t.Name())
		endStream := make(chan struct{})
		translator := newEndpointProfileTranslator(
			true, "cluster", "identity", make(map[uint32]struct{}),
			nil, nil,
			mockGetProfileServer,
			endStream,
			log,
		)
		translator.Start()
		defer translator.Stop()

		for i := 0; i < updateQueueCapacity/2; i++ {
			if err := translator.Update(podAddr); err != nil {
				t.Fatal("Expected update")
			}
			select {
			case <-endStream:
				t.Fatal("Stream ended prematurely")
			default:
			}

			if err := translator.Update(addr); err != nil {
				t.Fatal("Expected update")
			}
			select {
			case <-endStream:
				t.Fatal("Stream ended prematurely")
			default:
			}
		}

		if err := translator.Update(podAddr); err == nil {
			t.Fatal("Expected update to fail")
		}
		select {
		case <-endStream:
		default:
			t.Fatal("Stream should have ended")
		}

		// XXX We should assert that endpointProfileUpdatesQueueOverflowCounter
		// == 1 but we can't read counter values.
	})
}
