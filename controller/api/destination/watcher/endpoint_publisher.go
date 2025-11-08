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

// EndpointEvent represents a change notification published on an EndpointTopic.
// Exactly one of Snapshot or NoEndpoints will be non-nil.
type EndpointEvent struct {
	Snapshot    *AddressSnapshot
	NoEndpoints *bool
}

// EndpointTopic is a declarative stream of address snapshots for a specific
// service/port combination. Subscribers receive immutable snapshots and may
// safely skip intermediate versions if they fall behind.
//
// Note: This interface is defined in the watcher package (producer side) but
// will be consumed by the destination package. This is a temporary state during
// refactoring; the interface will eventually move to the destination package
// following Go idioms (interfaces belong to consumers).
type EndpointTopic interface {
	Subscribe(ctx context.Context, buffer int) (<-chan EndpointEvent, error)
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
	events chan EndpointEvent
}

func newEndpointTopic() *endpointTopic {
	return &endpointTopic{
		subscribers: make(map[*endpointSubscriber]struct{}),
	}
}

func (t *endpointTopic) Subscribe(ctx context.Context, buffer int) (<-chan EndpointEvent, error) {
	if buffer <= 0 {
		buffer = 1
	}

	sub := &endpointSubscriber{
		events: make(chan EndpointEvent, buffer),
	}

	t.mu.Lock()
	t.subscribers[sub] = struct{}{}
	snapshot, hasSnapshot := t.lastSnapshot, t.hasSnapshot
	noEndpoints, hasNoEndpoints := t.lastNoEndpoints, t.hasNoEndpoints
	t.mu.Unlock()

	if hasSnapshot {
		// Deliver the latest snapshot immediately.
		snapCopy := snapshot
		sub.events <- EndpointEvent{Snapshot: &snapCopy}
	} else if hasNoEndpoints {
		noCopy := noEndpoints
		sub.events <- EndpointEvent{NoEndpoints: &noCopy}
	}

	go func() {
		<-ctx.Done()
		t.removeSubscriber(sub)
	}()

	return sub.events, nil
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

	for _, sub := range subs {
		snapCopy := snapshot
		sub.events <- EndpointEvent{Snapshot: &snapCopy}
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

	for _, sub := range subs {
		existsCopy := exists
		sub.events <- EndpointEvent{NoEndpoints: &existsCopy}
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
