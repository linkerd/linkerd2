package k8s

import (
	"fmt"
	"sync"
	"time"

	common "github.com/runconduit/conduit/controller/gen/common"
	"github.com/runconduit/conduit/controller/util"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/fields"
	"k8s.io/client-go/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"

	log "github.com/sirupsen/logrus"
)

const kubeSystem = "kube-system"

type EndpointsListener interface {
	Update(add []common.TcpAddress, remove []common.TcpAddress)
}

/// EndpointsWatcher ///

type EndpointsWatcher struct {
	endpointInformer informer
	serviceInformer  informer
	// a map of service -> service port -> servicePort
	servicePorts *map[string]*map[uint32]*servicePort
	// This mutex protects the servicePorts data structure (nested map) itself
	// and does not protect the servicePort objects themselves.  They are locked
	// seperately.
	mutex *sync.RWMutex
}

// An EndpointsWatcher watches all endpoints and services in the Kubernetes
// cluster.  Listeners can subscribe to a particular service and port and
// EndpointsWatcher will publish the address set and all future changes for
// that service:port.
func NewEndpointsWatcher(clientset *kubernetes.Clientset) *EndpointsWatcher {

	servicePorts := make(map[string]*map[uint32]*servicePort)
	mutex := sync.RWMutex{}

	return &EndpointsWatcher{
		endpointInformer: newEndpointInformer(clientset, &servicePorts, &mutex),
		serviceInformer:  newServiceInformer(clientset, &servicePorts, &mutex),
		servicePorts:     &servicePorts,
		mutex:            &mutex,
	}
}

func (e *EndpointsWatcher) Run() {
	e.endpointInformer.run()
	e.serviceInformer.run()
}

func (e *EndpointsWatcher) Stop() {
	e.endpointInformer.stop()
	e.serviceInformer.stop()
}

// Subscribe to a service and service port.
// The provided listener will be updated each time the address set for the
// given service port is changed.
func (e *EndpointsWatcher) Subscribe(service string, port uint32, listener EndpointsListener) error {

	log.Printf("Establishing watch on endpoint %s:%d", service, port)

	e.mutex.Lock() // Acquire write-lock on servicePorts data structure.
	defer e.mutex.Unlock()

	svc, ok := (*e.servicePorts)[service]
	if !ok {
		ports := make(map[uint32]*servicePort)
		(*e.servicePorts)[service] = &ports
		svc = &ports
	}
	svcPort, ok := (*svc)[port]
	if !ok {
		var err error
		svcPort, err = newServicePort(service, port, e)
		if err != nil {
			return err
		}
		(*svc)[port] = svcPort
	}

	svcPort.subscribe(listener)
	return nil
}

func (e *EndpointsWatcher) Unsubscribe(service string, port uint32, listener EndpointsListener) error {

	log.Printf("Stopping watch on endpoint %s:%d", service, port)

	e.mutex.Lock() // Acquire write-lock on servicePorts data structure.
	defer e.mutex.Unlock()

	svc, ok := (*e.servicePorts)[service]
	if !ok {
		return fmt.Errorf("Cannot unsubscribe from %s: not subscribed", service)
	}
	svcPort, ok := (*svc)[port]
	if !ok {
		return fmt.Errorf("Cannot unsubscribe from %s: not subscribed", service)
	}
	if !svcPort.unsubscribe(listener) {
		return fmt.Errorf("Cannot unsubscribe from %s: not subscribed", service)
	}
	return nil
}

/// informer ///

// Watches a Kubernetes resource type
type informer struct {
	informer *cache.Controller
	store    *cache.Store
	stopCh   chan struct{}
}

func (i *informer) run() {
	go i.informer.Run(i.stopCh)
}

func (i *informer) stop() {
	i.stopCh <- struct{}{}
}

