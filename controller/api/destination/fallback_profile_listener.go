package destination

import (
	"sync"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	logging "github.com/sirupsen/logrus"
)

type fallbackProfileListener struct {
	underlying watcher.ProfileUpdateListener
	primary    *primaryProfileListener
	backup     *backupProfileListener
	log        *logging.Entry
	mutex      sync.Mutex
}

type fallbackChildListener struct {
	// state is only referenced from the outer struct primaryProfileListener
	// or backupProfileListener (e.g. listener.state where listener's type is
	// _not_ this struct). structcheck issues a false positive for this field
	// as it does not think it's used.
	//nolint:structcheck
	state  *sp.ServiceProfile
	parent *fallbackProfileListener
}

type primaryProfileListener struct {
	initialized bool
	fallbackChildListener
}

type backupProfileListener struct {
	fallbackChildListener
}

// newFallbackProfileListener takes an underlying profileUpdateListener and
// returns two profileUpdateListeners: a primary and a backup.  Updates to
// the primary and backup will propagate to the underlying with updates to
// the primary always taking priority.  If the value in the primary is cleared,
// the value from the backup is used.
func newFallbackProfileListener(
	listener watcher.ProfileUpdateListener,
	log *logging.Entry,
) (watcher.ProfileUpdateListener, watcher.ProfileUpdateListener) {
	// Primary and backup share a lock to ensure updates are atomic.
	fallback := fallbackProfileListener{
		mutex: sync.Mutex{},
		log:   log,
	}

	primary := primaryProfileListener{
		initialized: false,
		fallbackChildListener: fallbackChildListener{
			parent: &fallback,
		},
	}

	backup := backupProfileListener{
		fallbackChildListener{
			parent: &fallback,
		},
	}

	fallback.underlying = listener
	fallback.primary = &primary
	fallback.backup = &backup

	return &primary, &backup
}

func (f *fallbackProfileListener) publish() {
	profile := &sp.ServiceProfile{}

	if f.primary != nil && !f.primary.initialized {
		f.log.Debug("Waiting for primary profile listener to be initialized")
		return
	}

	if f.primary != nil && f.primary.state != nil {
		f.log.Debug("Publishing primary profil")
		profile = f.primary.state
	} else if f.backup != nil && f.backup.state != nil {
		f.log.Debug("Publishing backup profile")
		profile = f.backup.state
	}

	f.underlying.Update(profile)
}

// Primary

func (p *primaryProfileListener) Update(profile *sp.ServiceProfile) {
	p.parent.mutex.Lock()
	defer p.parent.mutex.Unlock()

	p.state = profile
	p.initialized = true
	p.parent.publish()
}

// Backup

func (b *backupProfileListener) Update(profile *sp.ServiceProfile) {
	b.parent.mutex.Lock()
	defer b.parent.mutex.Unlock()

	b.state = profile
	b.parent.publish()
}
