package destination

import (
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
)

// opaquePortsAdaptor holds an underlying ProfileUpdateListener and updates
// that listener with changes to a service's opaque ports annotation. It
// implements OpaquePortsUpdateListener and should be passed to a source of
// profile updates and opaque ports updates.
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

func (opa *opaquePortsAdaptor) Update(profile *sp.ServiceProfile) {
	opa.profile = profile
	opa.publish()
}

func (opa *opaquePortsAdaptor) UpdateService(ports map[uint32]struct{}) {
	opa.opaquePorts = ports
	opa.publish()
}

func (opa *opaquePortsAdaptor) publish() {
	merged := sp.ServiceProfile{}
	if opa.profile != nil {
		merged = *opa.profile
	}
	merged.Spec.OpaquePorts = opa.opaquePorts
	opa.listener.Update(&merged)
}
