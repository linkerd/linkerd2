package destination

import (
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	log "github.com/sirupsen/logrus"
)

type defaultProfileListener struct {
	parent  watcher.ProfileUpdateListener
	profile *sp.ServiceProfile
	log     *log.Entry
}

func newDefaultProfileListener(
	profile *sp.ServiceProfile,
	parent watcher.ProfileUpdateListener,
	log *log.Entry,
) watcher.ProfileUpdateListener {
	return &defaultProfileListener{parent, profile, log}
}

func (p *defaultProfileListener) Update(profile *sp.ServiceProfile) {
	if profile == nil {
		log.Debug("Using default profile")
		profile = p.profile
	}
	p.parent.Update(profile)
}
