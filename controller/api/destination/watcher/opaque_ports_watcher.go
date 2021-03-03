package watcher

import (
	"strconv"
	"sync"

	"github.com/linkerd/linkerd2-proxy-init/ports"
	"github.com/linkerd/linkerd2/controller/k8s"
	labels "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/util"
	log "github.com/sirupsen/logrus"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

type (
	// OpaquePortsWatcher watches all the services in the cluster. If the
	// opaque ports annotation is added to a service, the watcher will update
	// listeners—if any—subscribed to that service.
	OpaquePortsWatcher struct {
		subscriptions      map[ServiceID]*svcSubscriptions
		k8sAPI             *k8s.API
		log                *logging.Entry
		defaultOpaquePorts map[uint32]struct{}
		sync.RWMutex
	}

	svcSubscriptions struct {
		opaquePorts map[uint32]struct{}
		listeners   []OpaquePortsUpdateListener
	}

	// OpaquePortsUpdateListener is the interface that subscribers must implement.
	OpaquePortsUpdateListener interface {
		UpdateService(ports map[uint32]struct{})
	}
)

// NewOpaquePortsWatcher creates a OpaquePortsWatcher and begins watching for
// k8sAPI for service changes.
func NewOpaquePortsWatcher(k8sAPI *k8s.API, log *logging.Entry, opaquePorts map[uint32]struct{}) *OpaquePortsWatcher {
	opw := &OpaquePortsWatcher{
		subscriptions:      make(map[ServiceID]*svcSubscriptions),
		k8sAPI:             k8sAPI,
		log:                log.WithField("component", "opaque-ports-watcher"),
		defaultOpaquePorts: opaquePorts,
	}
	k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    opw.addService,
		DeleteFunc: opw.deleteService,
		UpdateFunc: func(_, obj interface{}) { opw.addService(obj) },
	})
	return opw
}

// Subscribe subscribes a listener to a service; each time the service
// changes, the listener will be updated if the list of opaque ports
// changes.
func (opw *OpaquePortsWatcher) Subscribe(id ServiceID, listener OpaquePortsUpdateListener) error {
	opw.Lock()
	defer opw.Unlock()
	svc, _ := opw.k8sAPI.Svc().Lister().Services(id.Namespace).Get(id.Name)
	if svc != nil && svc.Spec.Type == corev1.ServiceTypeExternalName {
		return invalidService(id.String())
	}
	opw.log.Infof("Starting watch on service %s", id)
	ss, ok := opw.subscriptions[id]
	// If there is no watched service, create a subscription for the service
	// and no opaque ports
	if !ok {
		opw.subscriptions[id] = &svcSubscriptions{
			opaquePorts: opw.defaultOpaquePorts,
			listeners:   []OpaquePortsUpdateListener{listener},
		}
		return nil
	}
	// There are subscriptions for this service, so add the listener to the
	// service listeners. If there are opaque ports for the service, update
	// the listener with that value.
	ss.listeners = append(ss.listeners, listener)
	listener.UpdateService(ss.opaquePorts)
	return nil
}

// Unsubscribe unsubscries a listener from service.
func (opw *OpaquePortsWatcher) Unsubscribe(id ServiceID, listener OpaquePortsUpdateListener) {
	opw.Lock()
	defer opw.Unlock()
	opw.log.Infof("Stopping watch on service %s", id)
	ss, ok := opw.subscriptions[id]
	if !ok {
		opw.log.Errorf("Cannot unsubscribe from unknown service %s", id)
		return
	}
	for i, l := range ss.listeners {
		if l == listener {
			n := len(ss.listeners)
			ss.listeners[i] = ss.listeners[n-1]
			ss.listeners[n-1] = nil
			ss.listeners = ss.listeners[:n-1]
		}
	}
}

