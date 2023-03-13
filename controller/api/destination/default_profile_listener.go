package destination

import (
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	log "github.com/sirupsen/logrus"
)

type defaultProfileListener struct {
	parent     watcher.ProfileUpdateListener
	profile    *sp.ServiceProfile
	wasDefault bool
	log        *log.Entry
}

func newDefaultProfileListener(
	profile *sp.ServiceProfile,
	parent watcher.ProfileUpdateListener,
	log *log.Entry,
) watcher.ProfileUpdateListener {
	wasNil := false
	return &defaultProfileListener{parent, profile, wasNil, log}
}

func (p *defaultProfileListener) Update(profile *sp.ServiceProfile) {
	if profile == nil {
		profile = p.profile
	}

	if profile == p.profile {
		if p.wasDefault {
			log.Debug("Skipping redundant default profile")
			return
		}
		p.wasDefault = true
		log.Debug("Using default profile")
	} else {
		p.wasDefault = false
	}

	p.parent.Update(profile)
}
