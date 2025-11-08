// Package destination implements Linkerd's endpoint discovery streaming via a
// snapshot-based pub/sub architecture. It replaces the prior callback model
// with three composable layers optimized for high subscriber counts:
//
//  1. EndpointTopic: a fan-out publisher that holds the latest immutable
//     endpoint snapshot (AddressSnapshot). Watchers publish snapshots here.
//     Each subscriber receives events on its own channel. A topic may carry
//     endpoints for a single service:port or, in federated mode, per-cluster
//     variants.
//
//  2. endpointView: per-RPC stream state & filtering. A view subscribes to a
//     topic, diffs successive snapshots, applies filtering (identity, hints,
//     address family, opaque ports), and translates endpoint set changes into
//     proxy-api Update messages. Views are isolated; locks do not span across
//     subscribers, eliminating cross-stream contention.
//
//  3. endpointStreamDispatcher: shared gRPC send queue for a group of views
//     (primarily federated services: one local + N remote cluster views). It
//     multiplexes updates, provides overflow protection (counting drops via a
//     Prometheus counter), and ensures cleanup of subscriptions when streams
//     terminate. Regular (non-federated) services currently still allocate a
//     dispatcher, but an optimization can instantiate a standalone view with a
//     direct send path (planned future improvement).
//
// Event Flow:
//
//	Kubernetes watcher -> AddressSnapshot publish -> EndpointTopic -> endpointView
//	-> filtered diff -> Updates enqueued -> dispatcher.process -> gRPC stream.
//
// Concurrency & Safety Contracts:
//   - EndpointTopic owns its internal mutex; publishes are serialized.
//   - endpointView protects per-stream filtering & diff state with sv.mu.
//   - endpointStreamDispatcher guards its view registry & queue with a mutex.
//   - No locks cross layer boundaries; communication occurs via channels.
//
// Backpressure & Overflow:
//   - Topic subscription channels have a bounded buffer (currently 10) to absorb
//     short bursts while a view filters previous events.
//   - Dispatcher queue size is bounded; on overflow it resets queued updates and
//     increments an overflow counter, signalling potential downstream slowness.
//
// Design Goals:
//   - Minimize memory per stream.
//   - Avoid head-of-line blocking across streams.
//   - Provide clear layering for future standalone-view optimization.
//
// Federated vs Regular Services:
//   - Regular: one topic -> one view -> (future: direct send)
//   - Federated: multiple topics (one per remote cluster + local) -> multiple
//     views -> one shared dispatcher (N:1 fan-in for update ordering & fairness).
//
// Future Improvements (non-breaking):
//   - Implement standalone view variant without dispatcher for regular services.
//   - Adaptive subscription buffer sizing based on observed processing latency.
//   - Compaction of diff state for very large endpoint sets.
//
// This documentation is kept in sync with refactors; if updating architecture,
// please modify this file alongside code changes.
package destination
