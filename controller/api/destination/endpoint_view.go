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

func newEndpointView(
	ctx context.Context,
	topic watcher.EndpointTopic,
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
	// Buffer size increased from 1 to 10 to reduce blocking when multiple events
	// arrive faster than a view can process them. This mitigates head-of-line
	// blocking in high-subscriber scenarios while keeping memory overhead low.
	events, err := topic.Subscribe(subCtx, 10)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to subscribe to endpoint topic: %w", err)
	}

	view.ctx = subCtx
	view.cancel = cancel
	view.wg.Add(1)
	go view.run(events)

	return view, nil
}

func (sv *endpointView) run(events <-chan watcher.EndpointEvent) {
	defer sv.wg.Done()
	for {
		select {
		case <-sv.ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			sv.handleEvent(evt)
		}
	}
}

func (sv *endpointView) handleEvent(evt watcher.EndpointEvent) {
	sv.log.Debugf("received event (snapshot=%v noEndpoints=%v)", evt.Snapshot != nil, evt.NoEndpoints != nil)
	var updates []*pb.Update
	switch {
	case evt.Snapshot != nil:
		updates = sv.onSnapshot(evt.Snapshot.Set, evt.Snapshot.Version)
	case evt.NoEndpoints != nil:
		updates = sv.onNoEndpoints(*evt.NoEndpoints)
	default:
		return
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
	if sv.dispatcher != nil {
		sv.dispatcher.unregisterView(sv)
	}
}
