package watcher

import (
	"fmt"
	"sync"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
)

const (
	kubeSystem = "kube-system"
	podIPIndex = "ip"
)

// TODO: prom metrics for all the queues/caches
// https://github.com/linkerd/linkerd2/issues/2204

type (
	// Address represents an individual port on an specific pod.
	Address struct {
		IP        string
		Port      Port
		Pod       *corev1.Pod
		OwnerName string
		OwnerKind string
	}

	// PodSet is a set of pods, indexed by IP.
	PodSet = map[PodID]Address

	// EndpointsWatcher watches all endpoints and services in the Kubernetes
	// cluster.  Listeners can subscribe to a particular service and port and
	// endpointsWatcher will publish the address set and all future changes for
	// that service:port.
	EndpointsWatcher struct {
		publishers   map[ServiceID]*servicePublisher
		publishersMu sync.RWMutex // This mutex protects modifcation of the map itself.
		k8sAPI       *k8s.API

		log *logging.Entry
	}

	// servicePublisher represents a service along with a port number.  Multiple
	// listeners may be subscribed to a servicePublisher.  servicePublisher maintains the
	// current state of the address set and publishes diffs to all listeners when
	// updates come from either the endpoints API or the service API.
	servicePublisher struct {
		id     ServiceID
		log    *logging.Entry
		k8sAPI *k8s.API

		ports map[Port]*portPublisher
		// All access to the servicePublisher and its portPublishers is explicitly synchronized by
		// this mutex.
		mutex sync.Mutex
	}

	portPublisher struct {
		id         ServiceID
		targetPort namedPort
		log        *logging.Entry
		k8sAPI     *k8s.API

		exists    bool
		pods      PodSet
		listeners []EndpointUpdateListener
	}

	// EndpointUpdateListener is the interface that subscribers must implement.
	EndpointUpdateListener interface {
		Add(set PodSet)
		Remove(set PodSet)
		NoEndpoints(exists bool)
	}
)

// NewEndpointsWatcher creates an EndpointsWatcher and begins watching the
// k8sAPI for pod, service, and endpoint changes.
func NewEndpointsWatcher(k8sAPI *k8s.API, log *logging.Entry) *EndpointsWatcher {
	ew := &EndpointsWatcher{
		publishers: make(map[ServiceID]*servicePublisher),
		k8sAPI:     k8sAPI,
		log: log.WithFields(logging.Fields{
			"component": "endpoints-watcher",
		}),
	}

	k8sAPI.Pod().Informer().AddIndexers(cache.Indexers{podIPIndex: func(obj interface{}) ([]string, error) {
		if pod, ok := obj.(*corev1.Pod); ok {
			return []string{pod.Status.PodIP}, nil
		}
		return []string{""}, fmt.Errorf("object is not a pod")
	}})

	k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ew.addService,
		DeleteFunc: ew.deleteService,
		UpdateFunc: func(_, obj interface{}) { ew.addService(obj) },
	})

	k8sAPI.Endpoint().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ew.addEndpoints,
		DeleteFunc: ew.deleteEndpoints,
		UpdateFunc: func(_, obj interface{}) { ew.addEndpoints(obj) },
	})

	return ew
}

////////////////////////
/// EndpointsWatcher ///
////////////////////////

// Subscribe to an authority.
// The provided listener will be updated each time the address set for the
// given authority is changed.
func (ew *EndpointsWatcher) Subscribe(authority string, listener EndpointUpdateListener) error {
	id, port, err := GetServiceAndPort(authority)
	if err != nil {
		return err
	}
	ew.log.Infof("Establishing watch on endpoint [%s:%d]", id, port)

	ew.publishersMu.Lock()
	// If the service doesn't yet exist, create a stub for it so the listener can
	// be registered.
	sp, ok := ew.publishers[id]
	if !ok {
		sp = ew.newServicePublisher(id)
		ew.publishers[id] = sp
	}
	ew.publishersMu.Unlock()

	sp.subscribe(port, listener)
	return nil
}

// Unsubscribe removes a listener from the subscribers list for this authority.
func (ew *EndpointsWatcher) Unsubscribe(authority string, listener EndpointUpdateListener) {
	id, port, err := GetServiceAndPort(authority)
	if err != nil {
		ew.log.Errorf("Invalid service name [%s]", authority)
		return
	}
	ew.log.Infof("Stopping watch on endpoint [%s:%d]", id, port)

	ew.publishersMu.RLock()
	sp, ok := ew.publishers[id]
	ew.publishersMu.RUnlock()
	if !ok {
		ew.log.Errorf("Cannot unsubscribe from unknown service [%s:%d]", id, port)
	}
	sp.unsubscribe(port, listener)
}

