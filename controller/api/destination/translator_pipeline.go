package destination

import (
	"sync"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	logging "github.com/sirupsen/logrus"
)

// translatorPipeline owns the mutable state required to transform watcher
// updates into Destination service updates. Separating this from
// endpointTranslator simplifies future reuse by other subscribers.
type translatorPipeline struct {
	cfg *endpointTranslatorConfig
	log *logging.Entry

	mu               sync.Mutex
	available        watcher.AddressSet
	filteredSnapshot watcher.AddressSet
	snapshotVersion  uint64
}

func newTranslatorPipeline(cfg *endpointTranslatorConfig, log *logging.Entry) *translatorPipeline {
	return &translatorPipeline{
		cfg:              cfg,
		log:              log,
		available:        newEmptyAddressSet(),
		filteredSnapshot: newEmptyAddressSet(),
	}
}

func (tp *translatorPipeline) OnSnapshot(set watcher.AddressSet, version uint64) []*pb.Update {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	// Store a shallow copy so downstream filters can treat the snapshot as
	// immutable while we retain the caller's map for future comparisons.
	tp.available = set
	tp.snapshotVersion = version

	return tp.buildFilteredUpdatesLocked()
}

func (tp *translatorPipeline) OnNoEndpoints(exists bool) []*pb.Update {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	tp.log.Debugf("NoEndpoints(%+v)", exists)
	tp.available = newEmptyAddressSet()

	return tp.buildFilteredUpdatesLocked()
}

func (tp *translatorPipeline) buildFilteredUpdatesLocked() []*pb.Update {
	filtered := filterAddresses(tp.cfg, &tp.available, tp.log)
	filtered = selectAddressFamily(tp.cfg, filtered)
	diffAdd, diffRemove := diffEndpoints(tp.filteredSnapshot, filtered)

	updates := make([]*pb.Update, 0, 2)

	if len(diffAdd.Addresses) > 0 {
		if add := buildClientAdd(tp.log, tp.cfg, diffAdd); add != nil {
			updates = append(updates, add)
		}
	}
	if len(diffRemove.Addresses) > 0 {
		if remove := buildClientRemove(tp.log, diffRemove); remove != nil {
			updates = append(updates, remove)
		}
	}

	tp.filteredSnapshot = filtered
	return updates
}
