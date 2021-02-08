package destination

import (
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
)

type opaquePortsAdaptor struct {
	listener    watcher.ProfileUpdateListener
	profile     *sp.ServiceProfile
	opaquePorts map[uint32]struct{}
}

func newOpaquePortsAdaptor(listener watcher.ProfileUpdateListener) *opaquePortsAdaptor {
	return &opaquePortsAdaptor{
		listener: listener,
	}
}

func (sa *opaquePortsAdaptor) Update(profile *sp.ServiceProfile) {
	sa.profile = profile
	sa.publish()
}

func (sa *opaquePortsAdaptor) UpdateService(ports map[uint32]struct{}) {
	sa.opaquePorts = ports
	sa.publish()
}

func (sa *opaquePortsAdaptor) publish() {
	merged := sp.ServiceProfile{}
	if sa.profile != nil {
		merged = *sa.profile
	}
	if len(sa.opaquePorts) != 0 {
		merged.Spec.OpaquePorts = sa.opaquePorts
	}
	sa.listener.Update(&merged)
}
