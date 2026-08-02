package watcher

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta3"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
)

const (
	// metrics labels
	service                = "service"
	namespace              = "namespace"
	targetCluster          = "target_cluster"
	targetService          = "target_service"
	targetServiceNamespace = "target_service_namespace"

	opaqueProtocol = "opaque"
)

const endpointTargetRefPod = "Pod"
const endpointTargetRefExternalWorkload = "ExternalWorkload"

type (

	// EndpointsWatcher watches all endpoints and services in the Kubernetes
	// cluster.  Listeners can subscribe to a particular service and port and
	// EndpointsWatcher will publish the address set and all future changes for
	// that service:port.
	EndpointsWatcher struct {
		publishers  map[ServiceID]*servicePublisher
		k8sAPI      *k8s.API
		metadataAPI *k8s.MetadataAPI

		cluster              string
		log                  *logging.Entry
		enableEndpointSlices bool
		enableIPv6           bool
		sync.RWMutex         // This mutex protects modification of the map itself.

		informerHandlers
	}

	// informerHandlers holds a registration handle for each informer handler
	// that has been registered for the EndpointsWatcher. The registration
	// handles are used to re-deregister informer handlers when the
	// EndpointsWatcher stops.
	informerHandlers struct {
		epHandle  cache.ResourceEventHandlerRegistration
		svcHandle cache.ResourceEventHandlerRegistration
		srvHandle cache.ResourceEventHandlerRegistration
	}

	EndpointUpdateListener interface {
		Add(set AddressSet)
		Remove(set AddressSet)
	}

	FilterKey struct {
		EnableEndpointFiltering bool
		NodeName                string
		Hostname                string
	}
)

var endpointsVecs = newEndpointsMetricsVecs()

var undefinedEndpointPort = Port(0)

// NewEndpointsWatcher creates an EndpointsWatcher and begins watching the
// k8sAPI for pod, service, and endpoint changes. An EndpointsWatcher will
// watch on Endpoints or EndpointSlice resources, depending on cluster configuration.
func NewEndpointsWatcher(k8sAPI *k8s.API, metadataAPI *k8s.MetadataAPI, log *logging.Entry, enableEndpointSlices bool, enableIPv6 bool, cluster string) (*EndpointsWatcher, error) {
	ew := &EndpointsWatcher{
		publishers:           make(map[ServiceID]*servicePublisher),
		k8sAPI:               k8sAPI,
		metadataAPI:          metadataAPI,
		enableEndpointSlices: enableEndpointSlices,
		enableIPv6:           enableIPv6,
		cluster:              cluster,
		log: log.WithFields(logging.Fields{
			"component": "endpoints-watcher",
		}),
	}

	var err error
	ew.svcHandle, err = k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ew.addService,
		DeleteFunc: ew.deleteService,
		UpdateFunc: ew.updateService,
	})
	if err != nil {
		return nil, err
	}

	ew.srvHandle, err = k8sAPI.Srv().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ew.addServer,
		DeleteFunc: ew.deleteServer,
		UpdateFunc: ew.updateServer,
	})
	if err != nil {
		return nil, err
	}

	if ew.enableEndpointSlices {
		ew.log.Debugf("Watching EndpointSlice resources")
		ew.epHandle, err = k8sAPI.ES().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    ew.addEndpointSlice,
			DeleteFunc: ew.deleteEndpointSlice,
			UpdateFunc: ew.updateEndpointSlice,
		})
		if err != nil {
			return nil, err
		}

	} else {
		ew.log.Debugf("Watching Endpoints resources")
		ew.epHandle, err = k8sAPI.Endpoint().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    ew.addEndpoints,
			DeleteFunc: ew.deleteEndpoints,
			UpdateFunc: ew.updateEndpoints,
		})
		if err != nil {
			return nil, err
		}
	}
	return ew, nil
}

////////////////////////
/// EndpointsWatcher ///
////////////////////////

// Subscribe to an authority.
// The provided listener will be updated each time the address set for the
// given authority is changed.
func (ew *EndpointsWatcher) Subscribe(id ServiceID, port Port, filterKey FilterKey, listener EndpointUpdateListener) error {
	svc, _ := ew.k8sAPI.Svc().Lister().Services(id.Namespace).Get(id.Name)
	if svc != nil && svc.Spec.Type == corev1.ServiceTypeExternalName {
		return invalidService(id.String())
	}

	if filterKey.Hostname == "" {
		ew.log.Debugf("Establishing watch on endpoint [%s:%d]", id, port)
	} else {
		ew.log.Debugf("Establishing watch on endpoint [%s.%s:%d]", filterKey.Hostname, id, port)
	}

	sp := ew.getOrNewServicePublisher(id)

	return sp.subscribe(port, listener, filterKey)
}

