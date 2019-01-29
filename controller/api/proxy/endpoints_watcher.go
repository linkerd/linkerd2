package proxy

import (
	"fmt"
	"strings"
	"sync"

	net "github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/addr"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	kubeSystem       = "kube-system"
	endpointResource = "endpoints"
)

// endpointsWatcher watches all endpoints and services in the Kubernetes
// cluster.  Listeners can subscribe to a particular service and port and
// endpointsWatcher will publish the address set and all future changes for
// that service:port.
type endpointsWatcher struct {
	serviceLister  corelisters.ServiceLister
	endpointLister corelisters.EndpointsLister
	podLister      corelisters.PodLister
	// a map of service -> service port -> servicePort
	servicePorts map[serviceID]map[uint32]*servicePort
	// This mutex protects the servicePorts data structure (nested map) itself
	// and does not protect the servicePort objects themselves.  They are locked
	// separately.
	mutex sync.RWMutex
}

func newEndpointsWatcher(k8sAPI *k8s.API) *endpointsWatcher {
	watcher := &endpointsWatcher{
		serviceLister:  k8sAPI.Svc().Lister(),
		endpointLister: k8sAPI.Endpoint().Lister(),
		podLister:      k8sAPI.Pod().Lister(),
		servicePorts:   make(map[serviceID]map[uint32]*servicePort),
		mutex:          sync.RWMutex{},
	}

	k8sAPI.Svc().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    watcher.addService,
			UpdateFunc: watcher.updateService,
		},
	)

	k8sAPI.Endpoint().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    watcher.addEndpoints,
			UpdateFunc: watcher.updateEndpoints,
			DeleteFunc: watcher.deleteEndpoints,
		},
	)

	return watcher
}

// Close all open streams on shutdown
func (e *endpointsWatcher) stop() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	for _, portMap := range e.servicePorts {
		for _, servicePort := range portMap {
			servicePort.unsubscribeAll()
		}
	}
}

// Subscribe to a service and service port.
// The provided listener will be updated each time the address set for the
// given service port is changed.
func (e *endpointsWatcher) subscribe(service *serviceID, port uint32, listener endpointUpdateListener) error {
	log.Infof("Establishing watch on endpoint %s:%d", service, port)

	svc, err := e.getService(service)
	if err != nil && !apierrors.IsNotFound(err) {
		log.Errorf("Error getting service: %s", err)
		return err
	}

	e.mutex.Lock() // Acquire write-lock on servicePorts data structure.
	defer e.mutex.Unlock()

	svcPorts, ok := e.servicePorts[*service]
	if !ok {
		svcPorts = make(map[uint32]*servicePort)
		e.servicePorts[*service] = svcPorts
	}
	svcPort, ok := svcPorts[port]
	if !ok {
		endpoints, err := e.getEndpoints(service)
		if apierrors.IsNotFound(err) {
			endpoints = &v1.Endpoints{}
		} else if err != nil {
			log.Errorf("Error getting endpoints: %s", err)
			return err
		}
		svcPort = newServicePort(svc, endpoints, port, e.podLister)
		svcPorts[port] = svcPort
	}

	exists := true
	if svc == nil || svc.Spec.Type == v1.ServiceTypeExternalName {
		// XXX: The proxy will use DNS to discover the service if it is told
		// the service doesn't exist. An external service is represented in DNS
		// as a CNAME, which the proxy will correctly resolve. Thus, there's no
		// benefit (yet) to distinguishing between "the service exists but it
		// is an ExternalName service so use DNS anyway" and "the service does
		// not exist."
		exists = false
	}

	svcPort.subscribe(exists, listener)
	return nil
}

func (e *endpointsWatcher) unsubscribe(service *serviceID, port uint32, listener endpointUpdateListener) error {
	log.Infof("Stopping watch on endpoint %s:%d", service, port)

	e.mutex.Lock() // Acquire write-lock on servicePorts data structure.
	defer e.mutex.Unlock()

	svc, ok := e.servicePorts[*service]
	if !ok {
		return fmt.Errorf("Cannot unsubscribe from %s: not subscribed", service)
	}
	svcPort, ok := svc[port]
	if !ok {
		return fmt.Errorf("Cannot unsubscribe from %s: not subscribed", service)
	}
	unsubscribed, numListeners := svcPort.unsubscribe(listener)
	if !unsubscribed {
		return fmt.Errorf("Cannot unsubscribe from %s: not subscribed", service)
	}
	if numListeners == 0 {
		delete(svc, port)
		if len(svc) == 0 {
			delete(e.servicePorts, *service)
		}
	}
	return nil
}

