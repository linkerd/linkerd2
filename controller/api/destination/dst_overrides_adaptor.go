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

func (spa *dstOverridesAdaptor) Update(profile *sp.ServiceProfile) {
	spa.profile = profile
	spa.publish()
}

func (spa *dstOverridesAdaptor) publish() {
	merged := sp.ServiceProfile{}
	if spa.profile != nil {
		merged = *spa.profile
	}
	// If there are no destination overrides set, always return a destination override
	// so that it's known the host is a service.
	if len(merged.Spec.DstOverrides) == 0 {
		dst := &sp.WeightedDst{
			Authority: fmt.Sprintf("%s.%s.svc.%s.:%d", spa.id.Name, spa.id.Namespace, spa.clusterDomain, spa.port),
			Weight:    resource.MustParse("1"),
		}
		merged.Spec.DstOverrides = []*sp.WeightedDst{dst}
	}

	spa.listener.Update(&merged)
}
