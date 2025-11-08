package destination

import (
	"context"
	"fmt"
	"testing"
	"time"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	logging "github.com/sirupsen/logrus"
)

func TestEndpointStreamDispatcherRegistersViews(t *testing.T) {
	dispatcher := newEndpointStreamDispatcher(0, nil)
	topic := newMockSnapshotTopic()
	cfg := endpointTranslatorConfig{
		controllerNS:            "linkerd",
		identityTrustDomain:     "trust.domain",
		defaultOpaquePorts:      map[uint32]struct{}{},
		enableH2Upgrade:         true,
		enableEndpointFiltering: true,
		enableIPv6:              false,
		service:                 "svc.ns",
	}

	view, err := dispatcher.newEndpointView(context.Background(), topic, &cfg, logging.WithField("test", t.Name()))
	if err != nil {
		t.Fatalf("failed to create snapshot view: %s", err)
	}

	dispatcher.mu.Lock()
	if len(dispatcher.views) != 1 {
		t.Fatalf("expected 1 registered view, got %d", len(dispatcher.views))
	}
	dispatcher.mu.Unlock()

	view.Close()

	dispatcher.mu.Lock()
	if len(dispatcher.views) != 0 {
		t.Fatalf("expected dispatcher to unregister view, still have %d", len(dispatcher.views))
	}
	dispatcher.mu.Unlock()

	dispatcher.close()

	err = retry(func() bool {
		topic.mu.Lock()
		defer topic.mu.Unlock()
		return len(topic.subscribers) == 0
	})
	if err != nil {
		t.Fatalf("expected topic subscribers to drain: %s", err)
	}
}

func TestEndpointStreamDispatcherQueueOverflowResets(t *testing.T) {
	resetCalled := false
	resetCh := make(chan struct{}, 1)

	// Use a very short send timeout (50ms) for testing
	sendTimeout := 50 * time.Millisecond

	dispatcher := newEndpointStreamDispatcher(sendTimeout, func() {
		t.Log("Reset function called")
		resetCalled = true
		select {
		case resetCh <- struct{}{}:
		default:
		}
	})
	defer dispatcher.close()

	// Start a process goroutine that blocks on Send to simulate slow consumer
	sendBlocked := make(chan struct{})
	go func() {
		_ = dispatcher.process(func(*pb.Update) error {
			<-sendBlocked // Block indefinitely
			return nil
		})
	}()

	topic := newMockSnapshotTopic()
	cfg := endpointTranslatorConfig{
		controllerNS:            "linkerd",
		identityTrustDomain:     "trust.domain",
		defaultOpaquePorts:      map[uint32]struct{}{},
		enableH2Upgrade:         true,
		enableEndpointFiltering: true,
		enableIPv6:              false,
		service:                 "svc.ns",
	}
	view, err := dispatcher.newEndpointView(context.Background(), topic, &cfg, logging.WithField("test", t.Name()))
	if err != nil {
		t.Fatalf("failed to create snapshot view: %s", err)
	}
	defer view.Close()

	set := mkAddressSetForServices(remoteGateway1)
	set2 := mkAddressSetForServices(remoteGateway2) // Different set

	// Publish snapshot to trigger view to enqueue an update.
	// With unbuffered channel, the first update will be received by process
	// goroutine, but process will block in Send (which is blocked on sendBlocked).
	t.Log("Publishing first snapshot")
	topic.publishSnapshot(watcher.AddressSnapshot{Version: 1, Set: set})

	// Give view time to process notification and attempt enqueue
	time.Sleep(50 * time.Millisecond)
	t.Log("First update should be blocking in Send")

	// Publish another snapshot with DIFFERENT data - view will process this
	// notification and try to enqueue, but the process goroutine is still
	// blocked in Send, so enqueue will timeout and call reset()
	t.Log("Publishing second snapshot (different data) to trigger timeout")
	topic.publishSnapshot(watcher.AddressSnapshot{Version: 2, Set: set2})

	// Wait for timeout + some buffer
	select {
	case <-resetCh:
		// Expected: send timeout triggered reset
		t.Log("Reset signal received successfully")
	case <-time.After(sendTimeout + 500*time.Millisecond):
		if !resetCalled {
			t.Fatalf("expected dispatcher reset due to send timeout, but reset was never called")
		}
		t.Fatalf("reset was called but signal not received")
	}

	close(sendBlocked)
}

func retry(cond func() bool) error {
	timeout := time.After(time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-timeout:
			return fmt.Errorf("condition not met before timeout")
		case <-tick.C:
			if cond() {
				return nil
			}
		}
	}
}
