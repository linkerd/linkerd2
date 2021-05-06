package destination

import (
	"fmt"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"k8s.io/apimachinery/pkg/api/resource"
)

// dstOverridesAdaptor holds an underlying ProfileUpdateListener and updates
// that listener with changes to the dstOverrides field of serviceProfiles.
type dstOverridesAdaptor struct {
	listener      watcher.ProfileUpdateListener
	id            watcher.ServiceID
	port          watcher.Port
	profile       *sp.ServiceProfile
	clusterDomain string
}

func newDSTOverridesAdaptor(listener watcher.ProfileUpdateListener, id watcher.ServiceID, port watcher.Port, clusterDomain string) *dstOverridesAdaptor {
	return &dstOverridesAdaptor{
		listener:      listener,
		id:            id,
		port:          port,
		clusterDomain: clusterDomain,
	}
}

func (doa *dstOverridesAdaptor) Update(profile *sp.ServiceProfile) {
	doa.profile = profile
	doa.publish()
}

func (doa *dstOverridesAdaptor) publish() {
	merged := sp.ServiceProfile{}
	if doa.profile != nil {
		merged = *doa.profile
	}
	// If there are no destination overrides set, always return a destination override
	// so that it's known the host is a service.
	if len(merged.Spec.DstOverrides) == 0 {
		dst := &sp.WeightedDst{
			Authority: fmt.Sprintf("%s.%s.svc.%s.:%d", doa.id.Name, doa.id.Namespace, doa.clusterDomain, doa.port),
			Weight:    resource.MustParse("1"),
		}
		merged.Spec.DstOverrides = []*sp.WeightedDst{dst}
	}

	doa.listener.Update(&merged)
}