func (opw *OpaquePortsWatcher) addService(obj interface{}) {
	opw.Lock()
	defer opw.Unlock()
	svc := obj.(*corev1.Service)
	if svc.Namespace == kubeSystem {
		return
	}
	id := ServiceID{
		Namespace: svc.Namespace,
		Name:      svc.Name,
	}
	opaquePorts, ok, err := getServiceOpaquePortsAnnotation(svc)
	if err != nil {
		opw.log.Errorf("failed to get %s service opaque ports annotation: %s", id, err)
		return
	}
	// If the opaque ports annotation was not set, then set the service's
	// opaque ports to the default value.
	if !ok {
		opaquePorts = opw.defaultOpaquePorts
	}
	ss, ok := opw.subscriptions[id]
	// If there are no subscriptions for this service, create one with the
	// opaque ports.
	if !ok {
		opw.subscriptions[id] = &svcSubscriptions{
			opaquePorts: opaquePorts,
			listeners:   []OpaquePortsUpdateListener{},
		}
		return
	}
	// Do not send updates if there was no change in the opaque ports; if
	// there was, send an update to each listener.
	if portsEqual(ss.opaquePorts, opaquePorts) {
		return
	}
	ss.opaquePorts = opaquePorts
	for _, listener := range ss.listeners {
		listener.UpdateService(ss.opaquePorts)
	}
}

func (opw *OpaquePortsWatcher) deleteService(obj interface{}) {
	opw.Lock()
	defer opw.Unlock()
	service, ok := obj.(*corev1.Service)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			opw.log.Errorf("could not get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		service, ok = tombstone.Obj.(*corev1.Service)
		if !ok {
			opw.log.Errorf("DeletedFinalStateUnknown contained object that is not a Service %#v", obj)
			return
		}
	}
	if service.Namespace == kubeSystem {
		return
	}
	id := ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}
	ss, ok := opw.subscriptions[id]
	if !ok {
		return
	}
	old := ss.opaquePorts
	ss.opaquePorts = opw.defaultOpaquePorts
	// Do not send an update if the service already had the default opaque ports
	if portsEqual(old, ss.opaquePorts) {
		return
	}
	for _, listener := range ss.listeners {
		listener.UpdateService(ss.opaquePorts)
	}
}

func getServiceOpaquePortsAnnotation(svc *corev1.Service) (map[uint32]struct{}, bool, error) {
	annotation, ok := svc.Annotations[labels.ProxyOpaquePortsAnnotation]
	if !ok {
		return nil, false, nil
	}
	opaquePorts := make(map[uint32]struct{})
	if annotation != "" {
		for _, portStr := range parseServiceOpaquePorts(annotation, svc.Spec.Ports) {
			port, err := strconv.ParseUint(portStr, 10, 32)
			if err != nil {
				return nil, true, err
			}
			opaquePorts[uint32(port)] = struct{}{}
		}
	}
	return opaquePorts, true, nil
}

func parseServiceOpaquePorts(annotation string, sps []corev1.ServicePort) []string {
	portRanges := util.GetPortRanges(annotation)
	var values []string
	for _, portRange := range portRanges {
		pr := portRange.GetPortRange()
		port, named := isNamed(pr, sps)
		if named {
			values = append(values, strconv.Itoa(int(port)))
		} else {
			pr, err := ports.ParsePortRange(pr)
			if err != nil {
				log.Warnf("Invalid port range [%v]: %s", pr, err)
				continue
			}
			for i := pr.LowerBound; i <= pr.UpperBound; i++ {
				values = append(values, strconv.Itoa(i))
			}
		}
	}
	return values
}

// isNamed checks if a port range is actually a service named port (e.g.
// `123-456` is a valid name, but also is a valid range); all port names must
// be checked before making it a list.
func isNamed(pr string, sps []corev1.ServicePort) (int32, bool) {
	for _, sp := range sps {
		if sp.Name == pr {
			return sp.Port, true
		}
	}
	return 0, false
}

func portsEqual(x, y map[uint32]struct{}) bool {
	if len(x) != len(y) {
		return false
	}
	for port := range x {
		_, ok := y[port]
		if !ok {
			return false
		}
	}
	return true
}
