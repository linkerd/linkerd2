package destination

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
)

// endpointStreamDispatcher owns the bounded per-stream queue that brokers
// watcher events to the Destination.Get send loop.
type endpointStreamDispatcher struct {
	updates     chan *pb.Update
	reset       func()
	sendTimeout time.Duration

	mu     sync.Mutex
	views  map[*endpointView]struct{}
	closed atomic.Bool
}

func newEndpointStreamDispatcher(capacity int, sendTimeout time.Duration, reset func()) *endpointStreamDispatcher {
	if capacity <= 0 {
		capacity = 1
	}
	if sendTimeout <= 0 {
		sendTimeout = DefaultStreamSendTimeout
	}
	return &endpointStreamDispatcher{
		updates:     make(chan *pb.Update, capacity),
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

func (d *endpointStreamDispatcher) enqueue(update *pb.Update, overflow prometheus.Counter) {
	if d.closed.Load() || update == nil {
		return
	}

	// First try non-blocking send
	select {
	case d.updates <- update:
		return
	default:
	}

	// Queue is full - try with timeout before resetting stream
	timer := time.NewTimer(d.sendTimeout)
	defer timer.Stop()

	select {
	case d.updates <- update:
		// Successfully enqueued within timeout
		return
	case <-timer.C:
		// Timeout exceeded - queue is persistently full, reset stream
		if overflow != nil {
			overflow.Inc()
		}
		if d.reset != nil {
			d.reset()
		}
	}
}

func (d *endpointStreamDispatcher) newEndpointView(
	ctx context.Context,
	topic watcher.EndpointTopic,
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

func (d *endpointStreamDispatcher) unregisterView(view *endpointView) {
	d.mu.Lock()
	delete(d.views, view)
	d.mu.Unlock()
}
