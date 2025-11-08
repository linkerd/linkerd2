package destination

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
)

// endpointView represents per-RPC stream state and filtering logic.
//
// Each gRPC Destination.Get stream has one or more endpointViews (multiple for
// federated services). A view subscribes to an EndpointTopic, filters incoming
// snapshots based on node affinity and address family, diffs successive
// snapshots, and translates changes into proxy-api Update messages.
//
// Views are isolated: each has its own subscription, filtering state, and diff
// state. This eliminates cross-stream contention that was present in the
// previous callback-based architecture.
//
// Lifecycle:
//   - Created by endpointStreamDispatcher.newEndpointView()
//   - Runs in its own goroutine (run method)
//   - Automatically unregisters from dispatcher when closed or context canceled
//   - Closed explicitly via Close() or implicitly via context cancellation
//
// Filtering:
//   - Node affinity: if enableEndpointFiltering is true, only endpoints on the
//     same node as the proxy are included (for topology-aware routing)
//   - Address family: prefers IPv6 or IPv4 based on enableIPv6 setting
//   - Opaque ports: marks endpoints with opaque protocol based on annotations
//
// State Management:
//   - available: raw snapshot from topic (before filtering)
//   - filteredSnapshot: post-filter snapshot (for diffing)
//   - snapshotVersion: monotonic version number for deduplication
//
// Thread Safety:
//   - sv.mu protects filtering and diff state from concurrent access
//   - Two goroutines can modify state concurrently:
//     1. run() goroutine: processes topic notifications
//     2. External caller: invokes NoEndpoints() (e.g., federated service watcher)
//   - The mutex ensures atomic updates when federated services are reconfigured
//     and need to send a final Remove before closing a view while the stream
//     remains active with other views
//   - Without the mutex, filtered state could be corrupted during concurrent
//     updates from topic notifications and explicit NoEndpoints() calls
type endpointView struct {
	cfg             *endpointTranslatorConfig
	log             *logging.Entry
	dispatcher      *endpointStreamDispatcher
	overflowCounter prometheus.Counter

	// Per-view state
	mu               sync.Mutex
	available        watcher.AddressSet
	filteredSnapshot watcher.AddressSet
	snapshotVersion  uint64

	ctx    context.Context
	cancel context.CancelFunc

	wg     sync.WaitGroup
	closed atomic.Bool
}

