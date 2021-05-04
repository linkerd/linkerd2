package destination

import (
	"fmt"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	"k8s.io/apimachinery/pkg/api/resource"
)

type serviceprofileAdaptor struct {
	listener      watcher.ProfileUpdateListener
	id            watcher.ServiceID
	port          watcher.Port
	profile       *sp.ServiceProfile
	clusterDomain string
}

func newServiceProfileAdaptor(listener watcher.ProfileUpdateListener, id watcher.ServiceID, port watcher.Port, clusterDomain string) *serviceprofileAdaptor {
	return &serviceprofileAdaptor{
		listener:      listener,
		id:            id,
		port:          port,
		clusterDomain: clusterDomain,
	}
}

func (spa *serviceprofileAdaptor) Update(profile *sp.ServiceProfile) {
	spa.profile = profile
	spa.publish()
}

func (spa *serviceprofileAdaptor) publish() {
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
