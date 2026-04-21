package destination

import (
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	log "github.com/sirupsen/logrus"
)

type dedupProfileListener struct {
	parent      watcher.ProfileUpdateListener
	state       *sp.ServiceProfile
	initialized bool
	log         *log.Entry
}

func newDedupProfileListener(
	parent watcher.ProfileUpdateListener,
	log *log.Entry,
) watcher.ProfileUpdateListener {
	return &dedupProfileListener{parent, nil, false, log}
}

func (p *dedupProfileListener) Update(profile *sp.ServiceProfile) {
	if p.initialized && profile == p.state {
		log.Debug("Skipping redundant update")
		return
	}
	p.parent.Update(profile)
	p.initialized = true
	p.state = profile
}
