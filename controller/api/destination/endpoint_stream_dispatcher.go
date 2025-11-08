package destination

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
)

// endpointStreamDispatcher coordinates updates from multiple endpoint views
// to a single gRPC stream. It uses an unbuffered channel with timeout-based
// backpressure to detect stuck or slow Send operations.
//
// Architecture:
//   - Multiple endpointViews may enqueue updates concurrently
//   - Single process() goroutine drains updates and calls gRPC Send
//   - Unbuffered channel ensures enqueue blocks until Send completes
//   - Timeout on enqueue detects stuck Send and triggers stream reset
//
// Backpressure semantics:
//   - Each enqueue waits up to sendTimeout for the Send to complete
//   - If Send is healthy, enqueue succeeds immediately (fast path)
//   - If Send is stuck/slow, enqueue times out and resets the stream
//   - This provides fast-fail behavior regardless of update rate
type endpointStreamDispatcher struct {
	updates     chan *pb.Update
	reset       func()
	sendTimeout time.Duration

	mu     sync.Mutex
	views  map[*endpointView]struct{}
	closed atomic.Bool
}

func newEndpointStreamDispatcher(sendTimeout time.Duration, reset func()) *endpointStreamDispatcher {
	if sendTimeout <= 0 {
		sendTimeout = DefaultStreamSendTimeout
	}
	return &endpointStreamDispatcher{
		updates:     make(chan *pb.Update), // Unbuffered for immediate backpressure
		sendTimeout: sendTimeout,
		reset:       reset,
		views:       make(map[*endpointView]struct{}),
	}
}

func (d *endpointStreamDispatcher) close() {
	if d.closed.Swap(true) {
		return
	}

	d.mu.Lock()
	views := make([]*endpointView, 0, len(d.views))
	for view := range d.views {
		views = append(views, view)
	}
	d.views = nil
	d.mu.Unlock()

	for _, view := range views {
		view.close()
	}

	close(d.updates)
}

// process drains updates, invoking the provided send function for each update.
// Returning a non-nil error stops processing and propagates the error to the
// caller.
func (d *endpointStreamDispatcher) process(send func(*pb.Update) error) error {
	for update := range d.updates {
		if update == nil {
			continue
		}
		if err := send(update); err != nil {
			return err
		}
	}
	return nil
}

// enqueue sends an update to the dispatcher's process goroutine with timeout.
// This method blocks until either:
//  1. The update is received by the process goroutine (Send completed), OR
//  2. The sendTimeout expires (Send is stuck/slow)
//
// If the timeout expires, the stream is reset via the reset callback and the
// overflow counter is incremented. The update is dropped.
//
// This blocking behavior is intentional and internal to the dispatcher - it
// provides backpressure to the calling view, preventing unbounded accumulation
// of stale updates when the client is slow or unresponsive.
func (d *endpointStreamDispatcher) enqueue(update *pb.Update, overflow prometheus.Counter) {
	if d.closed.Load() || update == nil {
		return
	}

	timer := time.NewTimer(d.sendTimeout)
	defer timer.Stop()

	select {
	case d.updates <- update:
		// Update successfully handed off to process goroutine.
		// This means the previous Send (if any) has completed and the
		// process goroutine is ready to Send this update.
		return
	case <-timer.C:
		// Timeout exceeded - the process goroutine is blocked in Send,
		// indicating the client is stuck or very slow. Reset the stream
		// to allow the client to reconnect and get fresh state.
		if overflow != nil {
			overflow.Inc()
		}
		if d.reset != nil {
			d.reset()
		}
	}
}

// newEndpointView creates a new view subscribed to the given topic and
// registers it with this dispatcher.
//
// The view will filter snapshots according to cfg and enqueue updates to this
// dispatcher's process goroutine. The dispatcher tracks all registered views
// and ensures they're cleaned up when the dispatcher closes.
//
// Returns an error if the dispatcher is already closed or if view creation fails.
func (d *endpointStreamDispatcher) newEndpointView(
	ctx context.Context,
	topic EndpointTopic,
	cfg *endpointTranslatorConfig,
	log *logging.Entry,
) (*endpointView, error) {
	if d.closed.Load() {
		return nil, fmt.Errorf("dispatcher closed")
	}
	view, err := newEndpointView(ctx, topic, d, cfg, log)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	if d.closed.Load() {
		d.mu.Unlock()
		view.close()
		return nil, fmt.Errorf("dispatcher closed")
	}
	d.views[view] = struct{}{}
	d.mu.Unlock()
	return view, nil
}

// unregisterView removes a view from the dispatcher's tracking map.
// Called by views when they close (either explicitly or via context cancellation).
func (d *endpointStreamDispatcher) unregisterView(view *endpointView) {
	d.mu.Lock()
	delete(d.views, view)
	d.mu.Unlock()
}
