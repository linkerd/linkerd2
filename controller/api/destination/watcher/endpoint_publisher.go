package watcher

import (
	"context"

	"sync"
)

// AddressSnapshot represents an immutable view of the most recent AddressSet
// as published by a portPublisher. The snapshot combines an endpoint set with
// a monotonically-increasing version number.
//
// The Version field serves two purposes:
//  1. Allows subscribers to detect duplicate notifications without comparing
//     the full AddressSet.
//  2. Enables efficient change detection when diffing successive snapshots.
//
// Snapshots are immutable once created; the Set field should not be modified
// after construction. This immutability allows safe sharing across multiple
// subscribers without requiring defensive copies or additional locking.
//
// The snapshot is typically created by the Kubernetes endpoints watcher and
// published to an EndpointTopic, which fans it out to multiple subscribers.
type AddressSnapshot struct {
	// Version is a monotonically-increasing counter that increments with each
	// snapshot publish. Subscribers can compare versions to detect whether
	// they've already processed a snapshot.
	Version uint64

	// Set contains the actual endpoint addresses for this service:port.
	// This is an immutable snapshot; modifying it would affect all subscribers.
	Set AddressSet
}

// endpointTopic implements the destination.EndpointTopic interface, providing
// a fan-out notification mechanism for endpoint snapshot updates.
//
// This implementation is the producer side of the endpoint discovery pub/sub
// system. It is created and owned by portPublisher, which receives endpoint
// updates from Kubernetes informers and publishes them to all active
// subscribers.
//
// Architecture:
//   - One endpointTopic per service:port[:hostname] combination
//   - Maintains a single "latest" snapshot that all subscribers can pull
//   - Notification-only channels (size 1) enable automatic coalescing
//   - Subscribers are tracked in a map and cleaned up when contexts cancel
//
// Publishing:
//   - publishSnapshot() and publishNoEndpoints() update the latest state
//   - All registered subscribers receive a notification (non-blocking)
//   - Publications are serialized by the topic's mutex
//
// Subscription:
//   - Each Subscribe() call creates a new endpointSubscriber
//   - Subscriber gets its own size-1 notification channel
//   - Context cancellation triggers automatic cleanup
//
// Memory characteristics:
//   - One AddressSnapshot (shared, immutable) per topic
//   - One endpointSubscriber (channel + map entry) per active subscription
//   - Size-1 channels use minimal memory compared to buffered event queues
type endpointTopic struct {
	mu              sync.RWMutex
	subscribers     map[*endpointSubscriber]struct{}
	lastSnapshot    AddressSnapshot
	hasSnapshot     bool
	lastNoEndpoints bool
	hasNoEndpoints  bool
}

// endpointSubscriber represents a single subscription to an endpointTopic.
// Each subscriber gets its own notification channel, enabling independent
// flow control and preventing slow subscribers from blocking others.
//
// The notify channel has size 1 to enable automatic coalescing: if multiple
// publishes occur while the subscriber is busy, they collapse into a single
// pending notification. The subscriber then pulls the latest state when ready.
type endpointSubscriber struct {
	notify chan struct{} // Size 1, notification-only
}

// newEndpointTopic creates a new endpointTopic for a service:port combination.
// The topic starts with no snapshot and no subscribers. It will be populated
// when the portPublisher receives its first endpoint update from Kubernetes.
func newEndpointTopic() *endpointTopic {
	return &endpointTopic{
		subscribers: make(map[*endpointSubscriber]struct{}),
	}
}

// Subscribe registers a new subscriber and returns its notification channel.
// Implements destination.EndpointTopic.Subscribe().
//
// If the topic already has a snapshot when Subscribe() is called, an initial
// notification is sent immediately (best-effort) so the subscriber can pull
// the current state without waiting for the next update.
//
// The subscription remains active until the provided context is canceled, at
// which point the subscriber is automatically removed and its notification
// channel is closed.
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

// Latest returns the most recent snapshot and whether a snapshot exists.
// Implements destination.EndpointTopic.Latest().
//
// This method is read-only and uses RLock for efficiency, allowing concurrent
// Latest() calls from multiple subscribers without blocking each other or
// blocking publishes (which use Lock).
//
// Returns:
//   - (snapshot, true) if endpoints have been published
//   - (empty, false) if no snapshot exists yet or service has no endpoints
func (t *endpointTopic) Latest() (AddressSnapshot, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.hasSnapshot {
		return AddressSnapshot{}, false
	}
	return t.lastSnapshot, true
}

// NoEndpointsStatus returns whether the topic is in a "no endpoints" state
// and whether the service exists.
//
// Returns:
//   - (exists, true) if the topic has received a NoEndpoints notification
//   - (false, false) if the topic has a snapshot or hasn't received any state yet
func (t *endpointTopic) NoEndpointsStatus() (exists bool, hasNoEndpoints bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.hasNoEndpoints {
		return false, false
	}
	return t.lastNoEndpoints, true
}

// publishSnapshot updates the topic's snapshot and notifies all subscribers.
// Called by portPublisher when Kubernetes endpoints are updated.
//
// The snapshot parameter should be immutable; modifying it after passing to
// publishSnapshot would violate the snapshot contract and could cause data
// races in subscribers.
//
// Notifications are sent non-blocking (size-1 channels auto-coalesce), so this
// method will not block even if some subscribers are slow to process.
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

// publishNoEndpoints indicates that the service has no endpoints (either the
// service doesn't exist, or it has no ready pods). Subscribers will receive
// a notification and Latest() will return (empty, false).
//
// The exists parameter indicates whether the service itself exists:
//   - true: service exists but has no endpoints
//   - false: service does not exist
//
// This distinction is preserved for potential future use but currently both
// cases result in Latest() returning false.
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

// removeSubscriber unregisters a subscriber and closes its notification channel.
// Called when a subscription's context is canceled.
//
// After this call, the subscriber will no longer receive notifications and its
// notification channel will be closed, signaling to the subscriber's receive
// loop that the subscription has ended.
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
