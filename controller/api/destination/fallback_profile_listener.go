package destination

import (
	"sync"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
)

type fallbackProfileListener struct {
	underlying watcher.ProfileUpdateListener
	primary    *primaryProfileListener
	backup     *backupProfileListener
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
func newFallbackProfileListener(listener watcher.ProfileUpdateListener) (watcher.ProfileUpdateListener, watcher.ProfileUpdateListener) {
	// Primary and backup share a lock to ensure updates are atomic.
	fallback := fallbackProfileListener{
		mutex:      sync.Mutex{},
		underlying: listener,
	}

	primary := primaryProfileListener{
		fallbackChildListener{
			parent: &fallback,
		},
	}
	backup := backupProfileListener{
		fallbackChildListener{
			parent: &fallback,
		},
	}
	fallback.primary = &primary
	fallback.backup = &backup
	return &primary, &backup
}

// Primary

func (p *primaryProfileListener) Update(profile *sp.ServiceProfile) {
	p.parent.mutex.Lock()
	defer p.parent.mutex.Unlock()

	p.state = profile

	if p.state != nil {
		// We got a value; apply the update.
		p.parent.underlying.Update(p.state)
		return
	}
	if p.parent.backup != nil {
		// Our value was cleared; fall back to backup.
		p.parent.underlying.Update(p.parent.backup.state)
		return
	}
	// Our value was cleared and there is no backup value.
	p.parent.underlying.Update(nil)
}

// Backup

func (b *backupProfileListener) Update(profile *sp.ServiceProfile) {
	b.parent.mutex.Lock()
	defer b.parent.mutex.Unlock()

	b.state = profile

	if b.parent.primary != nil && b.parent.primary.state != nil {
		// Primary has a value, so ignore this update.
		return
	}
	if b.state != nil {
		// We got a value; apply the update.
		b.parent.underlying.Update(b.state)
		return
	}
	// Our value was cleared and there is no primary value.
	b.parent.underlying.Update(nil)
}
