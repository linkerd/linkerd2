// Package destination implements Linkerd's endpoint discovery streaming via a
// snapshot-based pub/sub architecture. It replaces the prior callback model
// with three composable layers optimized for high subscriber counts:
//
//  1. EndpointTopic: a fan-out publisher interface defined in this package
//     (following Go idioms: interfaces belong to consumers). The concrete
//     implementation (watcher.endpointTopic) holds the latest immutable
//     endpoint snapshot (watcher.AddressSnapshot). Watchers publish snapshots
//     to topics, and each subscriber receives notifications on its own channel.
//     A topic may carry endpoints for a single service:port or, in federated
//     mode, per-cluster variants.
//
//  2. endpointView: per-RPC stream state & filtering. A view subscribes to an
//     EndpointTopic, diffs successive snapshots, applies filtering (node
//     affinity, address family, opaque ports), and translates endpoint set
//     changes into proxy-api Update messages. Views are isolated; locks do not
//     span across subscribers, eliminating cross-stream contention.
//
//  3. endpointStreamDispatcher: coordinates gRPC Send for a stream. It manages
//     multiple views (for federated services: one local + N remote cluster
//     views) and provides backpressure via unbuffered channel with timeout.
//     Regular (non-federated) services use a dispatcher with a single view.
//
// Interface Design:
//   - EndpointTopic interface is defined in this package (consumer)
//   - watcher.endpointTopic implements the interface (producer)
//   - This follows Go best practice: "interfaces belong to consumers"
//
// Event Flow:
//
//	Kubernetes watcher -> watcher.AddressSnapshot publish -> watcher.endpointTopic
//	-> notification -> endpointView (subscribes as destination.EndpointTopic)
//	-> filtered diff -> Updates enqueued -> dispatcher.process -> gRPC stream.
//
// Concurrency & Safety Contracts:
//   - EndpointTopic implementations own their internal mutex; publishes are serialized.
//   - endpointView protects per-stream filtering & diff state with sv.mu.
//   - endpointStreamDispatcher guards its view registry with a mutex.
//   - No locks cross layer boundaries; communication occurs via channels.
//
// Backpressure & Overflow:
//   - Topic notification channels have size 1 to enable automatic coalescing.
//     Multiple publishes while a subscriber is busy collapse into a single
//     notification; the subscriber then pulls the latest state.
//   - Dispatcher uses an unbuffered channel with timeout (DefaultStreamSendTimeout).
//     Each update must complete Send within the timeout or the stream resets.
//   - This provides fast-fail behavior: stuck Send is detected within 5 seconds
//     regardless of update rate or service churn.
//
// Design Goals:
//   - Minimize memory per stream (notification-only, not event-carrying).
//   - Avoid head-of-line blocking across streams (independent flow control).
//   - Fast failure detection (unbuffered channel + timeout).
//   - Clear layering with well-defined interfaces.
//
// Federated vs Regular Services:
//   - Regular: one topic -> one view -> dispatcher -> gRPC Send
//   - Federated: multiple topics (one per remote cluster + local) -> multiple
//     views -> one shared dispatcher (N:1 fan-in for update ordering & fairness).
//
// Future Improvements (non-breaking):
//   - Standalone view variant without dispatcher for regular services (direct Send).
//   - Adaptive notification buffer sizing based on observed processing latency.
//   - Compaction of diff state for very large endpoint sets.
//
// This documentation is kept in sync with refactors; if updating architecture,
// please modify this file alongside code changes.
package destination