// Unsubscribe removes a listener from the subscribers list for this authority.
func (ew *EndpointsWatcher) Unsubscribe(id ServiceID, port Port, filterKey FilterKey, listener EndpointUpdateListener, withRemove bool) {
	if filterKey.Hostname == "" {
		ew.log.Debugf("Stopping watch on endpoint [%s:%d]", id, port)
	} else {
		ew.log.Debugf("Stopping watch on endpoint [%s.%s:%d]", filterKey.Hostname, id, port)
	}

	sp, ok := ew.getServicePublisher(id)
	if !ok {
		ew.log.Errorf("Cannot unsubscribe from unknown service [%s:%d]", id, port)
		return
	}
	sp.unsubscribe(port, listener, filterKey, withRemove)
}

// removeHandlers will de-register any event handlers used by the
// EndpointsWatcher's informers.
func (ew *EndpointsWatcher) removeHandlers() {
	ew.Lock()
	defer ew.Unlock()
	if ew.svcHandle != nil {
		if err := ew.k8sAPI.Svc().Informer().RemoveEventHandler(ew.svcHandle); err != nil {
			ew.log.Errorf("Failed to remove Service informer event handlers: %s", err)
		}
	}

	if ew.srvHandle != nil {
		if err := ew.k8sAPI.Srv().Informer().RemoveEventHandler(ew.srvHandle); err != nil {
			ew.log.Errorf("Failed to remove Server informer event handlers: %s", err)
		}
	}

	if ew.epHandle != nil {
		if ew.enableEndpointSlices {
			if err := ew.k8sAPI.ES().Informer().RemoveEventHandler(ew.epHandle); err != nil {

				ew.log.Errorf("Failed to remove EndpointSlice informer event handlers: %s", err)
			}
		} else {
			if err := ew.k8sAPI.Endpoint().Informer().RemoveEventHandler(ew.epHandle); err != nil {
				ew.log.Errorf("Failed to remove Endpoints informer event handlers: %s", err)
			}
		}
	}
}

func (ew *EndpointsWatcher) addService(obj interface{}) {
	service := obj.(*corev1.Service)
	id := ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	sp := ew.getOrNewServicePublisher(id)

	sp.updateService(service)
}

func (ew *EndpointsWatcher) updateService(oldObj interface{}, newObj interface{}) {
	oldService := oldObj.(*corev1.Service)
	newService := newObj.(*corev1.Service)

	oldUpdated := latestUpdated(oldService.ManagedFields)
	updated := latestUpdated(newService.ManagedFields)
	if !updated.IsZero() && updated != oldUpdated {
		delta := time.Since(updated)
		serviceInformerLag.Observe(delta.Seconds())
	}

	ew.addService(newObj)
}

func (ew *EndpointsWatcher) deleteService(obj interface{}) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			ew.log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		service, ok = tombstone.Obj.(*corev1.Service)
		if !ok {
			ew.log.Errorf("DeletedFinalStateUnknown contained object that is not a Service %#v", obj)
			return
		}
	}

	id := ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	sp, ok := ew.getServicePublisher(id)
	if ok {
		sp.deleteEndpoints()
	}
}

func (ew *EndpointsWatcher) addEndpoints(obj interface{}) {
	endpoints, ok := obj.(*corev1.Endpoints)
	if !ok {
		ew.log.Errorf("error processing endpoints resource, got %#v expected *corev1.Endpoints", obj)
		return
	}

	id := ServiceID{Namespace: endpoints.Namespace, Name: endpoints.Name}
	sp := ew.getOrNewServicePublisher(id)
	sp.updateEndpoints(endpoints)
}

func (ew *EndpointsWatcher) updateEndpoints(oldObj interface{}, newObj interface{}) {
	oldEndpoints, ok := oldObj.(*corev1.Endpoints)
	if !ok {
		ew.log.Errorf("error processing endpoints resource, got %#v expected *corev1.Endpoints", oldObj)
		return
	}
	newEndpoints, ok := newObj.(*corev1.Endpoints)
	if !ok {
		ew.log.Errorf("error processing endpoints resource, got %#v expected *corev1.Endpoints", newObj)
		return
	}

	oldUpdated := latestUpdated(oldEndpoints.ManagedFields)
	updated := latestUpdated(newEndpoints.ManagedFields)
	if !updated.IsZero() && updated != oldUpdated {
		delta := time.Since(updated)
		endpointsInformerLag.Observe(delta.Seconds())
	}

	id := ServiceID{Namespace: newEndpoints.Namespace, Name: newEndpoints.Name}
	sp := ew.getOrNewServicePublisher(id)
	sp.updateEndpoints(newEndpoints)
}

