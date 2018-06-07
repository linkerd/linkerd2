package destination

import (
	"fmt"
	"sync"

	common "github.com/runconduit/conduit/controller/gen/common"
	"github.com/runconduit/conduit/controller/k8s"
	"github.com/runconduit/conduit/controller/util"
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
	// a map of service -> service port -> servicePort
	servicePorts map[serviceId]map[uint32]*servicePort
	// This mutex protects the servicePorts data structure (nested map) itself
	// and does not protect the servicePort objects themselves.  They are locked
	// separately.
	mutex sync.RWMutex
}

func newEndpointsWatcher(k8sAPI *k8s.API) *endpointsWatcher {
	watcher := &endpointsWatcher{
		serviceLister:  k8sAPI.Svc.Lister(),
		endpointLister: k8sAPI.Endpoint.Lister(),
		servicePorts:   make(map[serviceId]map[uint32]*servicePort),
		mutex:          sync.RWMutex{},
	}

	k8sAPI.Svc.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    watcher.addService,
			UpdateFunc: watcher.updateService,
		},
	)

	k8sAPI.Endpoint.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    watcher.addEndpoints,
			UpdateFunc: watcher.updateEndpoints,
			DeleteFunc: watcher.deleteEndpoints,
		},
	)

	return watcher
}

// TODO: this method should close all open streams
// https://github.com/runconduit/conduit/issues/644
func (e *endpointsWatcher) stop() {}

