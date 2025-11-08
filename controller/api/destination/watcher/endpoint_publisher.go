package watcher

import (
	"context"

	"sync"
)

// AddressSnapshot represents an immutable view of the most recent
// AddressSet as published by a portPublisher. The Version field
// monotonically increases with each update allowing downstream consumers
// to detect duplicate notifications without retaining their own copy of
// the snapshot.
type AddressSnapshot struct {
	Version uint64
	Set     AddressSet
}

// EndpointTopic is a declarative stream of address snapshots for a specific
// service/port combination. Subscribers receive notification-only signals and
// must call Latest() to retrieve the current state. This allows natural
// coalescing: if a subscriber falls behind, it processes only the latest state
// when ready, skipping intermediate snapshots.
//
// Note: This interface is defined in the watcher package (producer side) but
// will be consumed by the destination package. This is a temporary state during
// refactoring; the interface will eventually move to the destination package
// following Go idioms (interfaces belong to consumers).
type EndpointTopic interface {
	// Subscribe returns a notification-only channel. When the channel receives
	// a signal, the subscriber should call Latest() to get current state.
	// The notification channel has size 1 to enable automatic coalescing.
	Subscribe(ctx context.Context) (<-chan struct{}, error)

	// Latest returns the current snapshot and whether endpoints exist.
	// Returns (snapshot, true) if endpoints are available.
	// Returns (empty, false) if no endpoints or service doesn't exist.
	Latest() (AddressSnapshot, bool)
}

type endpointTopic struct {
	mu              sync.RWMutex
	subscribers     map[*endpointSubscriber]struct{}
	lastSnapshot    AddressSnapshot
	hasSnapshot     bool
	lastNoEndpoints bool
	hasNoEndpoints  bool
}

type endpointSubscriber struct {
	notify chan struct{} // Size 1, notification-only
}

func newEndpointTopic() *endpointTopic {
	return &endpointTopic{
		subscribers: make(map[*endpointSubscriber]struct{}),
	}
}

func (t *endpointTopic) Subscribe(ctx context.Context) (<-chan struct{}, error) {
	sub := &endpointSubscriber{
		notify: make(chan struct{}, 1), // Size 1 for automatic coalescing
	}

	t.mu.Lock()
	t.subscribers[sub] = struct{}{}
	hasUpdate := t.hasSnapshot || t.hasNoEndpoints
	t.mu.Unlock()

	// Send initial notification if we have state
	if hasUpdate {
		select {
		case sub.notify <- struct{}{}:
		default:
		}
	}

	go func() {
		<-ctx.Done()
		t.removeSubscriber(sub)
	}()

	return sub.notify, nil
}

func (t *endpointTopic) Latest() (AddressSnapshot, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.hasSnapshot {
		return AddressSnapshot{}, false
	}
	return t.lastSnapshot, true
}

func (t *endpointTopic) publishSnapshot(snapshot AddressSnapshot) {
	t.mu.Lock()
	t.lastSnapshot = snapshot
	t.hasSnapshot = true
	t.hasNoEndpoints = false

	subs := make([]*endpointSubscriber, 0, len(t.subscribers))
	for sub := range t.subscribers {
		subs = append(subs, sub)
	}
	t.mu.Unlock()

	// Non-blocking notify - if channel is full (size 1), subscriber already
	// has a pending notification and will see the latest state when it drains.
	for _, sub := range subs {
		select {
		case sub.notify <- struct{}{}:
		default:
			// Already notified, subscriber will pull latest state
		}
	}
}

func (t *endpointTopic) publishNoEndpoints(exists bool) {
	t.mu.Lock()
	t.hasSnapshot = false
	t.hasNoEndpoints = true
	t.lastNoEndpoints = exists

	subs := make([]*endpointSubscriber, 0, len(t.subscribers))
	for sub := range t.subscribers {
		subs = append(subs, sub)
	}
	t.mu.Unlock()

	// Non-blocking notify
	for _, sub := range subs {
		select {
		case sub.notify <- struct{}{}:
		default:
		}
	}
}

func (t *endpointTopic) removeSubscriber(sub *endpointSubscriber) {
	// We intentionally do NOT close the events channel here. Closing can race
	// with publishers that have already captured the subscribers slice, leading
	// to a send-on-closed-channel panic or data race. The subscriber's context
	// cancellation ensures the consumer stops reading. By removing the
	// subscriber from the map under the lock, future publishes will not enqueue
	// more events. Any in-flight publish may still send one final event which
	// will be dropped by the consumer; this is acceptable and avoids blocking.
	t.mu.Lock()
	delete(t.subscribers, sub)
	t.mu.Unlock()
}