// newEndpointView creates and starts a new endpoint view subscribed to the
// given topic. The view will filter and translate snapshots into Updates that
// are enqueued to the dispatcher.
//
// Parameters:
//   - ctx: controls view lifecycle; cancellation triggers cleanup
//   - topic: source of endpoint snapshots (typically from EndpointsWatcher)
//   - dispatcher: target for filtered/diffed updates
//   - cfg: filtering and translation configuration
//   - log: structured logger for this view
//
// The view starts its processing goroutine before returning. Caller should
// defer view.Close() to ensure cleanup.
func newEndpointView(
	ctx context.Context,
	topic EndpointTopic,
	dispatcher *endpointStreamDispatcher,
	cfg *endpointTranslatorConfig,
	log *logging.Entry,
) (*endpointView, error) {
	if dispatcher == nil {
		return nil, fmt.Errorf("endpoint view requires a dispatcher")
	}
	if topic == nil {
		return nil, fmt.Errorf("endpoint view requires an endpoint topic")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	log = log.WithFields(logging.Fields{
		"component": "endpoint-view",
		"service":   cfg.service,
	})

	counter, err := updatesQueueOverflowCounter.GetMetricWith(prometheus.Labels{"service": cfg.service})
	if err != nil {
		return nil, fmt.Errorf("failed to create updates queue overflow counter: %w", err)
	}

	view := &endpointView{
		cfg:              cfg,
		log:              log,
		dispatcher:       dispatcher,
		overflowCounter:  counter,
		available:        newEmptyAddressSet(),
		filteredSnapshot: newEmptyAddressSet(),
	}

	subCtx, cancel := context.WithCancel(ctx)
	// Subscribe with notification-only channel - view will pull latest state
	// on each notification, naturally coalescing intermediate updates.
	notify, err := topic.Subscribe(subCtx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to subscribe to endpoint topic: %w", err)
	}

	view.ctx = subCtx
	view.cancel = cancel
	view.wg.Add(1)
	go view.run(topic, notify)

	return view, nil
}

// run is the main processing loop for the endpoint view.
// It receives notifications from the topic, pulls the latest snapshot,
// filters it, diffs it against the previous state, and enqueues updates
// to the dispatcher.
//
// This method runs in its own goroutine and exits when:
//   - The view's context is canceled, OR
//   - The notification channel is closed (topic shutdown)
//
// Cleanup (dispatcher unregistration) happens in a defer to ensure it
// occurs regardless of exit path.
func (sv *endpointView) run(topic EndpointTopic, notify <-chan struct{}) {
	defer sv.wg.Done()
	// Ensure we unregister this view from the dispatcher regardless of how the
	// goroutine exits (Close or parent context cancellation). This allows
	// context-based cleanup without requiring explicit Close().
	defer func() {
		if sv.dispatcher != nil {
			sv.dispatcher.unregisterView(sv)
		}
	}()
	for {
		select {
		case <-sv.ctx.Done():
			return
		case _, ok := <-notify:
			if !ok {
				return
			}
			sv.handleLatest(topic)
		}
	}
}

// handleLatest pulls the current snapshot from the topic, applies filtering
// and diffing, and enqueues resulting updates to the dispatcher.
//
// Called from the run() loop each time a notification is received.
func (sv *endpointView) handleLatest(topic EndpointTopic) {
	// Pull the latest state from the topic
	snapshot, hasSnapshot := topic.Latest()

	sv.log.Debugf("pulled latest state (hasSnapshot=%v, version=%d)", hasSnapshot, snapshot.Version)
	var updates []*pb.Update

	if hasSnapshot {
		updates = sv.onSnapshot(snapshot.Set, snapshot.Version)
	} else {
		// No snapshot means service doesn't exist or has no endpoints
		updates = sv.onNoEndpoints(true)
	}

	sv.emitUpdates(updates)
}

func (sv *endpointView) onSnapshot(set watcher.AddressSet, version uint64) []*pb.Update {
	sv.mu.Lock()
	defer sv.mu.Unlock()

	// Record the latest snapshot and version for filtering and diffing.
	sv.available = set
	sv.snapshotVersion = version

	return sv.buildFilteredUpdatesLocked()
}

func (sv *endpointView) onNoEndpoints(exists bool) []*pb.Update {
	sv.mu.Lock()
	defer sv.mu.Unlock()

	sv.log.Debugf("NoEndpoints(%+v)", exists)
	sv.available = newEmptyAddressSet()

	return sv.buildFilteredUpdatesLocked()
}

func (sv *endpointView) buildFilteredUpdatesLocked() []*pb.Update {
	filtered := filterAddresses(sv.cfg, &sv.available, sv.log)
	filtered = selectAddressFamily(sv.cfg, filtered)
	diffAdd, diffRemove := diffEndpoints(sv.filteredSnapshot, filtered)

	updates := make([]*pb.Update, 0, 2)

	if len(diffAdd.Addresses) > 0 {
		if add := buildClientAdd(sv.log, sv.cfg, diffAdd); add != nil {
			updates = append(updates, add)
		}
	}
	if len(diffRemove.Addresses) > 0 {
		if remove := buildClientRemove(sv.log, diffRemove); remove != nil {
			updates = append(updates, remove)
		}
	}

	sv.filteredSnapshot = filtered
	return updates
}

func (sv *endpointView) emitUpdates(updates []*pb.Update) {
	sv.log.Debugf("emitting %d updates", len(updates))
	for _, update := range updates {
		sv.dispatcher.enqueue(update, sv.overflowCounter)
	}
}

// NoEndpoints sends a Remove update for all currently filtered endpoints.
// This is called externally (e.g., by federatedServiceWatcher) when a view
// needs to be torn down while the stream remains active with other views.
//
// Example: A federated service changes from backend-a to backend-b. The view
// for backend-a must send Remove for its endpoints before closing, so the
// client doesn't retain stale endpoint state while the view for backend-b
// provides new endpoints on the same stream.
//
// Thread-safety: This method acquires sv.mu to safely update state concurrently
// with the run() goroutine processing topic notifications.
func (sv *endpointView) NoEndpoints(exists bool) {
	if sv == nil || sv.closed.Load() {
		return
	}
	updates := sv.onNoEndpoints(exists)
	sv.emitUpdates(updates)
}

func (sv *endpointView) Close() {
	sv.close()
}

func (sv *endpointView) close() {
	if sv == nil || !sv.closed.CompareAndSwap(false, true) {
		return
	}
	if sv.cancel != nil {
		sv.cancel()
	}
	sv.wg.Wait()
}