func (e *endpointsWatcher) getService(service *serviceID) (*v1.Service, error) {
	return e.serviceLister.Services(service.namespace).Get(service.name)
}

func (e *endpointsWatcher) addService(obj interface{}) {
	service := obj.(*v1.Service)
	if service.Namespace == kubeSystem {
		return
	}
	id := serviceID{
		namespace: service.Namespace,
		name:      service.Name,
	}

	e.mutex.RLock()
	defer e.mutex.RUnlock()
	svc, ok := e.servicePorts[id]
	if ok {
		for _, sp := range svc {
			sp.updateService(service)
		}
	}
}

func (e *endpointsWatcher) updateService(oldObj, newObj interface{}) {
	service := newObj.(*v1.Service)
	if service.Namespace == kubeSystem {
		return
	}
	id := serviceID{
		namespace: service.Namespace,
		name:      service.Name,
	}

	e.mutex.RLock()
	defer e.mutex.RUnlock()
	svc, ok := e.servicePorts[id]
	if ok {
		for _, sp := range svc {
			sp.updateService(service)
		}
	}
}

func (e *endpointsWatcher) getEndpoints(service *serviceID) (*v1.Endpoints, error) {
	return e.endpointLister.Endpoints(service.namespace).Get(service.name)
}

func (e *endpointsWatcher) addEndpoints(obj interface{}) {
	endpoints := obj.(*v1.Endpoints)
	if endpoints.Namespace == kubeSystem {
		return
	}
	id := serviceID{
		namespace: endpoints.Namespace,
		name:      endpoints.Name,
	}

	e.mutex.RLock()
	defer e.mutex.RUnlock()
	service, ok := e.servicePorts[id]
	if ok {
		for _, sp := range service {
			sp.updateEndpoints(endpoints)
		}
	}
}

func (e *endpointsWatcher) deleteEndpoints(obj interface{}) {
	endpoints := obj.(*v1.Endpoints)
	if endpoints.Namespace == kubeSystem {
		return
	}
	id := serviceID{
		namespace: endpoints.Namespace,
		name:      endpoints.Name,
	}

	e.mutex.RLock()
	defer e.mutex.RUnlock()
	service, ok := e.servicePorts[id]
	if ok {
		for _, sp := range service {
			sp.deleteEndpoints()
		}
	}
}

func (e *endpointsWatcher) updateEndpoints(oldObj, newObj interface{}) {
	e.addEndpoints(newObj)
}

/// servicePort ///

// servicePort represents a service along with a port number.  Multiple
// listeners may be subscribed to a servicePort.  servicePort maintains the
// current state of the address set and publishes diffs to all listeners when
// updates come from either the endpoints API or the service API.
type servicePort struct {
	// these values are immutable properties of the servicePort
	service serviceID
	port    uint32 // service port
	// these values hold the current state of the servicePort and are mutable
	listeners  []endpointUpdateListener
	endpoints  *v1.Endpoints
	targetPort intstr.IntOrString
	addresses  []*updateAddress
	podLister  corelisters.PodLister
	// This mutex protects against concurrent modification of the listeners slice
	// as well as prevents updates for occuring while the listeners slice is being
	// modified.
	mutex sync.Mutex
}

func newServicePort(service *v1.Service, endpoints *v1.Endpoints, port uint32, podLister corelisters.PodLister) *servicePort {
	id := serviceID{}
	if service != nil {
		id.namespace = service.Namespace
		id.name = service.Name
	}

	targetPort := getTargetPort(service, port)

	sp := &servicePort{
		service:    id,
		listeners:  make([]endpointUpdateListener, 0),
		port:       port,
		endpoints:  endpoints,
		targetPort: targetPort,
		podLister:  podLister,
		mutex:      sync.Mutex{},
	}

	sp.addresses = sp.endpointsToAddresses(endpoints, targetPort)

	return sp
}

func (sp *servicePort) updateEndpoints(newEndpoints *v1.Endpoints) {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	sp.updateAddresses(newEndpoints, sp.targetPort)
	sp.endpoints = newEndpoints
}

func (sp *servicePort) deleteEndpoints() {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	log.Debugf("Deleting %s:%d", sp.service, sp.port)

	for _, listener := range sp.listeners {
		listener.NoEndpoints(false)
	}
	sp.endpoints = &v1.Endpoints{}
	sp.addresses = []*updateAddress{}
}

