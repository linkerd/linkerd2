package destination

import (
	"fmt"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	ts "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// trafficSplitAdaptor merges traffic splits into service profiles, encoding
// them as dst overrides.  trafficSplitAdaptor holds an underlying
// ProfileUpdateListener and updates that listener with a merged service
// service profile which includes the traffic split logic as a dst override
// when a traffic split exists.  trafficSplitAdaptor itself implements both
// ProfileUpdateListener and TrafficSplitUpdateListener and must be passed to
// a source of profile updates (such as a ProfileWatcher) and a source of
// traffic split updates (such as a TrafficSplitWatcher).
type trafficSplitAdaptor struct {
	listener      watcher.ProfileUpdateListener
	id            watcher.ServiceID
	port          watcher.Port
	profile       *sp.ServiceProfile
	split         *ts.TrafficSplit
	clusterDomain string
}

func newTrafficSplitAdaptor(listener watcher.ProfileUpdateListener, id watcher.ServiceID, port watcher.Port, clusterDomain string) *trafficSplitAdaptor {
	return &trafficSplitAdaptor{
		listener:      listener,
		id:            id,
		port:          port,
		clusterDomain: clusterDomain,
	}
}

func (tsa *trafficSplitAdaptor) Update(profile *sp.ServiceProfile) {
	tsa.profile = profile
	tsa.publish()
}

func (tsa *trafficSplitAdaptor) UpdateTrafficSplit(split *ts.TrafficSplit) {
	if tsa.split == nil && split == nil {
		return
	}
	tsa.split = split
	tsa.publish()
}

func (tsa *trafficSplitAdaptor) publish() {
	merged := sp.ServiceProfile{}
	if tsa.profile != nil {
		merged = *tsa.profile
	}
	if tsa.split != nil {
		overrides := []*sp.WeightedDst{}
		for _, backend := range tsa.split.Spec.Backends {
			dst := &sp.WeightedDst{
				// The proxy expects authorities to be absolute and have the
				// host part end with a trailing dot.
				Authority: fmt.Sprintf("%s.%s.svc.%s.:%d", backend.Service, tsa.id.Namespace, tsa.clusterDomain, tsa.port),
				Weight:    *backend.Weight,
			}
			overrides = append(overrides, dst)
		}
		merged.Spec.DstOverrides = overrides
	} else {
		// If there is no traffic split, always return a destination override
		// so that it's known the host is a service.
		dst := &sp.WeightedDst{
			Authority: fmt.Sprintf("%s.%s.svc.%s.:%d", tsa.id.Name, tsa.id.Namespace, tsa.clusterDomain, tsa.port),
			Weight:    resource.MustParse("1"),
		}
		merged.Spec.DstOverrides = []*sp.WeightedDst{dst}
	}

	tsa.listener.Update(&merged)
}