// Subscribe to a service and service port.
// The provided listener will be updated each time the address set for the
// given service port is changed.
func (e *endpointsWatcher) subscribe(service *serviceId, port uint32, listener updateListener) error {
	log.Printf("Establishing watch on endpoint %s:%d", service, port)

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
		svcPort = newServicePort(svc, endpoints, port)
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

func (e *endpointsWatcher) unsubscribe(service *serviceId, port uint32, listener updateListener) error {
	log.Printf("Stopping watch on endpoint %s:%d", service, port)

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
	if !svcPort.unsubscribe(listener) {
		return fmt.Errorf("Cannot unsubscribe from %s: not subscribed", service)
	}
	return nil
}

func (e *endpointsWatcher) getService(service *serviceId) (*v1.Service, error) {
	return e.serviceLister.Services(service.namespace).Get(service.name)
}

func (e *endpointsWatcher) addService(obj interface{}) {
	service := obj.(*v1.Service)
	if service.Namespace == kubeSystem {
		return
	}
	id := serviceId{
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
	id := serviceId{
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

func (e *endpointsWatcher) getEndpoints(service *serviceId) (*v1.Endpoints, error) {
	return e.endpointLister.Endpoints(service.namespace).Get(service.name)
}

func (e *endpointsWatcher) addEndpoints(obj interface{}) {
	endpoints := obj.(*v1.Endpoints)
	if endpoints.Namespace == kubeSystem {
		return
	}
	id := serviceId{
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
	id := serviceId{
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
	endpoints := newObj.(*v1.Endpoints)
	if endpoints.Namespace == kubeSystem {
		return
	}
	id := serviceId{
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

/// servicePort ///

// servicePort represents a service along with a port number.  Multiple
// listeners may be subscribed to a servicePort.  servicePort maintains the
// current state of the address set and publishes diffs to all listeners when
// updates come from either the endpoints API or the service API.
type servicePort struct {
	// these values are immutable properties of the servicePort
	service serviceId
	port    uint32 // service port
	// these values hold the current state of the servicePort and are mutable
	listeners  []updateListener
	endpoints  *v1.Endpoints
	targetPort intstr.IntOrString
	addresses  []common.TcpAddress
	// This mutex protects against concurrent modification of the listeners slice
	// as well as prevents updates for occuring while the listeners slice is being
	// modified.
	mutex sync.Mutex
}

func newServicePort(service *v1.Service, endpoints *v1.Endpoints, port uint32) *servicePort {
	// Use the service port as the target port by default.
	targetPort := intstr.FromInt(int(port))

	id := serviceId{}

	if service != nil {
		id.namespace = service.Namespace
		id.name = service.Name
		// If a port spec exists with a matching service port, use that port spec's
		// target port.
		for _, portSpec := range service.Spec.Ports {
			if portSpec.Port == int32(port) && portSpec.TargetPort != intstr.FromInt(0) {
				targetPort = portSpec.TargetPort
				break
			}
		}
	}

	addrs := addresses(endpoints, targetPort)

	return &servicePort{
		service:    id,
		listeners:  make([]updateListener, 0),
		port:       port,
		endpoints:  endpoints,
		targetPort: targetPort,
		addresses:  addrs,
		mutex:      sync.Mutex{},
	}
}

func (sp *servicePort) updateEndpoints(newEndpoints *v1.Endpoints) {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	newAddresses := addresses(newEndpoints, sp.targetPort)
	sp.updateAddresses(newAddresses)
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
	sp.addresses = []common.TcpAddress{}
}

func (sp *servicePort) updateService(newService *v1.Service) {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	// Use the service port as the target port by default.
	newTargetPort := intstr.FromInt(int(sp.port))
	// If a port spec exists with a matching service port, use that port spec's
	// target port.
	for _, portSpec := range newService.Spec.Ports {
		if portSpec.Port == int32(sp.port) && portSpec.TargetPort != intstr.FromInt(0) {
			newTargetPort = portSpec.TargetPort
			break
		}
	}
	if newTargetPort != sp.targetPort {
		newAddresses := addresses(sp.endpoints, newTargetPort)
		sp.updateAddresses(newAddresses)
		sp.targetPort = newTargetPort
	}
}

func (sp *servicePort) updateAddresses(newAddresses []common.TcpAddress) {
	log.Debugf("Updating %s:%d to %s", sp.service, sp.port, util.AddressesToString(newAddresses))

	if len(newAddresses) == 0 {
		for _, listener := range sp.listeners {
			listener.NoEndpoints(true)
		}
	} else {
		add, remove := util.DiffAddresses(sp.addresses, newAddresses)
		for _, listener := range sp.listeners {
			listener.Update(add, remove)
		}
	}
	sp.addresses = newAddresses
}

func (sp *servicePort) subscribe(exists bool, listener updateListener) {
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

// true iff the listener was found and removed
func (sp *servicePort) unsubscribe(listener updateListener) bool {
	log.Debugf("Unsubscribing %s:%d", sp.service, sp.port)

	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	for i, item := range sp.listeners {
		if item == listener {
			// delete the item from the slice
			sp.listeners[i] = sp.listeners[len(sp.listeners)-1]
			sp.listeners[len(sp.listeners)-1] = nil
			sp.listeners = sp.listeners[:len(sp.listeners)-1]
			return true
		}
	}
	return false
}

/// helpers ///

func addresses(endpoints *v1.Endpoints, port intstr.IntOrString) []common.TcpAddress {
	ips := make([]common.IPAddress, 0)
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			ip, err := util.ParseIPV4(address.IP)
			if err != nil {
				log.Printf("%s is not a valid IP address", address.IP)
				continue
			}
			ips = append(ips, *ip)
		}
	}

	var portNum uint32
	if port.Type == intstr.String {
	outer:
		for _, subset := range endpoints.Subsets {
			for _, p := range subset.Ports {
				if p.Name == port.StrVal {
					portNum = uint32(p.Port)
					break outer
				}
			}
		}
		if portNum == 0 {
			log.Printf("Port %s not found", port.StrVal)
			return []common.TcpAddress{}
		}
	} else if port.Type == intstr.Int {
		portNum = uint32(port.IntVal)
	}

	addrs := make([]common.TcpAddress, len(ips))
	for i := range ips {
		addrs[i] = common.TcpAddress{
			Ip:   &ips[i],
			Port: portNum,
		}
	}
	return addrs
}
