package destination

import (
	"context"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
)

// EndpointTopic represents a notification-based stream of endpoint snapshots
// for a specific service:port combination. It implements a pull-based model
// where subscribers receive lightweight notifications and must explicitly call
// Latest() to retrieve the current snapshot state.
//
// This interface follows Go idioms by being defined in the consumer package
// (destination) rather than the producer package (watcher). The watcher package
// provides concrete implementations that satisfy this interface.
//
// Notification Semantics:
//   - Notifications are delivered on a size-1 channel, enabling automatic
//     coalescing: if multiple updates occur while a subscriber is busy, they
//     collapse into a single notification.
//   - Subscribers call Latest() to pull the current state when ready, ensuring
//     they always process the most recent data without queuing intermediate
//     snapshots.
//   - Slow subscribers don't block publishers or other subscribers; each
//     subscription has independent flow control.
//
// Lifecycle:
//   - Subscribe(ctx) registers a new subscriber and returns its notification
//     channel. The subscription remains active until the context is canceled.
//   - When the context is canceled, the subscription is automatically cleaned
//     up and the notification channel is closed.
//   - Multiple concurrent subscriptions are supported; each receives
//     independent notifications.
//
// Thread Safety:
//   - All methods are safe for concurrent use.
//   - Subscribe() can be called concurrently from multiple goroutines.
//   - Latest() can be called concurrently with Subscribe() and publishes.
//
// Example Usage:
//
//	topic, err := endpoints.Topic(serviceID, port, hostname)
//	if err != nil {
//	    return err
//	}
//
//	notify, err := topic.Subscribe(ctx)
//	if err != nil {
//	    return err
//	}
//
//	for {
//	    select {
//	    case <-notify:
//	        snapshot, ok := topic.Latest()
//	        if ok {
//	            // Process snapshot...
//	        }
//	    case <-ctx.Done():
//	        return
//	    }
//	}
type EndpointTopic interface {
	// Subscribe registers a new subscriber and returns a notification channel.
	// The notification channel has size 1 to enable automatic coalescing.
	//
	// When a new snapshot is published:
	//   1. A notification is sent to the channel (non-blocking due to size 1)
	//   2. If the channel already has a pending notification, the send is a no-op
	//   3. This ensures at-most-one pending notification per subscriber
	//
	// The subscriber should drain the notification channel and call Latest() to
	// retrieve the current snapshot. Multiple notifications may represent a
	// single snapshot if the subscriber was busy.
	//
	// The notification channel is closed when the provided context is canceled.
	// Subscribers should handle both notification receives and context
	// cancellation in their select loops.
	//
	// Returns an error if subscription fails (e.g., if the topic is shutting down).
	Subscribe(ctx context.Context) (<-chan struct{}, error)

	// Latest returns the most recent snapshot and whether a snapshot exists.
	//
	// Returns:
	//   - (snapshot, true) if endpoints are available for this service:port
	//   - (empty, false) if no endpoints exist (service may not exist, may have
	//     no ready pods, or may have no endpoints matching the port)
	//
	// This method is pull-based: callers should invoke it after receiving a
	// notification to retrieve current state. Calling Latest() without a
	// notification is safe but may return stale data.
	//
	// The returned snapshot is immutable and safe to use without further
	// synchronization. The snapshot's Version field monotonically increases,
	// allowing subscribers to detect duplicate notifications.
	//
	// Thread-safe: can be called concurrently with Subscribe() and publishes.
	Latest() (watcher.AddressSnapshot, bool)
}