func (ew *EndpointsWatcher) deleteEndpoints(obj interface{}) {
	endpoints, ok := obj.(*corev1.Endpoints)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			ew.log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		endpoints, ok = tombstone.Obj.(*corev1.Endpoints)
		if !ok {
			ew.log.Errorf("DeletedFinalStateUnknown contained object that is not an Endpoints %#v", obj)
			return
		}
	}

	id := ServiceID{
		Namespace: endpoints.Namespace,
		Name:      endpoints.Name,
	}

	sp, ok := ew.getServicePublisher(id)
	if ok {
		sp.deleteEndpoints()
	}
}

func (ew *EndpointsWatcher) addEndpointSlice(obj interface{}) {
	newSlice, ok := obj.(*discovery.EndpointSlice)
	if !ok {
		ew.log.Errorf("error processing EndpointSlice resource, got %#v expected *discovery.EndpointSlice", obj)
		return
	}

	id, err := getEndpointSliceServiceID(newSlice)
	if err != nil {
		ew.log.Errorf("Could not fetch resource service name:%v", err)
		return
	}

	sp := ew.getOrNewServicePublisher(id)
	sp.addEndpointSlice(newSlice)
}

func (ew *EndpointsWatcher) updateEndpointSlice(oldObj interface{}, newObj interface{}) {
	oldSlice, ok := oldObj.(*discovery.EndpointSlice)
	if !ok {
		ew.log.Errorf("error processing EndpointSlice resource, got %#v expected *discovery.EndpointSlice", oldObj)
		return
	}
	newSlice, ok := newObj.(*discovery.EndpointSlice)
	if !ok {
		ew.log.Errorf("error processing EndpointSlice resource, got %#v expected *discovery.EndpointSlice", newObj)
		return
	}
	oldUpdated := latestUpdated(oldSlice.ManagedFields)
	updated := latestUpdated(newSlice.ManagedFields)
	if !updated.IsZero() && updated != oldUpdated {
		delta := time.Since(updated)
		endpointsliceInformerLag.Observe(delta.Seconds())
	}

	id, err := getEndpointSliceServiceID(newSlice)
	if err != nil {
		ew.log.Errorf("Could not fetch resource service name:%v", err)
		return
	}

	sp, ok := ew.getServicePublisher(id)
	if ok {
		sp.updateEndpointSlice(oldSlice, newSlice)
	}
}

func (ew *EndpointsWatcher) deleteEndpointSlice(obj interface{}) {
	es, ok := obj.(*discovery.EndpointSlice)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			ew.log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
		}
		es, ok = tombstone.Obj.(*discovery.EndpointSlice)
		if !ok {
			ew.log.Errorf("DeletedFinalStateUnknown contained object that is not an EndpointSlice %#v", obj)
			return
		}
	}

	id, err := getEndpointSliceServiceID(es)
	if err != nil {
		ew.log.Errorf("Could not fetch resource service name:%v", err)
	}

	sp, ok := ew.getServicePublisher(id)
	if ok {
		sp.deleteEndpointSlice(es)
	}
}

// Returns the servicePublisher for the given id if it exists.  Otherwise,
// create a new one and return it.
func (ew *EndpointsWatcher) getOrNewServicePublisher(id ServiceID) *servicePublisher {
	ew.Lock()
	defer ew.Unlock()

	// If the service doesn't yet exist, create a stub for it so the listener can
	// be registered.
	sp, ok := ew.publishers[id]
	if !ok {
		sp = &servicePublisher{
			id: id,
			log: ew.log.WithFields(logging.Fields{
				"component": "service-publisher",
				"ns":        id.Namespace,
				"svc":       id.Name,
			}),
			k8sAPI:               ew.k8sAPI,
			metadataAPI:          ew.metadataAPI,
			cluster:              ew.cluster,
			ports:                make(map[Port]*portPublisher),
			enableEndpointSlices: ew.enableEndpointSlices,
			enableIPv6:           ew.enableIPv6,
		}
		ew.publishers[id] = sp
	}
	return sp
}

func (ew *EndpointsWatcher) getServicePublisher(id ServiceID) (sp *servicePublisher, ok bool) {
	ew.RLock()
	defer ew.RUnlock()
	sp, ok = ew.publishers[id]
	return
}

