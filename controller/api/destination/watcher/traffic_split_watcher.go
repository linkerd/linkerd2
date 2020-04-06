package watcher

import (
	"fmt"
	"sync"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/prometheus/client_golang/prometheus"
	ts1 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	ts2 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha2"
	ts3 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha3"
	logging "github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
)

type (
	// TrafficSplitWatcher watches all TrafficSplits in the Kubernetes cluster.
	// Listeners can subscribe to a particular apex service and
	// TrafficSplitWatcher will publish all TrafficSplits for that apex service.
	TrafficSplitWatcher struct {
		k8sAPI     *k8s.API
		publishers map[ServiceID]*trafficSplitPublisher

		log          *logging.Entry
		sync.RWMutex // This mutex protects modification of the map itself.
	}

	trafficSplitPublisher struct {
		split     *ts3.TrafficSplit
		listeners []TrafficSplitUpdateListener

		log          *logging.Entry
		splitMetrics metrics
		// All access to the trafficSplitPublisher is explicitly synchronized by this mutex.
		sync.Mutex
	}

	// TrafficSplitUpdateListener is the interface that subscribers must implement.
	TrafficSplitUpdateListener interface {
		UpdateTrafficSplit(split *ts3.TrafficSplit)
	}
)

var splitVecs = newMetricsVecs("trafficsplit", []string{"namespace", "service"})

// NewTrafficSplitWatcher creates a TrafficSplitWatcher and begins watching the k8sAPI for
// TrafficSplit changes.
func NewTrafficSplitWatcher(k8sAPI *k8s.API, log *logging.Entry) *TrafficSplitWatcher {
	watcher := &TrafficSplitWatcher{
		k8sAPI:     k8sAPI,
		publishers: make(map[ServiceID]*trafficSplitPublisher),
		log:        log.WithField("component", "traffic-split-watcher"),
	}

	k8sAPI.TS1().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    watcher.addTrafficSplit,
			UpdateFunc: watcher.updateTrafficSplit,
			DeleteFunc: watcher.deleteTrafficSplit,
		},
	)
	k8sAPI.TS2().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    watcher.addTrafficSplit,
			UpdateFunc: watcher.updateTrafficSplit,
			DeleteFunc: watcher.deleteTrafficSplit,
		},
	)
	k8sAPI.TS3().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    watcher.addTrafficSplit,
			UpdateFunc: watcher.updateTrafficSplit,
			DeleteFunc: watcher.deleteTrafficSplit,
		},
	)

	return watcher
}

///
/// TrafficSplitWatcher
///

// Subscribe to a service.
// Each time a traffic split is updated with the given apex service, the
// listener will be updated.
func (tsw *TrafficSplitWatcher) Subscribe(id ServiceID, listener TrafficSplitUpdateListener) error {
	tsw.log.Infof("Establishing watch on service %s", id)

	publisher := tsw.getOrNewTrafficSplitPublisher(id, nil)

	publisher.subscribe(listener)
	return nil
}

// Unsubscribe removes a listener from the subscribers list for this service.
func (tsw *TrafficSplitWatcher) Unsubscribe(id ServiceID, listener TrafficSplitUpdateListener) error {
	tsw.log.Infof("Stopping watch on profile %s", id)

	publisher, ok := tsw.getTrafficSplitPublisher(id)
	if !ok {
		return fmt.Errorf("cannot unsubscribe from unknown service [%s] ", id)
	}
	publisher.unsubscribe(listener)
	return nil
}

func (tsw *TrafficSplitWatcher) addTrafficSplit(obj interface{}) {
	var split *ts3.TrafficSplit
	switch s := obj.(type) {
	case *ts3.TrafficSplit:
		split = s
	case *ts2.TrafficSplit:
		split = k8s.ConvertTrafficSplitV1alpha2(s)
	case *ts1.TrafficSplit:
		split = k8s.ConvertTrafficSplitV1alpha1(s)
	default:
		tsw.log.Errorf("object is not a TrafficSplit: %#v", obj)
		return
	}
	id := ServiceID{
		Name:      split.Spec.Service,
		Namespace: split.Namespace,
	}

	publisher := tsw.getOrNewTrafficSplitPublisher(id, split)

	publisher.update(split)
}