func (ew *EndpointsWatcher) addService(obj interface{}) {
	service := obj.(*corev1.Service)
	id := ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	ew.publishersMu.Lock()
	sp, ok := ew.publishers[id]
	if !ok {
		sp = ew.newServicePublisher(id)
		ew.publishers[id] = sp
	}
	ew.publishersMu.Unlock()

	sp.updateService(service)
}

func (ew *EndpointsWatcher) deleteService(obj interface{}) {
	service := obj.(*corev1.Service)
	id := ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	ew.publishersMu.RLock()
	sp, ok := ew.publishers[id]
	ew.publishersMu.RUnlock()
	if ok {
		sp.deleteEndpoints()
	}
}

func (ew *EndpointsWatcher) addEndpoints(obj interface{}) {
	endpoints := obj.(*corev1.Endpoints)
	if endpoints.Namespace == kubeSystem {
		return
	}
	id := ServiceID{
		Namespace: endpoints.Namespace,
		Name:      endpoints.Name,
	}

	ew.publishersMu.Lock()
	sp, ok := ew.publishers[id]
	if !ok {
		sp = ew.newServicePublisher(id)
		ew.publishers[id] = sp

	}
	ew.publishersMu.Unlock()

	sp.updateEndpoints(endpoints)
}

func (ew *EndpointsWatcher) deleteEndpoints(obj interface{}) {
	endpoints := obj.(*corev1.Endpoints)
	if endpoints.Namespace == kubeSystem {
		return
	}
	id := ServiceID{
		Namespace: endpoints.Namespace,
		Name:      endpoints.Name,
	}

	ew.publishersMu.RLock()
	sp, ok := ew.publishers[id]
	ew.publishersMu.RUnlock()
	if ok {
		sp.deleteEndpoints()
	}
}

func (ew *EndpointsWatcher) newServicePublisher(service ServiceID) *servicePublisher {
	return &servicePublisher{
		id: service,
		log: ew.log.WithFields(logging.Fields{
			"component": "service-publisher",
			"ns":        service.Namespace,
			"svc":       service.Name,
		}),
		k8sAPI: ew.k8sAPI,
		ports:  make(map[Port]*portPublisher),
	}
}

////////////////////////
/// servicePublisher ///
////////////////////////

func (sp *servicePublisher) updateEndpoints(newEndpoints *corev1.Endpoints) {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()
	sp.log.Debugf("Updating endpoints for %s", sp.id)

	for _, port := range sp.ports {
		port.updateEndpoints(newEndpoints)
	}
}

func (sp *servicePublisher) deleteEndpoints() {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()
	sp.log.Debugf("Deleting endpoints for %s", sp.id)

	for _, port := range sp.ports {
		port.noEndpoints(false)
	}
}

func (sp *servicePublisher) updateService(newService *corev1.Service) {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()
	sp.log.Debugf("Updating service for %s", sp.id)

	for srcPort, port := range sp.ports {
		newTargetPort := getTargetPort(newService, srcPort)
		if newTargetPort != port.targetPort {
			port.updatePort(newTargetPort)
		}
	}
}

func (sp *servicePublisher) subscribe(srcPort Port, listener EndpointUpdateListener) {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	port, ok := sp.ports[srcPort]
	if !ok {
		port = sp.newPortPublisher(srcPort)
		sp.ports[srcPort] = port
	}
	port.subscribe(listener)
}

// unsubscribe returns true iff the listener was found and removed.
// it also returns the number of listeners remaining after unsubscribing.
func (sp *servicePublisher) unsubscribe(srcPort Port, listener EndpointUpdateListener) {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	port, ok := sp.ports[srcPort]
	if ok {
		port.unsubscribe(listener)
	}
}

func (sp *servicePublisher) newPortPublisher(srcPort Port) *portPublisher {
	targetPort := intstr.FromInt(int(srcPort))
	svc, err := sp.k8sAPI.Svc().Lister().Services(sp.id.Namespace).Get(sp.id.Name)
	exists := false
	if err == nil && svc.Spec.Type != corev1.ServiceTypeExternalName {
		// XXX: The proxy will use DNS to discover the service if it is told
		// the service doesn't exist. An external service is represented in DNS
		// as a CNAME, which the proxy will correctly resolve. Thus, there's no
		// benefit (yet) to distinguishing between "the service exists but it
		// is an ExternalName service so use DNS anyway" and "the service does
		// not exist."
		targetPort = getTargetPort(svc, srcPort)
		exists = true
	}

	port := &portPublisher{
		listeners:  []EndpointUpdateListener{},
		targetPort: targetPort,
		exists:     exists,
		k8sAPI:     sp.k8sAPI,
		log:        sp.log.WithField("port", srcPort),
	}

	endpoints, err := sp.k8sAPI.Endpoint().Lister().Endpoints(sp.id.Namespace).Get(sp.id.Name)
	if err == nil {
		port.updateEndpoints(endpoints)
	}

	return port
}

