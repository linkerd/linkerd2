package watcher

import (
	"context"

	"sync"
)

// SnapshotEvent represents a change notification published on a SnapshotTopic.
// Exactly one of Snapshot or NoEndpoints will be non-nil.
type SnapshotEvent struct {
	Snapshot    *AddressSnapshot
	NoEndpoints *bool
}

// SnapshotTopic is a declarative stream of address snapshots for a specific
// service/port combination. Subscribers receive immutable snapshots and may
// safely skip intermediate versions if they fall behind.
type SnapshotTopic interface {
	Subscribe(ctx context.Context, buffer int) (<-chan SnapshotEvent, error)
	Latest() (AddressSnapshot, bool)
}

type snapshotTopic struct {
	mu              sync.RWMutex
	subscribers     map[*topicSubscriber]struct{}
	lastSnapshot    AddressSnapshot
	hasSnapshot     bool
	lastNoEndpoints bool
	hasNoEndpoints  bool
}

type topicSubscriber struct {
	events chan SnapshotEvent
}

func newSnapshotTopic() *snapshotTopic {
	return &snapshotTopic{
		subscribers: make(map[*topicSubscriber]struct{}),
	}
}

func (t *snapshotTopic) Subscribe(ctx context.Context, buffer int) (<-chan SnapshotEvent, error) {
	if buffer <= 0 {
		buffer = 1
	}

	sub := &topicSubscriber{
		events: make(chan SnapshotEvent, buffer),
	}

	t.mu.Lock()
	t.subscribers[sub] = struct{}{}
	snapshot, hasSnapshot := t.lastSnapshot, t.hasSnapshot
	noEndpoints, hasNoEndpoints := t.lastNoEndpoints, t.hasNoEndpoints
	t.mu.Unlock()

	if hasSnapshot {
		// Deliver the latest snapshot immediately.
		snapCopy := snapshot
		sub.events <- SnapshotEvent{Snapshot: &snapCopy}
	} else if hasNoEndpoints {
		noCopy := noEndpoints
		sub.events <- SnapshotEvent{NoEndpoints: &noCopy}
	}

	go func() {
		<-ctx.Done()
		t.removeSubscriber(sub)
	}()

	return sub.events, nil
}

func (t *snapshotTopic) Latest() (AddressSnapshot, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.hasSnapshot {
		return AddressSnapshot{}, false
	}
	return t.lastSnapshot, true
}

func (t *snapshotTopic) publishSnapshot(snapshot AddressSnapshot) {
	t.mu.Lock()
	t.lastSnapshot = snapshot
	t.hasSnapshot = true
	t.hasNoEndpoints = false

	subs := make([]*topicSubscriber, 0, len(t.subscribers))
	for sub := range t.subscribers {
		subs = append(subs, sub)
	}
	t.mu.Unlock()

	for _, sub := range subs {
		snapCopy := snapshot
		sub.events <- SnapshotEvent{Snapshot: &snapCopy}
	}
}

func (t *snapshotTopic) publishNoEndpoints(exists bool) {
	t.mu.Lock()
	t.hasSnapshot = false
	t.hasNoEndpoints = true
	t.lastNoEndpoints = exists

	subs := make([]*topicSubscriber, 0, len(t.subscribers))
	for sub := range t.subscribers {
		subs = append(subs, sub)
	}
	t.mu.Unlock()

	for _, sub := range subs {
		existsCopy := exists
		sub.events <- SnapshotEvent{NoEndpoints: &existsCopy}
	}
}

func (t *snapshotTopic) removeSubscriber(sub *topicSubscriber) {
	t.mu.Lock()
	if _, ok := t.subscribers[sub]; ok {
		delete(t.subscribers, sub)
		close(sub.events)
	}
	t.mu.Unlock()
}