func (sp *servicePort) updateService(newService *v1.Service) {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	newTargetPort := getTargetPort(newService, sp.port)
	if newTargetPort != sp.targetPort {
		sp.updateAddresses(sp.endpoints, newTargetPort)
		sp.targetPort = newTargetPort
	}
}

func (sp *servicePort) updateAddresses(endpoints *v1.Endpoints, port intstr.IntOrString) {
	newAddresses := sp.endpointsToAddresses(endpoints, port)
	if log.GetLevel() >= log.DebugLevel {
		var s []string
		for _, v := range newAddresses {
			s = append(s, fmt.Sprintf("%v", *v))
		}
		log.Debugf("Updating %s:%d to [%v]", sp.service, sp.port, strings.Join(s, ", "))
	}

	if len(newAddresses) == 0 {
		for _, listener := range sp.listeners {
			listener.NoEndpoints(true)
		}
	} else {
		add, remove := diffUpdateAddresses(sp.addresses, newAddresses)
		for _, listener := range sp.listeners {
			listener.Update(add, remove)
		}
	}
	sp.addresses = newAddresses
}

func (sp *servicePort) subscribe(exists bool, listener endpointUpdateListener) {
	log.Debugf("Subscribing %s:%d exists=%t", sp.service, sp.port, exists)

	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	sp.listeners = append(sp.listeners, listener)
	if !exists {
		listener.NoEndpoints(false)
	} else if len(sp.addresses) == 0 {
		listener.NoEndpoints(true)
	} else {
		listener.Update(sp.addresses, nil)
	}
}

// unsubscribe returns true iff the listener was found and removed.
// it also returns the number of listeners remaining after unsubscribing.
func (sp *servicePort) unsubscribe(listener endpointUpdateListener) (bool, int) {
	log.Debugf("Unsubscribing %s:%d", sp.service, sp.port)

	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	for i, item := range sp.listeners {
		if item == listener {
			// delete the item from the slice
			sp.listeners[i] = sp.listeners[len(sp.listeners)-1]
			sp.listeners[len(sp.listeners)-1] = nil
			sp.listeners = sp.listeners[:len(sp.listeners)-1]
			return true, len(sp.listeners)
		}
	}
	return false, len(sp.listeners)
}

func (sp *servicePort) unsubscribeAll() {
	log.Debugf("Unsubscribing %s:%d", sp.service, sp.port)

	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	for _, listener := range sp.listeners {
		listener.Stop()
	}
}

/// helpers ///

func (sp *servicePort) endpointsToAddresses(endpoints *v1.Endpoints, targetPort intstr.IntOrString) []*updateAddress {
	addrs := make([]*updateAddress, 0)

	for _, subset := range endpoints.Subsets {
		var portNum uint32
		switch targetPort.Type {
		case intstr.String:
			for _, port := range subset.Ports {
				if port.Name == targetPort.StrVal {
					portNum = uint32(port.Port)
					break
				}
			}
		case intstr.Int:
			portNum = uint32(targetPort.IntVal)
		}
		if portNum == 0 {
			log.Errorf("Port %v not found", targetPort)
			return addrs
		}

		for _, address := range subset.Addresses {
			target := address.TargetRef
			if target == nil {
				log.Errorf("Target not found for endpoint %v", address)
				continue
			}

			idStr := fmt.Sprintf("%s %s.%s", address.IP, target.Name, target.Namespace)

			ip, err := addr.ParseProxyIPV4(address.IP)
			if err != nil {
				log.Errorf("[%s] not a valid IPV4 address", idStr)
				continue
			}

			pod, err := sp.podLister.Pods(target.Namespace).Get(target.Name)
			if err != nil {
				log.Errorf("[%s] failed to lookup pod: %s", idStr, err)
				continue
			}

			addrs = append(addrs, &updateAddress{
				address: &net.TcpAddress{Ip: ip, Port: portNum},
				pod:     pod,
			})
		}
	}
	return addrs
}

// getTargetPort returns the port specified as an argument if no service is
// present. If the service is present and it has a port spec matching the
// specified port and a target port configured, it returns the name of the
// service's port (not the name of the target pod port), so that it can be
// looked up in the the endpoints API response, which uses service port names.
func getTargetPort(service *v1.Service, port uint32) intstr.IntOrString {
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