/////////////////////
/// portPublisher ///
/////////////////////

// Note that portPublishers methods are generally NOT thread-safe.  You should
// hold the parent servicePublisher's mutex before calling methods on a
// portPublisher.

func (pp *portPublisher) updateEndpoints(endpoints *corev1.Endpoints) {
	newPods := pp.endpointsToAddresses(endpoints)
	pp.exists = true
	pp.pods = newPods
	if len(newPods) == 0 {
		for _, listener := range pp.listeners {
			listener.NoEndpoints(true)
		}
	} else {
		add, remove := diffPods(pp.pods, newPods)
		for _, listener := range pp.listeners {
			if len(remove) > 0 {
				listener.Remove(remove)
			}
			if len(add) > 0 {
				listener.Add(add)
			}
		}
	}
}

func (pp *portPublisher) endpointsToAddresses(endpoints *corev1.Endpoints) PodSet {
	pods := make(PodSet)
	for _, subset := range endpoints.Subsets {
		resolvedPort := pp.resolveTargetPort(subset)
		for _, endpoint := range subset.Addresses {
			if endpoint.TargetRef.Kind == "Pod" {
				id := PodID{
					Name:      endpoint.TargetRef.Name,
					Namespace: endpoint.TargetRef.Namespace,
				}
				pod, err := pp.k8sAPI.Pod().Lister().Pods(id.Namespace).Get(id.Name)
				if err != nil {
					pp.log.Errorf("Unable to fetch pod %v: %s", id, err)
					continue
				}
				ownerKind, ownerName := pp.k8sAPI.GetOwnerKindAndName(pod)
				pods[id] = Address{
					IP:        endpoint.IP,
					Port:      resolvedPort,
					Pod:       pod,
					OwnerName: ownerName,
					OwnerKind: ownerKind,
				}
			}
		}
	}
	return pods
}

func (pp *portPublisher) resolveTargetPort(subset corev1.EndpointSubset) Port {
	switch pp.targetPort.Type {
	case intstr.Int:
		return Port(pp.targetPort.IntVal)
	case intstr.String:
		for _, p := range subset.Ports {
			if p.Name == pp.targetPort.StrVal {
				return Port(p.Port)
			}
		}
	}
	return Port(0)
}

func (pp *portPublisher) updatePort(targetPort namedPort) {
	pp.targetPort = targetPort
	endpoints, err := pp.k8sAPI.Endpoint().Lister().Endpoints(pp.id.Namespace).Get(pp.id.Name)
	if err == nil {
		pp.updateEndpoints(endpoints)
	} else {
		pp.log.Errorf("Unable to get endpoints during port update: %s", err)
	}
}

func (pp *portPublisher) noEndpoints(exists bool) {
	pp.exists = exists
	for _, listener := range pp.listeners {
		listener.NoEndpoints(exists)
	}
}

func (pp *portPublisher) subscribe(listener EndpointUpdateListener) {
	if pp.exists {
		if len(pp.pods) > 0 {
			listener.Add(pp.pods)
		} else {
			listener.NoEndpoints(true)
		}
	} else {
		listener.NoEndpoints(false)
	}
	pp.listeners = append(pp.listeners, listener)
}

func (pp *portPublisher) unsubscribe(listener EndpointUpdateListener) {
	for i, e := range pp.listeners {
		if e == listener {
			n := len(pp.listeners)
			pp.listeners[i] = pp.listeners[n-1]
			pp.listeners[n-1] = nil
			pp.listeners = pp.listeners[:n-1]
			return
		}
	}
}

////////////
/// util ///
////////////

// getTargetPort returns the port specified as an argument if no service is
// present. If the service is present and it has a port spec matching the
// specified port and a target port configured, it returns the name of the
// service's port (not the name of the target pod port), so that it can be
// looked up in the the endpoints API response, which uses service port names.
func getTargetPort(service *corev1.Service, port Port) namedPort {
	// Use the specified port as the target port by default
	targetPort := intstr.FromInt(int(port))

	if service == nil {
		return targetPort
	}

	// If a port spec exists with a port matching the specified port and a target
	// port configured, use that port spec's name as the target port
	for _, portSpec := range service.Spec.Ports {
		if portSpec.Port == int32(port) && portSpec.TargetPort != intstr.FromInt(0) {
			return intstr.FromString(portSpec.Name)
		}
	}

	return targetPort
}

func diffPods(old, new PodSet) (add, remove PodSet) {
	// TODO: this detects pods which have been added or removed, but does not
	// detect pods which have been modified.  A modified pod should trigger
	// an add of the new version.
	add = make(PodSet)
	remove = make(PodSet)
	for id, pod := range new {
		if _, ok := old[id]; !ok {
			add[id] = pod
		}
	}
	for id, pod := range old {
		if _, ok := new[id]; !ok {
			remove[id] = pod
		}
	}
	return
}
