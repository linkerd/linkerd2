package watcher

import (
	"fmt"
	"sync"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

type (
	IPWatcher struct {
		publishers map[string]*serviceSubscriptions
		endpoints  *EndpointsWatcher
		k8sAPI     *k8s.API

		log          *logging.Entry
		sync.RWMutex // This mutex protects modification of the map itself.
	}

	serviceSubscriptions struct {
		id        ServiceID
		listeners map[EndpointUpdateListener]Port
		endpoints *EndpointsWatcher

		log *logging.Entry
		// All access to the servicePublisher and its portPublishers is explicitly synchronized by
		// this mutex.
		sync.Mutex
	}
)

func NewIPWatcher(k8sAPI *k8s.API, endpoints *EndpointsWatcher, log *logging.Entry) *IPWatcher {
	iw := &IPWatcher{
		publishers: make(map[string]*serviceSubscriptions),
		endpoints:  endpoints,
		k8sAPI:     k8sAPI,
		log: log.WithFields(logging.Fields{
			"component": "ip-watcher",
		}),
	}

	k8sAPI.Svc().Informer().AddIndexers(cache.Indexers{podIPIndex: func(obj interface{}) ([]string, error) {
		if svc, ok := obj.(*corev1.Service); ok {
			return []string{svc.Spec.ClusterIP}, nil
		}
		return []string{""}, fmt.Errorf("object is not a service")
	}})

	k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    iw.addService,
		DeleteFunc: iw.deleteService,
		UpdateFunc: func(before interface{}, after interface{}) {
			iw.deleteService(before)
			iw.addService(after)
		},
	})

	return iw
}

////////////////////////
/// IPWatcher ///
////////////////////////

// Subscribe to an authority.
// The provided listener will be updated each time the address set for the
// given authority is changed.
func (iw *IPWatcher) Subscribe(clusterIP string, port Port, listener EndpointUpdateListener) error {
	iw.log.Infof("Establishing watch on service cluster ip [%s:%d]", clusterIP, port)
	ss := iw.getOrNewServiceSubscriptions(clusterIP)
	ss.subscribe(port, listener)
	return nil
}

// Unsubscribe removes a listener from the subscribers list for this authority.
func (iw *IPWatcher) Unsubscribe(clusterIP string, port Port, listener EndpointUpdateListener) {
	iw.log.Infof("Stopping watch on service cluster ip [%s:%d]", clusterIP, port)
	ss, ok := iw.getServiceSubscriptions(clusterIP)
	if !ok {
		iw.log.Errorf("Cannot unsubscribe from unknown service ip [%s:%d]", clusterIP, port)
		return
	}
	ss.unsubscribe(port, listener)
}

func (iw *IPWatcher) addService(obj interface{}) {
	service := obj.(*corev1.Service)
	if service.Namespace == kubeSystem {
		return
	}

	ss := iw.getOrNewServiceSubscriptions(service.Spec.ClusterIP)

	ss.updateService(service)
}

func (iw *IPWatcher) deleteService(obj interface{}) {
	service := obj.(*corev1.Service)
	if service.Namespace == kubeSystem {
		return
	}

	ss, ok := iw.getServiceSubscriptions(service.Spec.ClusterIP)
	if ok {
		ss.deleteService()
	}
}

// Returns the servicePublisher for the given id if it exists.  Otherwise,
// create a new one and return it.
func (iw *IPWatcher) getOrNewServiceSubscriptions(clusterIP string) *serviceSubscriptions {
	iw.Lock()
	defer iw.Unlock()

	// If the service doesn't yet exist, create a stub for it so the listener can
	// be registered.
	ss, ok := iw.publishers[clusterIP]
	if !ok {
		id := ServiceID{}
		objs, err := iw.k8sAPI.Svc().Informer().GetIndexer().ByIndex(podIPIndex, clusterIP)
		if err != nil {
			if len(objs) > 1 {
				iw.log.Errorf("Service cluster IP conflict: %v, %v", objs[0], objs[1])
			}
			if len(objs) == 1 {
				if svc, ok := objs[0].(*corev1.Service); ok {
					id.Namespace = svc.Namespace
					id.Name = svc.Name
				}
			}
		}
		ss = &serviceSubscriptions{
			id:        id,
			listeners: make(map[EndpointUpdateListener]Port),
			endpoints: iw.endpoints,
			log:       iw.log.WithField("clusterIP", clusterIP),
		}
		iw.publishers[clusterIP] = ss
	}
	return ss
}

func (iw *IPWatcher) getServiceSubscriptions(clusterIP string) (ss *serviceSubscriptions, ok bool) {
	iw.RLock()
	defer iw.RUnlock()
	ss, ok = iw.publishers[clusterIP]
	return
}

////////////////////////
/// serviceSubscriptions ///
////////////////////////

func (ss *serviceSubscriptions) updateService(service *corev1.Service) {
	ss.Lock()
	defer ss.Unlock()

	id := ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	if id != ss.id {
		for listener, port := range ss.listeners {
			ss.endpoints.Unsubscribe(ss.id, port, "", listener)
			ss.endpoints.Subscribe(id, port, "", listener)
		}
		ss.id = id
	}
}

func (ss *serviceSubscriptions) deleteService() {
	ss.Lock()
	defer ss.Unlock()

	for listener, port := range ss.listeners {
		ss.endpoints.Unsubscribe(ss.id, port, "", listener)
	}
	ss.id = ServiceID{}
}

func (ss *serviceSubscriptions) subscribe(port Port, listener EndpointUpdateListener) {
	ss.Lock()
	defer ss.Unlock()

	if (ss.id != ServiceID{}) {
		ss.endpoints.Subscribe(ss.id, port, "", listener)
	}
	ss.listeners[listener] = port
}

func (ss *serviceSubscriptions) unsubscribe(port Port, listener EndpointUpdateListener) {
	ss.Lock()
	defer ss.Unlock()

	if (ss.id != ServiceID{}) {
		ss.endpoints.Unsubscribe(ss.id, port, "", listener)
	}
	delete(ss.listeners, listener)
}
