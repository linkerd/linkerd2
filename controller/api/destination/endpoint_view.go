package destination

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
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
	filtered := sv.filterAddresses(&sv.available)
	filtered = sv.selectAddressFamily(filtered)
	diffAdd, diffRemove := sv.diffEndpoints(sv.filteredSnapshot, filtered)

	updates := make([]*pb.Update, 0, 2)

	if len(diffAdd.Addresses) > 0 {
		// Use pluggable strategy from config, fallback to default if not set
		buildAdd := sv.cfg.BuildClientAdd
		if buildAdd == nil {
			buildAdd = defaultBuildClientAdd
		}
		if add := buildAdd(sv.log, sv.cfg, diffAdd); add != nil {
			updates = append(updates, add)
		}
	}
	if len(diffRemove.Addresses) > 0 {
		// Use pluggable strategy from config, fallback to default if not set
		buildRemove := sv.cfg.BuildClientRemove
		if buildRemove == nil {
			buildRemove = defaultBuildClientRemove
		}
		if remove := buildRemove(sv.log, diffRemove); remove != nil {
			updates = append(updates, remove)
		}
	}

	sv.filteredSnapshot = filtered
	return updates
}

// selectAddressFamily filters addresses based on the configured IP family (IPv4 vs IPv6).
// When IPv6 is enabled, it prefers IPv6 addresses over IPv4. When disabled, only IPv4 addresses
// are returned.
func (sv *endpointView) selectAddressFamily(addresses watcher.AddressSet) watcher.AddressSet {
	filtered := make(map[watcher.ID]watcher.Address)
	for id, addr := range addresses.Addresses {
		if id.IPFamily == corev1.IPv6Protocol && !sv.cfg.enableIPv6 {
			continue
		}

		if id.IPFamily == corev1.IPv4Protocol && sv.cfg.enableIPv6 {
			// Only consider IPv4 address for which there's not already an IPv6
			// alternative.
			altID := id
			altID.IPFamily = corev1.IPv6Protocol
			if _, ok := addresses.Addresses[altID]; ok {
				continue
			}
		}

		filtered[id] = addr
	}

	return watcher.AddressSet{
		Addresses:          filtered,
		Labels:             addresses.Labels,
		LocalTrafficPolicy: addresses.LocalTrafficPolicy,
		Cluster:            addresses.Cluster,
	}
}

// filterAddresses is responsible for filtering endpoints based on the node's
// topology zone. The client will only receive endpoints with the same
// consumption zone as the node. An endpoints consumption zone is set
// by its Hints field and can be different than its actual Topology zone.
// when service.spec.internalTrafficPolicy is set to local, Topology Aware
// Hints are not used.
func (sv *endpointView) filterAddresses(available *watcher.AddressSet) watcher.AddressSet {
	filtered := make(map[watcher.ID]watcher.Address)

	// If endpoint filtering is disabled globally or unsupported by the data
	// source, return all available addresses.
	if !sv.cfg.enableEndpointFiltering || available.Cluster != "local" {
		for k, v := range available.Addresses {
			filtered[k] = v
		}
		return watcher.AddressSet{
			Addresses:          filtered,
			Labels:             available.Labels,
			LocalTrafficPolicy: available.LocalTrafficPolicy,
			Cluster:            available.Cluster,
		}
	}

	// If service.spec.internalTrafficPolicy is set to local, filter and return the addresses
	// for local node only
	if available.LocalTrafficPolicy {
		sv.log.Debugf("Filtering through addresses that should be consumed by node %s", sv.cfg.nodeName)
		for id, address := range available.Addresses {
			if address.Pod != nil && address.Pod.Spec.NodeName == sv.cfg.nodeName {
				filtered[id] = address
			}
		}
		sv.log.Debugf("Filtered from %d to %d addresses", len(available.Addresses), len(filtered))
		return watcher.AddressSet{
			Addresses:          filtered,
			Labels:             available.Labels,
			LocalTrafficPolicy: available.LocalTrafficPolicy,
			Cluster:            available.Cluster,
		}
	}
	// If any address does not have a hint, then all hints are ignored and all
	// available addresses are returned. This replicates kube-proxy behavior
	// documented in the KEP: https://github.com/kubernetes/enhancements/blob/master/keps/sig-network/2433-topology-aware-hints/README.md#kube-proxy
	for _, address := range available.Addresses {
		if len(address.ForZones) == 0 {
			for k, v := range available.Addresses {
				filtered[k] = v
			}
			sv.log.Debugf("Hints not available on endpointslice. Zone Filtering disabled. Falling back to routing to all pods")
			return watcher.AddressSet{
				Addresses:          filtered,
				Labels:             available.Labels,
				LocalTrafficPolicy: available.LocalTrafficPolicy,
				Cluster:            available.Cluster,
			}
		}
	}

	// Each address that has a hint matching the node's zone should be added
	// to the set of addresses that will be returned.
	sv.log.Debugf("Filtering through addresses that should be consumed by zone %s", sv.cfg.nodeTopologyZone)
	for id, address := range available.Addresses {
		for _, zone := range address.ForZones {
			if zone.Name == sv.cfg.nodeTopologyZone {
				filtered[id] = address
			}
		}
	}
	if len(filtered) > 0 {
		sv.log.Debugf("Filtered from %d to %d addresses", len(available.Addresses), len(filtered))
		return watcher.AddressSet{
			Addresses:          filtered,
			Labels:             available.Labels,
			LocalTrafficPolicy: available.LocalTrafficPolicy,
			Cluster:            available.Cluster,
		}
	}

	// If there were no filtered addresses, then fall to using endpoints from
	// all zones.
	for k, v := range available.Addresses {
		filtered[k] = v
	}
	return watcher.AddressSet{
		Addresses:          filtered,
		Labels:             available.Labels,
		LocalTrafficPolicy: available.LocalTrafficPolicy,
		Cluster:            available.Cluster,
	}
}

// diffEndpoints calculates the difference between the filtered set of
// endpoints in the current (Add/Remove) operation and the snapshot of
// previously filtered endpoints.
func (sv *endpointView) diffEndpoints(previous watcher.AddressSet, filtered watcher.AddressSet) (watcher.AddressSet, watcher.AddressSet) {
	add := make(map[watcher.ID]watcher.Address)
	remove := make(map[watcher.ID]watcher.Address)

	for id, new := range filtered.Addresses {
		old, ok := previous.Addresses[id]
		if !ok {
			add[id] = new
		} else if !reflect.DeepEqual(old, new) {
			add[id] = new
		}
	}

	for id, address := range previous.Addresses {
		if _, ok := filtered.Addresses[id]; !ok {
			remove[id] = address
		}
	}

	return watcher.AddressSet{
			Addresses:          add,
			Labels:             filtered.Labels,
			LocalTrafficPolicy: filtered.LocalTrafficPolicy,
			Cluster:            filtered.Cluster,
		},
		watcher.AddressSet{
			Addresses:          remove,
			Labels:             filtered.Labels,
			LocalTrafficPolicy: filtered.LocalTrafficPolicy,
			Cluster:            filtered.Cluster,
		}
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
