package destination

import (
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEndpointProfileTranslator(t *testing.T) {
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
		mockGetProfileServer := newMockDestinationGetProfileServer(1)
		log := logging.WithField("test", t.Name())

		cancelCalled := false
		cancel := func() {
			mockGetProfileServer.Cancel()
			cancelCalled = true
		}

		translator := newEndpointProfileTranslator(
			true, // forceOpaqueTransport
			true, // enableH2Upgrade
			"cluster",
			"identity",
			make(map[uint32]struct{}),
			nil, // meshedHTTP2ClientParams
			mockGetProfileServer.profilesReceived,
			cancel,
			log,
		)
		defer translator.Close()

		if err := translator.Update(addr); err != nil {
			t.Fatalf("Expected update, got error: %v", err)
		}
		select {
		case <-mockGetProfileServer.profilesReceived:
		case <-time.After(time.Second):
			t.Fatal("No update received for initial address")
		}

		if err := translator.Update(addr); err != nil {
			t.Fatalf("Unexpected error on duplicate update: %v", err)
		}
		select {
		case upd := <-mockGetProfileServer.profilesReceived:
			t.Fatalf("Duplicate update sent: %+v", upd)
		default:
		}

		if err := translator.Update(podAddr); err != nil {
			t.Fatalf("Expected pod update, got error: %v", err)
		}
		select {
		case <-mockGetProfileServer.profilesReceived:
		case <-time.After(time.Second):
			t.Fatal("No update received for pod address")
		}

		if cancelCalled {
			t.Fatal("Unexpected cancel invocation")
		}
	})

	t.Run("Handles overflow", func(t *testing.T) {
		mockGetProfileServer := newMockDestinationGetProfileServer(0) // unbuffered
		log := logging.WithField("test", t.Name())

		cancelCalled := false
		cancel := func() {
			mockGetProfileServer.Cancel()
			cancelCalled = true
		}
		translator := newEndpointProfileTranslator(
			true,
			true,
			"cluster",
			"identity",
			make(map[uint32]struct{}),
			nil,
			mockGetProfileServer.profilesReceived,
			cancel,
			log,
		)

		if err := translator.Update(podAddr); err == nil {
			t.Fatal("Expected update to fail when downstream is not draining")
		}
		if !cancelCalled {
			t.Fatal("Expected cancel to be invoked on overflow")
		}
	})
}