func (tsw *TrafficSplitWatcher) updateTrafficSplit(old interface{}, new interface{}) {
	tsw.addTrafficSplit(new)
}

func (tsw *TrafficSplitWatcher) deleteTrafficSplit(obj interface{}) {
	var split *ts3.TrafficSplit
	switch s := obj.(type) {
	case *ts3.TrafficSplit:
		split = s
	case *ts2.TrafficSplit:
		split = k8s.ConvertTrafficSplitV1alpha2(s)
	case *ts1.TrafficSplit:
		split = k8s.ConvertTrafficSplitV1alpha1(s)
	case cache.DeletedFinalStateUnknown:
		switch s := s.Obj.(type) {
		case *ts3.TrafficSplit:
			split = s
		case *ts2.TrafficSplit:
			split = k8s.ConvertTrafficSplitV1alpha2(s)
		case *ts1.TrafficSplit:
			split = k8s.ConvertTrafficSplitV1alpha1(s)
		default:
			tsw.log.Errorf("DeletedFinalStateUnknown contained object that is not a TrafficSplit %#v", obj)
			return
		}
	default:
		tsw.log.Errorf("object is not a TrafficSplit: %#v", obj)
		return
	}

	id := ServiceID{
		Name:      split.Spec.Service,
		Namespace: split.Namespace,
	}

	publisher, ok := tsw.getTrafficSplitPublisher(id)
	if ok {
		publisher.update(nil)
	}
}

func (tsw *TrafficSplitWatcher) getOrNewTrafficSplitPublisher(id ServiceID, split *ts3.TrafficSplit) *trafficSplitPublisher {
	tsw.Lock()
	defer tsw.Unlock()

	publisher, ok := tsw.publishers[id]
	if !ok {
		if split == nil {
			var err error
			split, err = tsw.k8sAPI.GetTrafficSplit(id.Namespace, id.Name)
			if err != nil && !apierrors.IsNotFound(err) {
				tsw.log.Errorf("error getting TrafficSplit: %s", err)
			}
			if err != nil {
				split = nil
			}
		}

		publisher = &trafficSplitPublisher{
			split:     split,
			listeners: make([]TrafficSplitUpdateListener, 0),
			log: tsw.log.WithFields(logging.Fields{
				"component": "traffic-split-publisher",
				"ns":        id.Namespace,
				"service":   id.Name,
			}),
			splitMetrics: splitVecs.newMetrics(prometheus.Labels{
				"namespace": id.Namespace,
				"service":   id.Name,
			}),
		}
		tsw.publishers[id] = publisher
	}

	return publisher
}

func (tsw *TrafficSplitWatcher) getTrafficSplitPublisher(id ServiceID) (publisher *trafficSplitPublisher, ok bool) {
	tsw.RLock()
	defer tsw.RUnlock()
	publisher, ok = tsw.publishers[id]
	return
}

///
/// trafficSplitPublisher
///

func (tsp *trafficSplitPublisher) subscribe(listener TrafficSplitUpdateListener) {
	tsp.Lock()
	defer tsp.Unlock()

	tsp.listeners = append(tsp.listeners, listener)
	listener.UpdateTrafficSplit(tsp.split)

	tsp.splitMetrics.setSubscribers(len(tsp.listeners))
}

func (tsp *trafficSplitPublisher) unsubscribe(listener TrafficSplitUpdateListener) {
	tsp.Lock()
	defer tsp.Unlock()

	for i, item := range tsp.listeners {
		if item == listener {
			// delete the item from the slice
			n := len(tsp.listeners)
			tsp.listeners[i] = tsp.listeners[n-1]
			tsp.listeners[n-1] = nil
			tsp.listeners = tsp.listeners[:n-1]
			break
		}
	}

	tsp.splitMetrics.setSubscribers(len(tsp.listeners))
}

func (tsp *trafficSplitPublisher) update(split *ts3.TrafficSplit) {
	tsp.Lock()
	defer tsp.Unlock()
	tsp.log.Debug("Updating TrafficSplit")

	tsp.split = split
	for _, listener := range tsp.listeners {
		listener.UpdateTrafficSplit(split)
	}

	tsp.splitMetrics.incUpdates()
}
