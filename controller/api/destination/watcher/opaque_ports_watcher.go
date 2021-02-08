package watcher

import (
	"reflect"
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
	// OpaquePortsWatcher watches all the services and their namespaces in the
	// cluster. If the Linkerd opaque ports annotation is added to either
	// service or namespace, the watcher will merge the port value or ranges
	// added—giving priority to service annotations.
	OpaquePortsWatcher struct {
		subscriptions map[string]*nsSubscriptions
		k8sAPI        *k8s.API
		log           *logging.Entry
		sync.RWMutex
	}

	nsSubscriptions struct {
		opaquePorts map[uint32]struct{}
		services    map[ServiceID]*svcSubscriptions
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
// k8sAPI for service and namespace changes.
func NewOpaquePortsWatcher(k8sAPI *k8s.API, log *logging.Entry) *OpaquePortsWatcher {
	opw := &OpaquePortsWatcher{
		subscriptions: make(map[string]*nsSubscriptions),
		k8sAPI:        k8sAPI,
		log:           log.WithField("component", "opaque-ports-watcher"),
	}
	k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    opw.addService,
		DeleteFunc: opw.deleteService,
		UpdateFunc: func(_, obj interface{}) { opw.addService(obj) },
	})
	k8sAPI.NS().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    opw.addNamespace,
		DeleteFunc: opw.deleteNamespace,
		UpdateFunc: func(_, obj interface{}) { opw.addNamespace(obj) },
	})
	return opw
}

// Subscribe subscribes a listener to a service; each time the service or
// namespace changes, the listener will be updated if the list of opaque ports
// changes.
func (opw *OpaquePortsWatcher) Subscribe(id ServiceID, listener OpaquePortsUpdateListener) error {
	opw.Lock()
	defer opw.Unlock()
	svc, _ := opw.k8sAPI.Svc().Lister().Services(id.Namespace).Get(id.Name)
	if svc != nil && svc.Spec.Type == corev1.ServiceTypeExternalName {
		return invalidService(id.String())
	}
	opw.log.Infof("Starting watch on service %s", id)
	ns, ok := opw.subscriptions[id.Namespace]
	// If there is no watched namespace for the service, create a subscription
	// for the namespace qualified service and no opaque ports.
	if !ok {
		opw.subscriptions[id.Namespace] = &nsSubscriptions{
			opaquePorts: make(map[uint32]struct{}),
			services: map[ServiceID]*svcSubscriptions{id: {
				opaquePorts: make(map[uint32]struct{}),
				listeners:   []OpaquePortsUpdateListener{listener},
			}},
		}
		return nil
	}
	ss, ok := ns.services[id]
	// If there is no watched service, create a subscription for the service
	// and no opaque ports.
	if !ok {
		ns.services[id] = &svcSubscriptions{
			opaquePorts: make(map[uint32]struct{}),
			listeners:   []OpaquePortsUpdateListener{listener},
		}
		if len(ns.opaquePorts) != 0 {
			listener.UpdateService(ns.opaquePorts)
		}
		return nil
	}
	// There are subscriptions for this service, so add the listener to the
	// service listeners. If there are opaque ports for the service or the
	// namespace, update the listener with that value.
	ss.listeners = append(ss.listeners, listener)
	op := ss.opaquePorts
	if len(op) == 0 {
		op = ns.opaquePorts
	}
	if len(op) != 0 {
		listener.UpdateService(op)
	}
	return nil
}

