package destination

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	logging "github.com/sirupsen/logrus"
)

func TestEndpointStreamDispatcherRegistersViews(t *testing.T) {
	dispatcher := newEndpointStreamDispatcher(1, nil)
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
	resetCh := make(chan struct{}, 1)
	dispatcher := newEndpointStreamDispatcher(1, func() {
		select {
		case resetCh <- struct{}{}:
		default:
		}
	})
	defer dispatcher.close()

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
	topic.publishSnapshot(watcher.AddressSnapshot{Version: 1, Set: set})

	// Publish a second event to overflow the queue without draining.
	topic.publishNoEndpoints(true)

	select {
	case <-resetCh:
	case <-time.After(time.Second):
		t.Fatalf("expected dispatcher reset due to overflow")
	}
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
