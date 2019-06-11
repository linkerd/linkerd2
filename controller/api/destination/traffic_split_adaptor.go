package destination

import (
	"fmt"

	ts "github.com/deislabs/smi-sdk-go/pkg/apis/split/v1alpha1"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
)

type trafficSplitAdaptor struct {
	listener watcher.ProfileUpdateListener
	id       watcher.ServiceID
	port     watcher.Port
	profile  *sp.ServiceProfile
	split    *ts.TrafficSplit
}

func newTrafficSplitAdaptor(listener watcher.ProfileUpdateListener, id watcher.ServiceID, port watcher.Port) *trafficSplitAdaptor {
	return &trafficSplitAdaptor{
		listener: listener,
		id:       id,
		port:     port,
	}
}

func (tsa *trafficSplitAdaptor) Update(profile *sp.ServiceProfile) {
	if tsa.profile != profile {
		tsa.profile = profile
		tsa.publish()
	}
}

func (tsa *trafficSplitAdaptor) UpdateTrafficSplit(split *ts.TrafficSplit) {
	if tsa.split != split {
		tsa.split = split
		tsa.publish()
	}
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
				Authority: fmt.Sprintf("%s.%s.svc.cluster.local:%d", backend.Service, tsa.split.Namespace, tsa.port),
				Weight:    backend.Weight,
			}
			overrides = append(overrides, dst)
		}
		merged.Spec.DstOverrides = overrides
	}

	if tsa.profile == nil && tsa.split == nil {
		tsa.listener.Update(nil)
	} else {
		tsa.listener.Update(&merged)
	}
}
