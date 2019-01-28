package proxy

import (
	"sync"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
)

type fallbackProfileListener struct {
	underlying profileUpdateListener
	state      *sp.ServiceProfile
	primary    *fallbackProfileListener
	backup     *fallbackProfileListener
	mutex      *sync.Mutex
}

// newFallbackProfileListener takes an underlying profileUpdateListener and
// returns two profileUpdateListeners: a primary and a secondary.  Updates to
// the primary and secondary will propagate to the underlying with updates to
// the primary always taking priority.  If the value in the primary is cleared,
// the value from the secondary is used.
func newFallbackProfileListener(listener profileUpdateListener) (profileUpdateListener, profileUpdateListener) {
	// Primary and secondary share a lock to ensure updates are atomic.
	mutex := sync.Mutex{}

	primary := fallbackProfileListener{
		underlying: listener,
		mutex:      &mutex,
	}
	secondary := fallbackProfileListener{
		underlying: listener,
		mutex:      &mutex,
	}
	primary.backup = &secondary
	secondary.primary = &primary
	return &primary, &secondary
}

func (f *fallbackProfileListener) Update(profile *sp.ServiceProfile) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.state = profile

	if f.primary != nil && f.primary.state != nil {
		// Primary has a value, so ignore this update.
		return
	}
	if f.state != nil {
		// We got a value; apply the update.
		f.underlying.Update(f.state)
		return
	}
	if f.backup != nil {
		// Our value was cleared; fall back to backup.
		f.underlying.Update(f.backup.state)
		return
	}
	// Our value was cleared and there is no backup.
	f.underlying.Update(nil)
}

func (f fallbackProfileListener) ClientClose() <-chan struct{} {
	return f.underlying.ClientClose()
}

func (f fallbackProfileListener) ServerClose() <-chan struct{} {
	return f.underlying.ServerClose()
}

func (f fallbackProfileListener) Stop() {
	f.underlying.Stop()
}
