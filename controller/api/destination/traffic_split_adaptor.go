package destination

import (
	"fmt"
	"strconv"
	"strings"

	ts "github.com/deislabs/smi-sdk-go/pkg/apis/split/v1alpha1"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	log "github.com/sirupsen/logrus"
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
	// Use `DstOverrides` from the service profile if they exist.
	if len(merged.Spec.DstOverrides) != 0 {
		overrides := []*sp.WeightedDst{}
		for _, dst := range merged.Spec.DstOverrides {
			hostPort := strings.Split(dst.Service, ":")
			if len(hostPort) > 2 {
				log.Errorf("invalid dstOverride service: %s", dst.Service)
				continue
			}
			port := 80
			if len(hostPort) == 2 {
				var err error
				port, err = strconv.Atoi(hostPort[1])
				if err != nil {
					log.Errorf("invalid dstOverride service port: %s", hostPort[1])
					continue
				}
			}
			segments := strings.Split(hostPort[0], ".")
			namespace := tsa.id.Namespace
			if len(segments) >= 2 {
				namespace = segments[1]
			}
			service := segments[0]

			dst.Service = fmt.Sprintf("%s.%s.svc.cluster.local:%d", service, namespace, port)
			overrides = append(overrides, dst)
		}
		merged.Spec.DstOverrides = overrides
		// Otherwise, use `DstOverrides` from the traffic split if it exists.
	} else if tsa.split != nil {
		overrides := []*sp.WeightedDst{}
		for _, backend := range tsa.split.Spec.Backends {
			dst := &sp.WeightedDst{
				Service: fmt.Sprintf("%s.%s.svc.cluster.local:%d", backend.Service, tsa.id.Namespace, tsa.port),
				Weight:  backend.Weight,
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