// Unsubscribe unsubscries a listener from service.
func (opw *OpaquePortsWatcher) Unsubscribe(id ServiceID, listener OpaquePortsUpdateListener) {
	opw.Lock()
	defer opw.Unlock()
	opw.log.Infof("Stopping watch on service %s", id)
	ns, ok := opw.subscriptions[id.Namespace]
	if !ok {
		opw.log.Errorf("Cannot unsubscribe from service in unknown namespace %s", id.Namespace)
		return
	}
	ss, ok := ns.services[id]
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
	opaquePorts, err := getServiceOpaquePortsAnnotation(svc)
	if err != nil {
		opw.log.Errorf("failed to get %s service opaque ports annotation: %s", id, err)
		return
	}
	// If the service has no opaque ports, we check the namespace. If the
	// namespace does have the service, that means there are listeners waiting
	// for updates; we must update them with the namespace's opaque ports.
	if len(opaquePorts) == 0 {
		ns, ok := opw.subscriptions[id.Namespace]
		// If there are no namespace subscriptions or the namespace has no
		// opaque ports, there are no listeners to update.
		if !ok || len(ns.opaquePorts) == 0 {
			return
		}
		ss, ok := ns.services[id]
		// There are no listeners for this service.
		if !ok {
			return
		}
		for _, listener := range ss.listeners {
			listener.UpdateService(ns.opaquePorts)
		}
		return
	}
	ns, ok := opw.subscriptions[id.Namespace]
	// If there are no namespace subscriptions for the service's namespace,
	// create one and add the service subscription with its opaque ports.
	if !ok {
		opw.subscriptions[id.Namespace] = &nsSubscriptions{
			opaquePorts: make(map[uint32]struct{}),
			services: map[ServiceID]*svcSubscriptions{id: {
				opaquePorts: opaquePorts,
				listeners:   []OpaquePortsUpdateListener{},
			}},
		}
		return
	}
	ss, ok := ns.services[id]
	// If there is service subscription, create one with the opaque ports.
	if !ok {
		ns.services[id] = &svcSubscriptions{
			opaquePorts: opaquePorts,
			listeners:   []OpaquePortsUpdateListener{},
		}
		return
	}
	// Do not send updates if there was no change in the opaque ports; if
	// there was, send an update to each of the listeners.
	if !reflect.DeepEqual(ss.opaquePorts, opaquePorts) {
		ss.opaquePorts = opaquePorts
		for _, listener := range ss.listeners {
			listener.UpdateService(ss.opaquePorts)
		}
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
	ns, ok := opw.subscriptions[id.Namespace]
	if !ok {
		return
	}
	ss, ok := ns.services[id]
	if !ok {
		return
	}
	old := ss.opaquePorts
	ss.opaquePorts = make(map[uint32]struct{})
	// Do not send an update if the service already had no opaque ports
	if len(old) == 0 {
		return
	}
	// Deleting a service does not mean there are no opaque ports; if the
	// namespace has a list, that must be sent instead.
	if len(ns.opaquePorts) != 0 {
		for _, listener := range ss.listeners {
			listener.UpdateService(ns.opaquePorts)
		}
		return
	}
	for _, listener := range ss.listeners {
		listener.UpdateService(make(map[uint32]struct{}))
	}
}

func (opw *OpaquePortsWatcher) addNamespace(obj interface{}) {
	opw.Lock()
	defer opw.Unlock()
	namespace := obj.(*corev1.Namespace)
	opaquePorts, err := getNamespaceOpaquePortsAnnotation(namespace)
	if err != nil {
		opw.log.Errorf("failed to get %s namespaces opaque ports annotation: %s", namespace.Name, err)
		return
	}
	// If there are no opaque ports on the namespaces, there is nothing to do.
	if len(opaquePorts) == 0 {
		return
	}
	ns, ok := opw.subscriptions[namespace.Name]
	// If there are no namespace subscriptions, there are no listeners to
	// update; we do set the opaque ports though.
	if !ok {
		opw.subscriptions[namespace.Name] = &nsSubscriptions{
			opaquePorts: opaquePorts,
			services:    make(map[ServiceID]*svcSubscriptions),
		}
	}
	if ns == nil {
		ns = &nsSubscriptions{}
	}
	ns.opaquePorts = opaquePorts
	// For each service subscribed to in the namespace, send an update with
	// the namespace's opaque ports only if the service does not have its own.
	for _, svc := range ns.services {
		if len(svc.opaquePorts) == 0 {
			for _, listener := range svc.listeners {
				listener.UpdateService(ns.opaquePorts)
			}
		}
	}
}

func (opw *OpaquePortsWatcher) deleteNamespace(obj interface{}) {
	opw.Lock()
	defer opw.Unlock()
	namespace := obj.(*corev1.Namespace)
	ns, ok := opw.subscriptions[namespace.Name]
	// If there are no namespace subscriptions, there are no listeners to
	// update.
	if !ok {
		return
	}
	if ns == nil {
		ns = &nsSubscriptions{}
	}
	old := ns.opaquePorts
	ns.opaquePorts = make(map[uint32]struct{})
	// For each service subscribed to in the namespace, send an update only if
	// the namespace had a list of opaque ports, and the subscribed service
	// has none.
	//
	// Note: At this point if the namespace is being deleted, then the
	// services within that namespace have likely been deleted. In this case,
	// each service will have no opaque ports, but it's still important that
	// the updates are sent. Since the stream remains open, clients must be
	// updated that the service is not an opaque protocol.
	if len(old) == 0 {
		return
	}
	for _, svc := range ns.services {
		if len(svc.opaquePorts) == 0 {
			for _, listener := range svc.listeners {
				listener.UpdateService(ns.opaquePorts)
			}
		}
	}
}

func getNamespaceOpaquePortsAnnotation(ns *corev1.Namespace) (map[uint32]struct{}, error) {
	opaquePorts := make(map[uint32]struct{})
	annotation := ns.GetAnnotations()[labels.ProxyOpaquePortsAnnotation]
	if annotation != "" {
		for _, portStr := range util.ParseOpaquePorts(annotation) {
			port, err := strconv.ParseUint(portStr, 10, 32)
			if err != nil {
				return nil, err
			}
			opaquePorts[uint32(port)] = struct{}{}
		}
	}
	return opaquePorts, nil
}

func getServiceOpaquePortsAnnotation(svc *corev1.Service) (map[uint32]struct{}, error) {
	opaquePorts := make(map[uint32]struct{})
	annotation := svc.Annotations[labels.ProxyOpaquePortsAnnotation]
	if annotation != "" {
		for _, portStr := range parseServiceOpaquePorts(annotation, svc.Spec.Ports) {
			port, err := strconv.ParseUint(portStr, 10, 32)
			if err != nil {
				return nil, err
			}
			opaquePorts[uint32(port)] = struct{}{}
		}
	}
	return opaquePorts, nil
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