func newEndpointInformer(clientset *kubernetes.Clientset, servicePorts *map[string]*map[uint32]*servicePort, mutex *sync.RWMutex) informer {

	endpointsListWatcher := cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "Endpoints", v1.NamespaceAll, fields.Everything())

	store, inf := cache.NewInformer(
		endpointsListWatcher,
		&v1.Endpoints{},
		time.Duration(0),
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				endpoints := obj.(*v1.Endpoints)
				if endpoints.Namespace == kubeSystem {
					return
				}
				id := endpoints.Namespace + "/" + endpoints.Name

				mutex.RLock()
				service, ok := (*servicePorts)[id]
				if ok {
					for _, sp := range *service {
						sp.updateEndpoints(endpoints)
					}
				}
				mutex.RUnlock()
			},
			DeleteFunc: func(obj interface{}) {
				endpoints := obj.(*v1.Endpoints)
				if endpoints.Namespace == kubeSystem {
					return
				}
				id := endpoints.Namespace + "/" + endpoints.Name

				mutex.RLock()
				service, ok := (*servicePorts)[id]
				if ok {
					for _, sp := range *service {
						sp.deleteEndpoints()
					}
				}
				mutex.RUnlock()
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				endpoints := newObj.(*v1.Endpoints)
				if endpoints.Namespace == kubeSystem {
					return
				}
				id := endpoints.Namespace + "/" + endpoints.Name

				mutex.RLock()
				service, ok := (*servicePorts)[id]
				if ok {
					for _, sp := range *service {
						sp.updateEndpoints(endpoints)
					}
				}
				mutex.RUnlock()
			},
		},
	)

	stopCh := make(chan struct{})

	return informer{
		informer: inf,
		store:    &store,
		stopCh:   stopCh,
	}
}

func newServiceInformer(clientset *kubernetes.Clientset, servicePorts *map[string]*map[uint32]*servicePort, mutex *sync.RWMutex) informer {

	serviceListWatcher := cache.NewListWatchFromClient(clientset.CoreV1().RESTClient(), "services", v1.NamespaceAll, fields.Everything())

	store, inf := cache.NewInformer(
		serviceListWatcher,
		&v1.Service{},
		time.Duration(0),
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				service := obj.(*v1.Service)
				if service.Namespace == kubeSystem {
					return
				}
				id := service.Namespace + "/" + service.Name

				mutex.RLock()
				svc, ok := (*servicePorts)[id]
				if ok {
					for _, sp := range *svc {
						sp.updateService(service)
					}
				}
				mutex.RUnlock()
			},
			DeleteFunc: func(obj interface{}) {
				service := obj.(*v1.Service)
				if service.Namespace == kubeSystem {
					return
				}
				id := service.Namespace + "/" + service.Name

				mutex.RLock()
				svc, ok := (*servicePorts)[id]
				if ok {
					for _, sp := range *svc {
						sp.deleteService()
					}
				}
				mutex.RUnlock()
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				service := newObj.(*v1.Service)
				if service.Namespace == kubeSystem {
					return
				}
				id := service.Namespace + "/" + service.Name

				mutex.RLock()
				svc, ok := (*servicePorts)[id]
				if ok {
					for _, sp := range *svc {
						sp.updateService(service)
					}
				}
				mutex.RUnlock()
			},
		},
	)

	stopCh := make(chan struct{})

	return informer{
		informer: inf,
		store:    &store,
		stopCh:   stopCh,
	}
}

/// servicePort ///

// servicePort represents a service along with a port number.  Multiple
// listeners may be subscribed to a servicePort.  servicePort maintains the
// current state of the address set and publishes diffs to all listeners when
// updates come from either the endpoints API or the service API.
type servicePort struct {
	// these values are immutable properties of the servicePort
	service string
	port    uint32 // service port
	// these values hold the current state of the servicePort and are mutable
	listeners  []EndpointsListener
	endpoints  *v1.Endpoints
	targetPort intstr.IntOrString
	addresses  []common.TcpAddress
	// This mutex protects against concurrent modification of the listeners slice
	// as well as prevents updates for occuring while the listeners slice is being
	// modified.
	mutex sync.Mutex
}

