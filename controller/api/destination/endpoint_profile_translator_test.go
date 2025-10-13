package destination

import (
	"errors"
	"net/http"
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
			profilesReceived: make(chan *pb.DestinationProfile), // UNBUFFERED
		}
		log := logging.WithField("test", t.Name())
		translator := newEndpointProfileTranslator(
			true, true, "cluster", "identity", make(map[uint32]struct{}), nil,
			mockGetProfileServer,
			nil,
			log,
			DefaultStreamQueueCapacity,
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
		queueCapacity := DefaultStreamQueueCapacity
		translator := newEndpointProfileTranslator(
			true, true, "cluster", "identity", make(map[uint32]struct{}), nil,
			mockGetProfileServer,
			endStream,
			log,
			queueCapacity,
		)

		// We avoid starting the translator so that it doesn't drain its update
		// queue and we can test the overflow behavior.

		for i := 0; i < queueCapacity/2; i++ {
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

		// The queue should be full and the next update should fail.
		t.Logf("Queue length=%d capacity=%d", translator.queueLen(), queueCapacity)
		if err := translator.Update(podAddr); err == nil {
			if !errors.Is(err, http.ErrServerClosed) {
				t.Fatalf("Expected update to fail; queue=%d; capacity=%d", translator.queueLen(), queueCapacity)
			}
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