func (ew *EndpointsWatcher) addServer(obj interface{}) {
	ew.Lock()
	defer ew.Unlock()
	server := obj.(*v1beta3.Server)
	for _, sp := range ew.publishers {
		sp.updateServer(nil, server)
	}
}

func (ew *EndpointsWatcher) updateServer(oldObj interface{}, newObj interface{}) {
	ew.Lock()
	defer ew.Unlock()

	oldServer := oldObj.(*v1beta3.Server)
	newServer := newObj.(*v1beta3.Server)
	if oldServer != nil && newServer != nil {
		oldUpdated := latestUpdated(oldServer.ManagedFields)
		updated := latestUpdated(newServer.ManagedFields)
		if !updated.IsZero() && updated != oldUpdated {
			delta := time.Since(updated)
			serverInformerLag.Observe(delta.Seconds())
		}
	}

	namespace := ""
	if oldServer != nil {
		namespace = oldServer.GetNamespace()
	}
	if newServer != nil {
		namespace = newServer.GetNamespace()
	}

	for id, sp := range ew.publishers {
		// Servers may only select workloads in their namespace.
		if id.Namespace == namespace {
			sp.updateServer(oldServer, newServer)
		}
	}
}

func (ew *EndpointsWatcher) deleteServer(obj interface{}) {
	ew.Lock()
	defer ew.Unlock()
	server := obj.(*v1beta3.Server)
	for _, sp := range ew.publishers {
		sp.updateServer(server, nil)
	}
}

////////////
/// util ///
////////////

// getTargetPort returns the port specified as an argument if no service is
// present. If the service is present and it has a port spec matching the
// specified port, it returns the name of the service's port (not the name
// of the target pod port), so that it can be looked up in the endpoints API
// response, which uses service port names.
func getTargetPort(service *corev1.Service, port Port) namedPort {
	// Use the specified port as the target port by default
	targetPort := intstr.FromInt(int(port))

	if service == nil {
		return targetPort
	}

	// If a port spec exists with a port matching the specified port use that
	// port spec's name as the target port
	for _, portSpec := range service.Spec.Ports {
		if portSpec.Port == int32(port) {
			return intstr.FromString(portSpec.Name)
		}
	}

	return targetPort
}

func addressChanged(oldAddress Address, newAddress Address) bool {

	if oldAddress.Identity != newAddress.Identity {
		// in this case the identity could have changed; this can happen when for
		// example a mirrored service is reassigned to a new gateway with a different
		// identity and the service mirroring controller picks that and updates the
		// identity
		return true
	}

	// If the zone hints have changed, then the address has changed
	if len(newAddress.ForZones) != len(oldAddress.ForZones) {
		return true
	}

	// Sort the zone information so that we can compare them accurately
	// We can't use `sort.StringSlice` because these are arrays of structs and not just strings
	sort.Slice(oldAddress.ForZones, func(i, j int) bool {
		return oldAddress.ForZones[i].Name < (oldAddress.ForZones[j].Name)
	})
	sort.Slice(newAddress.ForZones, func(i, j int) bool {
		return newAddress.ForZones[i].Name < (newAddress.ForZones[j].Name)
	})

	// Both old and new addresses have the same number of zones, so we can just compare them directly
	for k := range oldAddress.ForZones {
		if oldAddress.ForZones[k].Name != newAddress.ForZones[k].Name {
			return true
		}
	}

	if oldAddress.OpaqueProtocol != newAddress.OpaqueProtocol {
		return true
	}

	if oldAddress.Pod != nil && newAddress.Pod != nil {
		// if these addresses are owned by pods we can check the resource versions
		return oldAddress.Pod.ResourceVersion != newAddress.Pod.ResourceVersion
	}
	return false
}

func diffAddresses(oldAddresses, newAddresses AddressSet) (add, remove AddressSet) {
	addAddresses := make(map[ID]Address)
	removeAddresses := make(map[ID]Address)
	for id, newAddress := range newAddresses.Addresses {
		if oldAddress, ok := oldAddresses.Addresses[id]; ok {
			if addressChanged(oldAddress, newAddress) {
				addAddresses[id] = newAddress
			}
		} else {
			// this is a new address, we need to add it
			addAddresses[id] = newAddress
		}
	}
	for id, address := range oldAddresses.Addresses {
		if _, ok := newAddresses.Addresses[id]; !ok {
			removeAddresses[id] = address
		}
	}
	add = AddressSet{
		Addresses: addAddresses,
		Labels:    newAddresses.Labels,
	}
	remove = AddressSet{
		Addresses: removeAddresses,
	}
	return add, remove
}