func newServicePort(service string, port uint32, e *EndpointsWatcher) (*servicePort, error) {
	endpoints := &v1.Endpoints{}
	obj, exists, err := (*e.endpointInformer.store).GetByKey(service)
	if err != nil {
		return nil, err
	}
	if exists {
		endpoints = obj.(*v1.Endpoints)
	}

	// Use the service port as the target port by default.
	targetPort := intstr.FromInt(int(port))
	obj, exists, err = (*e.serviceInformer.store).GetByKey(service)
	if err != nil {
		return nil, err
	}
	if exists {
		// If a port spec exists with a matching service port, use that port spec's
		// target port.
		for _, portSpec := range obj.(*v1.Service).Spec.Ports {
			if portSpec.Port == int32(port) && portSpec.TargetPort != intstr.FromInt(0) {
				targetPort = portSpec.TargetPort
				break
			}
		}
	}
	addrs := addresses(endpoints, targetPort)

	return &servicePort{
		service:    service,
		listeners:  make([]EndpointsListener, 0),
		port:       port,
		endpoints:  endpoints,
		targetPort: targetPort,
		addresses:  addrs,
		mutex:      sync.Mutex{},
	}, nil
}

func (sp *servicePort) updateEndpoints(newEndpoints *v1.Endpoints) {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	newAddresses := addresses(newEndpoints, sp.targetPort)

	log.Debugf("Updating %s:%d to %s", sp.service, sp.port, util.AddressesToString(newAddresses))

	add, remove := diff(sp.addresses, newAddresses)
	for _, listener := range sp.listeners {
		listener.Update(add, remove)
	}

	sp.endpoints = newEndpoints
	sp.addresses = newAddresses
}

func (sp *servicePort) deleteEndpoints() {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	log.Debugf("Deleting %s:%d", sp.service, sp.port)

	for _, listener := range sp.listeners {
		listener.Update(nil, sp.addresses)
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

		log.Debugf("Updating %s:%d to %s", sp.service, sp.port, util.AddressesToString(newAddresses))

		add, remove := diff(sp.addresses, newAddresses)
		for _, listener := range sp.listeners {
			listener.Update(add, remove)
		}
		sp.targetPort = newTargetPort
		sp.addresses = newAddresses
	}
}

func (sp *servicePort) deleteService() {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	newTargetPort := intstr.FromInt(int(sp.port))
	if newTargetPort != sp.targetPort {
		newAddresses := addresses(sp.endpoints, newTargetPort)

		log.Debugf("Updating %s:%d to %s", sp.service, sp.port, util.AddressesToString(newAddresses))

		add, remove := diff(sp.addresses, newAddresses)
		for _, listener := range sp.listeners {
			listener.Update(add, remove)
		}
		sp.targetPort = newTargetPort
		sp.addresses = newAddresses
	}
}

func (sp *servicePort) subscribe(listener EndpointsListener) {
	sp.mutex.Lock()
	defer sp.mutex.Unlock()

	sp.listeners = append(sp.listeners, listener)
	listener.Update(sp.addresses, nil)
}

// true iff the listener was found and removed
func (sp *servicePort) unsubscribe(listener EndpointsListener) bool {
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

func diff(old []common.TcpAddress, new []common.TcpAddress) ([]common.TcpAddress, []common.TcpAddress) {
	addSet := make(map[string]common.TcpAddress)
	removeSet := make(map[string]common.TcpAddress)
	for _, addr := range new {
		addSet[util.AddressToString(&addr)] = addr
	}
	for _, addr := range old {
		delete(addSet, util.AddressToString(&addr))
		removeSet[util.AddressToString(&addr)] = addr
	}
	for _, addr := range new {
		delete(removeSet, util.AddressToString(&addr))
	}
	add := make([]common.TcpAddress, 0)
	for _, addr := range addSet {
		add = append(add, addr)
	}
	remove := make([]common.TcpAddress, 0)
	for _, addr := range removeSet {
		remove = append(remove, addr)
	}
	return add, remove
}
