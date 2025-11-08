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

type snapshotView struct {
	cfg             endpointTranslatorConfig
	log             *logging.Entry
	dispatcher      *endpointStreamDispatcher
	overflowCounter prometheus.Counter
	pipeline        *translatorPipeline

	ctx    context.Context
	cancel context.CancelFunc

	wg     sync.WaitGroup
	closed atomic.Bool
}

func newSnapshotView(
	ctx context.Context,
	topic watcher.SnapshotTopic,
	dispatcher *endpointStreamDispatcher,
	cfg endpointTranslatorConfig,
	log *logging.Entry,
) (*snapshotView, error) {
	if dispatcher == nil {
		return nil, fmt.Errorf("snapshot view requires a dispatcher")
	}
	if topic == nil {
		return nil, fmt.Errorf("snapshot view requires a snapshot topic")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	log = log.WithFields(logging.Fields{
		"component": "snapshot-view",
		"service":   cfg.service,
	})

	counter, err := updatesQueueOverflowCounter.GetMetricWith(prometheus.Labels{"service": cfg.service})
	if err != nil {
		return nil, fmt.Errorf("failed to create updates queue overflow counter: %w", err)
	}

	pipeCfg := cfg
	view := &snapshotView{
		cfg:             pipeCfg,
		log:             log,
		dispatcher:      dispatcher,
		overflowCounter: counter,
	}
	view.pipeline = newTranslatorPipeline(&view.cfg, view.log)

	subCtx, cancel := context.WithCancel(ctx)
	events, err := topic.Subscribe(subCtx, 1)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to subscribe to snapshot topic: %w", err)
	}

	view.ctx = subCtx
	view.cancel = cancel
	view.wg.Add(1)
	go view.run(events)

	return view, nil
}

func (sv *snapshotView) run(events <-chan watcher.SnapshotEvent) {
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

func (sv *snapshotView) handleEvent(evt watcher.SnapshotEvent) {
	sv.log.Infof("received event (snapshot=%v noEndpoints=%v)", evt.Snapshot != nil, evt.NoEndpoints != nil)
	var updates []*pb.Update
	switch {
	case evt.Snapshot != nil:
		updates = sv.pipeline.OnSnapshot(evt.Snapshot.Set, evt.Snapshot.Version)
	case evt.NoEndpoints != nil:
		updates = sv.pipeline.OnNoEndpoints(*evt.NoEndpoints)
	default:
		return
	}

	sv.emitUpdates(updates)
}

func (sv *snapshotView) emitUpdates(updates []*pb.Update) {
	sv.log.Infof("emitting %d updates", len(updates))
	for _, update := range updates {
		sv.dispatcher.enqueue(update, sv.overflowCounter)
	}
}

func (sv *snapshotView) NoEndpoints(exists bool) {
	if sv == nil || sv.closed.Load() {
		return
	}
	updates := sv.pipeline.OnNoEndpoints(exists)
	sv.emitUpdates(updates)
}

func (sv *snapshotView) Close() {
	sv.close()
}

func (sv *snapshotView) close() {
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
