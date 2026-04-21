package destination

import (
	"sync"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	logging "github.com/sirupsen/logrus"
)

type fallbackProfileListener struct {
	primary, backup *childListener
	parent          watcher.ProfileUpdateListener
	log             *logging.Entry
	mutex           sync.Mutex
}

type childListener struct {
	// state is only referenced from the outer struct primaryProfileListener
	// or backupProfileListener (e.g. listener.state where listener's type is
	// _not_ this struct). structcheck issues a false positive for this field
	// as it does not think it's used.
	//nolint:structcheck
	state       *sp.ServiceProfile
	initialized bool
	parent      *fallbackProfileListener
}

// newFallbackProfileListener takes a parent ProfileUpdateListener and returns
// two ProfileUpdateListeners: a primary and a backup.
//
// If the primary listener is updated with a non-nil value, it is published to
// the parent listener.
//
// Otherwise, if the backup listener has most recently been updated with a
// non-nil value, its valeu is published to the parent listener.
//
// A nil ServiceProfile is published only when both the primary and backup have
// been initialized and have nil values.
func newFallbackProfileListener(
	parent watcher.ProfileUpdateListener,
	log *logging.Entry,
) (watcher.ProfileUpdateListener, watcher.ProfileUpdateListener) {
	// Primary and backup share a lock to ensure updates are atomic.
	fallback := fallbackProfileListener{
		mutex: sync.Mutex{},
		log:   log,
	}

	primary := childListener{
		initialized: false,
		parent:      &fallback,
	}

	backup := childListener{
		initialized: false,
		parent:      &fallback,
	}

	fallback.parent = parent
	fallback.primary = &primary
	fallback.backup = &backup

	return &primary, &backup
}

func (f *fallbackProfileListener) publish() {
	if !f.primary.initialized {
		f.log.Debug("Waiting for primary profile listener to be initialized")
		return
	}
	if !f.backup.initialized {
		f.log.Debug("Waiting for backup profile listener to be initialized")
		return
	}

	if f.primary.state == nil && f.backup.state != nil {
		f.log.Debug("Publishing backup profile")
		f.parent.Update(f.backup.state)
		return
	}

	f.log.Debug("Publishing primary profile")
	f.parent.Update(f.primary.state)
}

func (p *childListener) Update(profile *sp.ServiceProfile) {
	p.parent.mutex.Lock()
	defer p.parent.mutex.Unlock()

	p.state = profile
	p.initialized = true
	p.parent.publish()
}