func getEndpointSliceServiceID(es *discovery.EndpointSlice) (ServiceID, error) {
	if !isValidSlice(es) {
		return ServiceID{}, fmt.Errorf("EndpointSlice [%s/%s] is invalid", es.Namespace, es.Name)
	}

	if svc, ok := es.Labels[discovery.LabelServiceName]; ok {
		return ServiceID{Namespace: es.Namespace, Name: svc}, nil
	}

	for _, ref := range es.OwnerReferences {
		if ref.Kind == "Service" && ref.Name != "" {
			return ServiceID{Namespace: es.Namespace, Name: ref.Name}, nil
		}
	}

	return ServiceID{}, fmt.Errorf("EndpointSlice [%s/%s] is invalid", es.Namespace, es.Name)
}

func isValidSlice(es *discovery.EndpointSlice) bool {
	serviceName, ok := es.Labels[discovery.LabelServiceName]
	if !ok && len(es.OwnerReferences) == 0 {
		return false
	} else if len(es.OwnerReferences) == 0 && serviceName == "" {
		return false
	}

	return true
}

// SetToServerProtocol sets the address's OpaqueProtocol field based off any
// Servers that select it and override the expected protocol.
func SetToServerProtocol(k8sAPI *k8s.API, address *Address, log *logging.Entry) error {
	if address.Pod == nil {
		return fmt.Errorf("endpoint not backed by Pod: %s:%d", address.IP, address.Port)
	}
	servers, err := k8sAPI.Srv().Lister().Servers(address.Pod.Namespace).List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list Servers: %w", err)
	}
	for _, server := range servers {
		selector, err := metav1.LabelSelectorAsSelector(server.Spec.PodSelector)
		if err != nil {
			log.Errorf("failed to create Selector: %q", err)
			continue
		}
		if server.Spec.ProxyProtocol == opaqueProtocol && selector.Matches(labels.Set(address.Pod.Labels)) {
			var portMatch bool
			switch server.Spec.Port.Type {
			case intstr.Int:
				if server.Spec.Port.IntVal == int32(address.Port) {
					portMatch = true
				}
			case intstr.String:
				for _, c := range append(address.Pod.Spec.InitContainers, address.Pod.Spec.Containers...) {
					for _, p := range c.Ports {
						if (p.ContainerPort == int32(address.Port) || p.HostPort == int32(address.Port)) &&
							p.Name == server.Spec.Port.StrVal {
							portMatch = true
						}
					}
				}
			default:
				continue
			}
			if portMatch {
				address.OpaqueProtocol = true
				return nil
			}
		}
	}
	return nil
}

// setToServerProtocolExternalWorkload sets the address's OpaqueProtocol field based off any
// Servers that select it and override the expected protocol for ExternalWorkloads.
func SetToServerProtocolExternalWorkload(k8sAPI *k8s.API, address *Address) error {
	if address.ExternalWorkload == nil {
		return fmt.Errorf("endpoint not backed by ExternalWorkload: %s:%d", address.IP, address.Port)
	}
	servers, err := k8sAPI.Srv().Lister().Servers(address.ExternalWorkload.Namespace).List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list Servers: %w", err)
	}
	for _, server := range servers {
		selector, err := metav1.LabelSelectorAsSelector(server.Spec.ExternalWorkloadSelector)
		if err != nil {
			return fmt.Errorf("failed to create Selector: %w", err)
		}
		if server.Spec.ProxyProtocol == opaqueProtocol && selector.Matches(labels.Set(address.ExternalWorkload.Labels)) {
			var portMatch bool
			switch server.Spec.Port.Type {
			case intstr.Int:
				if server.Spec.Port.IntVal == int32(address.Port) {
					portMatch = true
				}
			case intstr.String:
				for _, p := range address.ExternalWorkload.Spec.Ports {
					if p.Port == int32(address.Port) && p.Name == server.Spec.Port.StrVal {
						portMatch = true
					}

				}
			default:
				continue
			}
			if portMatch {
				address.OpaqueProtocol = true
				return nil
			}
		}
	}
	return nil
}

func latestUpdated(managedFields []metav1.ManagedFieldsEntry) time.Time {
	var latest time.Time
	for _, field := range managedFields {
		if field.Time == nil {
			continue
		}
		if field.Operation == metav1.ManagedFieldsOperationUpdate {
			if latest.IsZero() || field.Time.After(latest) {
				latest = field.Time.Time
			}
		}
	}
	return latest
}
