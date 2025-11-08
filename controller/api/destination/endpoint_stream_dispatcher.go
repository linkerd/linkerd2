package destination

import (
	"fmt"
	"sync"
	"sync/atomic"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
)

// endpointStreamDispatcher owns the bounded per-stream queue that brokers
// watcher events to the Destination.Get send loop.
type endpointStreamDispatcher struct {
	events chan endpointEvent
	reset  func()

	mu      sync.RWMutex
	handles map[endpointHandleID]*endpointTranslator
	nextID  endpointHandleID
	closed  atomic.Bool
}

func newEndpointStreamDispatcher(capacity int, reset func()) *endpointStreamDispatcher {
	return &endpointStreamDispatcher{
		events:  make(chan endpointEvent, capacity),
		reset:   reset,
		handles: make(map[endpointHandleID]*endpointTranslator),
	}
}

func (d *endpointStreamDispatcher) close() {
	if d.closed.Swap(true) {
		return
	}
	d.mu.Lock()
	d.handles = nil
	d.mu.Unlock()
	close(d.events)
}

// process drains events, invoking the provided send function for each translated
// update. Returning a non-nil error stops processing and propagates the error to
// the caller.
func (d *endpointStreamDispatcher) process(send func(*pb.Update) error) error {
	for evt := range d.events {
		if evt.handle == 0 {
			continue
		}
		if evt.typ == endpointEventClose {
			d.unregisterTranslator(evt.handle)
			continue
		}
		translator := d.getTranslator(evt.handle)
		if translator == nil {
			continue
		}
		for _, update := range translator.handleEvent(evt) {
			if err := send(update); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *endpointStreamDispatcher) enqueue(evt endpointEvent, overflow prometheus.Counter) {
	if d.closed.Load() {
		return
	}
	select {
	case d.events <- evt:
	default:
		if overflow != nil {
			overflow.Inc()
		}
		if d.reset != nil {
			d.reset()
		}
	}
}

func (d *endpointStreamDispatcher) newTranslator(cfg endpointTranslatorConfig, log *logging.Entry) (*endpointTranslator, error) {
	if d.closed.Load() {
		return nil, fmt.Errorf("dispatcher closed")
	}
	translator, err := newEndpointTranslator(cfg, d, log)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed.Load() {
		return nil, fmt.Errorf("dispatcher closed")
	}

	d.nextID++
	translator.id = d.nextID
	d.handles[translator.id] = translator
	return translator, nil
}

func (d *endpointStreamDispatcher) getTranslator(id endpointHandleID) *endpointTranslator {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.handles[id]
}

func (d *endpointStreamDispatcher) unregisterTranslator(id endpointHandleID) {
	d.mu.Lock()
	delete(d.handles, id)
	d.mu.Unlock()
}
