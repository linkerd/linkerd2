package watcher

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	ewv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	"github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta3"
	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	// Address represents an individual port on a specific endpoint.
	// This endpoint might be the result of a the existence of a pod
	// that is targeted by this service; alternatively it can be the
	// case that this endpoint is not associated with a pod and maps
	// to some other IP (i.e. a remote service gateway)
	Address struct {
		IP                string
		Port              Port
		Pod               *corev1.Pod
		ExternalWorkload  *ewv1beta1.ExternalWorkload
		OwnerName         string
		OwnerKind         string
		Identity          string
		AuthorityOverride string
		Zone              *string
		ForZones          []discovery.ForZone
		OpaqueProtocol    bool
	}

	// AddressSet is a set of Address, indexed by ID.
	// The ID can be either:
	// 1) A reference to service: id.Name contains both the service name and
	// the target IP and port (see newServiceRefAddress)
	// 2) A reference to a pod: id.Name refers to the pod's name, and
	// id.IPFamily refers to the ES AddressType (see newPodRefAddress).
	// 3) A reference to an ExternalWorkload: id.Name refers to the EW's name.
	AddressSet struct {
		Addresses          map[ID]Address
		Labels             map[string]string
		LocalTrafficPolicy bool
	}

	portAndHostname struct {
		port     Port
		hostname string
	}

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

	// servicePublisher represents a service.  It keeps a map of portPublishers
	// keyed by port and hostname.  This is because each watch on a service
	// will have a port and optionally may specify a hostname.  The port
	// and hostname will influence the endpoint set which is why a separate
	// portPublisher is required for each port and hostname combination.  The
	// service's port mapping will be applied to the requested port and the
	// mapped port will be used in the addresses set.  If a hostname is
	// requested, the address set will be filtered to only include addresses
	// with the requested hostname.
	servicePublisher struct {
		id                   ServiceID
		log                  *logging.Entry
		k8sAPI               *k8s.API
		metadataAPI          *k8s.MetadataAPI
		enableEndpointSlices bool
		localTrafficPolicy   bool
		cluster              string
		ports                map[portAndHostname]*portPublisher
		// All access to the servicePublisher and its portPublishers is explicitly synchronized by
		// this mutex.
		sync.Mutex
	}

	// portPublisher represents a service along with a port and optionally a
	// hostname.  Multiple listeners may be subscribed to a portPublisher.
	// portPublisher maintains the current state of the address set and
	// publishes diffs to all listeners when updates come from either the
	// endpoints API or the service API.
	portPublisher struct {
		id                   ServiceID
		targetPort           namedPort
		srcPort              Port
		hostname             string
		log                  *logging.Entry
		k8sAPI               *k8s.API
		metadataAPI          *k8s.MetadataAPI
		enableEndpointSlices bool
		exists               bool
		addresses            AddressSet
		listeners            []EndpointUpdateListener
		metrics              endpointsMetrics
		localTrafficPolicy   bool
	}

	// EndpointUpdateListener is the interface that subscribers must implement.
	EndpointUpdateListener interface {
		Add(set AddressSet)
		Remove(set AddressSet)
		NoEndpoints(exists bool)
	}
)

var endpointsVecs = newEndpointsMetricsVecs()

var undefinedEndpointPort = Port(0)

// shallowCopy returns a shallow copy of addr, in the sense that the Pod and
// ExternalWorkload fields of the Addresses map values still point to the
// locations of the original variable
func (addr AddressSet) shallowCopy() AddressSet {
	addresses := make(map[ID]Address)
	for k, v := range addr.Addresses {
		addresses[k] = v
	}

	labels := make(map[string]string)
	for k, v := range addr.Labels {
		labels[k] = v
	}

	return AddressSet{
		Addresses:          addresses,
		Labels:             labels,
		LocalTrafficPolicy: addr.LocalTrafficPolicy,
	}
}

// NewEndpointsWatcher creates an EndpointsWatcher and begins watching the
// k8sAPI for pod, service, and endpoint changes. An EndpointsWatcher will
// watch on Endpoints or EndpointSlice resources, depending on cluster configuration.
func NewEndpointsWatcher(k8sAPI *k8s.API, metadataAPI *k8s.MetadataAPI, log *logging.Entry, enableEndpointSlices bool, cluster string) (*EndpointsWatcher, error) {
	ew := &EndpointsWatcher{
		publishers:           make(map[ServiceID]*servicePublisher),
		k8sAPI:               k8sAPI,
		metadataAPI:          metadataAPI,
		enableEndpointSlices: enableEndpointSlices,
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
func (ew *EndpointsWatcher) Subscribe(id ServiceID, port Port, hostname string, listener EndpointUpdateListener) error {
	svc, _ := ew.k8sAPI.Svc().Lister().Services(id.Namespace).Get(id.Name)
	if svc != nil && svc.Spec.Type == corev1.ServiceTypeExternalName {
		return invalidService(id.String())
	}

	if hostname == "" {
		ew.log.Debugf("Establishing watch on endpoint [%s:%d]", id, port)
	} else {
		ew.log.Debugf("Establishing watch on endpoint [%s.%s:%d]", hostname, id, port)
	}

	sp := ew.getOrNewServicePublisher(id)

	sp.subscribe(port, hostname, listener)
	return nil
}

// Unsubscribe removes a listener from the subscribers list for this authority.
func (ew *EndpointsWatcher) Unsubscribe(id ServiceID, port Port, hostname string, listener EndpointUpdateListener) {
	if hostname == "" {
		ew.log.Debugf("Stopping watch on endpoint [%s:%d]", id, port)
	} else {
		ew.log.Debugf("Stopping watch on endpoint [%s.%s:%d]", hostname, id, port)
	}

	sp, ok := ew.getServicePublisher(id)
	if !ok {
		ew.log.Errorf("Cannot unsubscribe from unknown service [%s:%d]", id, port)
		return
	}
	sp.unsubscribe(port, hostname, listener)
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
			ports:                make(map[portAndHostname]*portPublisher),
			enableEndpointSlices: ew.enableEndpointSlices,
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

////////////////////////
/// servicePublisher ///
////////////////////////

func (sp *servicePublisher) updateEndpoints(newEndpoints *corev1.Endpoints) {
	sp.Lock()
	defer sp.Unlock()
	sp.log.Debugf("Updating endpoints for %s", sp.id)
	for _, port := range sp.ports {
		port.updateEndpoints(newEndpoints)
	}
}

func (sp *servicePublisher) deleteEndpoints() {
	sp.Lock()
	defer sp.Unlock()
	sp.log.Debugf("Deleting endpoints for %s", sp.id)
	for _, port := range sp.ports {
		port.noEndpoints(false)
	}
}

func (sp *servicePublisher) addEndpointSlice(newSlice *discovery.EndpointSlice) {
	sp.Lock()
	defer sp.Unlock()

	sp.log.Debugf("Adding ES %s/%s", newSlice.Namespace, newSlice.Name)
	for _, port := range sp.ports {
		port.addEndpointSlice(newSlice)
	}
}

func (sp *servicePublisher) updateEndpointSlice(oldSlice *discovery.EndpointSlice, newSlice *discovery.EndpointSlice) {
	sp.Lock()
	defer sp.Unlock()

	sp.log.Debugf("Updating ES %s/%s", oldSlice.Namespace, oldSlice.Name)
	for _, port := range sp.ports {
		port.updateEndpointSlice(oldSlice, newSlice)
	}
}

func (sp *servicePublisher) deleteEndpointSlice(es *discovery.EndpointSlice) {
	sp.Lock()
	defer sp.Unlock()

	sp.log.Debugf("Deleting ES %s/%s", es.Namespace, es.Name)
	for _, port := range sp.ports {
		port.deleteEndpointSlice(es)
	}
}

func (sp *servicePublisher) updateService(newService *corev1.Service) {
	sp.Lock()
	defer sp.Unlock()
	sp.log.Debugf("Updating service for %s", sp.id)

	// set localTrafficPolicy to true if InternalTrafficPolicy is set to local
	if newService.Spec.InternalTrafficPolicy != nil {
		sp.localTrafficPolicy = *newService.Spec.InternalTrafficPolicy == corev1.ServiceInternalTrafficPolicyLocal
	} else {
		sp.localTrafficPolicy = false
	}

	for key, port := range sp.ports {
		newTargetPort := getTargetPort(newService, key.port)
		if newTargetPort != port.targetPort {
			port.updatePort(newTargetPort)
		}
		// update service endpoints with new localTrafficPolicy
		if port.localTrafficPolicy != sp.localTrafficPolicy {
			port.updateLocalTrafficPolicy(sp.localTrafficPolicy)
		}
	}

}

func (sp *servicePublisher) subscribe(srcPort Port, hostname string, listener EndpointUpdateListener) {
	sp.Lock()
	defer sp.Unlock()

	key := portAndHostname{
		port:     srcPort,
		hostname: hostname,
	}
	port, ok := sp.ports[key]
	if !ok {
		port = sp.newPortPublisher(srcPort, hostname)
		sp.ports[key] = port
	}
	port.subscribe(listener)
}

func (sp *servicePublisher) unsubscribe(srcPort Port, hostname string, listener EndpointUpdateListener) {
	sp.Lock()
	defer sp.Unlock()

	key := portAndHostname{
		port:     srcPort,
		hostname: hostname,
	}
	port, ok := sp.ports[key]
	if ok {
		port.unsubscribe(listener)
		if len(port.listeners) == 0 {
			endpointsVecs.unregister(sp.metricsLabels(srcPort, hostname))
			delete(sp.ports, key)
		}
	}
}

func (sp *servicePublisher) newPortPublisher(srcPort Port, hostname string) *portPublisher {
	targetPort := intstr.FromInt(int(srcPort))
	svc, err := sp.k8sAPI.Svc().Lister().Services(sp.id.Namespace).Get(sp.id.Name)
	if err != nil && !apierrors.IsNotFound(err) {
		sp.log.Errorf("error getting service: %s", err)
	}
	exists := false
	if err == nil {
		targetPort = getTargetPort(svc, srcPort)
		exists = true
	}

	log := sp.log.WithField("port", srcPort)

	port := &portPublisher{
		listeners:            []EndpointUpdateListener{},
		targetPort:           targetPort,
		srcPort:              srcPort,
		hostname:             hostname,
		exists:               exists,
		k8sAPI:               sp.k8sAPI,
		metadataAPI:          sp.metadataAPI,
		log:                  log,
		metrics:              endpointsVecs.newEndpointsMetrics(sp.metricsLabels(srcPort, hostname)),
		enableEndpointSlices: sp.enableEndpointSlices,
		localTrafficPolicy:   sp.localTrafficPolicy,
	}

	if port.enableEndpointSlices {
		matchLabels := map[string]string{discovery.LabelServiceName: sp.id.Name}
		selector := labels.Set(matchLabels).AsSelector()

		sliceList, err := sp.k8sAPI.ES().Lister().EndpointSlices(sp.id.Namespace).List(selector)
		if err != nil && !apierrors.IsNotFound(err) {
			sp.log.Errorf("error getting endpointSlice list: %s", err)
		}
		if err == nil {
			for _, slice := range sliceList {
				port.addEndpointSlice(slice)
			}
		}
	} else {
		endpoints, err := sp.k8sAPI.Endpoint().Lister().Endpoints(sp.id.Namespace).Get(sp.id.Name)
		if err != nil && !apierrors.IsNotFound(err) {
			sp.log.Errorf("error getting endpoints: %s", err)
		}
		if err == nil {
			port.updateEndpoints(endpoints)
		}
	}

	return port
}

func (sp *servicePublisher) metricsLabels(port Port, hostname string) prometheus.Labels {
	return endpointsLabels(sp.cluster, sp.id.Namespace, sp.id.Name, strconv.Itoa(int(port)), hostname)
}

func (sp *servicePublisher) updateServer(oldServer, newServer *v1beta3.Server) {
	sp.Lock()
	defer sp.Unlock()

	for _, pp := range sp.ports {
		pp.updateServer(oldServer, newServer)
	}
}

/////////////////////
/// portPublisher ///
/////////////////////

// Note that portPublishers methods are generally NOT thread-safe.  You should
// hold the parent servicePublisher's mutex before calling methods on a
// portPublisher.

func (pp *portPublisher) updateEndpoints(endpoints *corev1.Endpoints) {
	newAddressSet := pp.endpointsToAddresses(endpoints)
	if len(newAddressSet.Addresses) == 0 {
		for _, listener := range pp.listeners {
			listener.NoEndpoints(true)
		}
	} else {
		add, remove := diffAddresses(pp.addresses, newAddressSet)
		for _, listener := range pp.listeners {
			if len(remove.Addresses) > 0 {
				listener.Remove(remove)
			}
			if len(add.Addresses) > 0 {
				listener.Add(add)
			}
		}
	}
	pp.addresses = newAddressSet
	pp.exists = true
	pp.metrics.incUpdates()
	pp.metrics.setPods(len(pp.addresses.Addresses))
	pp.metrics.setExists(true)
}

func (pp *portPublisher) addEndpointSlice(slice *discovery.EndpointSlice) {
	newAddressSet := pp.endpointSliceToAddresses(slice)
	for id, addr := range pp.addresses.Addresses {
		if _, ok := newAddressSet.Addresses[id]; !ok {
			newAddressSet.Addresses[id] = addr
		}
	}

	add, _ := diffAddresses(pp.addresses, newAddressSet)
	if len(add.Addresses) > 0 {
		for _, listener := range pp.listeners {
			listener.Add(add)
		}
	}

	// even if the ES doesn't have addresses yet we need to create a new
	// pp.addresses entry with the appropriate Labels and LocalTrafficPolicy,
	// which isn't going to be captured during the ES update event when
	// addresses get added

	pp.addresses = newAddressSet
	pp.exists = true
	pp.metrics.incUpdates()
	pp.metrics.setPods(len(pp.addresses.Addresses))
	pp.metrics.setExists(true)
}

func (pp *portPublisher) updateEndpointSlice(oldSlice *discovery.EndpointSlice, newSlice *discovery.EndpointSlice) {
	updatedAddressSet := AddressSet{
		Addresses:          make(map[ID]Address),
		Labels:             pp.addresses.Labels,
		LocalTrafficPolicy: pp.localTrafficPolicy,
	}

	for id, address := range pp.addresses.Addresses {
		updatedAddressSet.Addresses[id] = address
	}

	for _, id := range pp.endpointSliceToIDs(oldSlice) {
		delete(updatedAddressSet.Addresses, id)
	}

	newAddressSet := pp.endpointSliceToAddresses(newSlice)
	for id, address := range newAddressSet.Addresses {
		updatedAddressSet.Addresses[id] = address
	}

	add, remove := diffAddresses(pp.addresses, updatedAddressSet)
	for _, listener := range pp.listeners {
		if len(remove.Addresses) > 0 {
			listener.Remove(remove)
		}
		if len(add.Addresses) > 0 {
			listener.Add(add)
		}
	}

	pp.addresses = updatedAddressSet
	pp.exists = true
	pp.metrics.incUpdates()
	pp.metrics.setPods(len(pp.addresses.Addresses))
	pp.metrics.setExists(true)
}

func metricLabels(resource interface{}) map[string]string {
	var serviceName, ns string
	var resLabels, resAnnotations map[string]string
	switch res := resource.(type) {
	case *corev1.Endpoints:
		{
			serviceName, ns = res.Name, res.Namespace
			resLabels, resAnnotations = res.Labels, res.Annotations
		}
	case *discovery.EndpointSlice:
		{
			serviceName, ns = res.Labels[discovery.LabelServiceName], res.Namespace
			resLabels, resAnnotations = res.Labels, res.Annotations
		}
	}

	labels := map[string]string{service: serviceName, namespace: ns}

	remoteClusterName, hasRemoteClusterName := resLabels[consts.RemoteClusterNameLabel]
	serviceFqn, hasServiceFqn := resAnnotations[consts.RemoteServiceFqName]

	if hasRemoteClusterName {
		// this means we are looking at Endpoints created for the purpose of mirroring
		// an out of cluster service.
		labels[targetCluster] = remoteClusterName
		if hasServiceFqn {
			fqParts := strings.Split(serviceFqn, ".")
			if len(fqParts) >= 2 {
				labels[targetService] = fqParts[0]
				labels[targetServiceNamespace] = fqParts[1]
			}
		}
	}
	return labels
}

func (pp *portPublisher) endpointSliceToAddresses(es *discovery.EndpointSlice) AddressSet {
	resolvedPort := pp.resolveESTargetPort(es.Ports)
	if resolvedPort == undefinedEndpointPort {
		return AddressSet{
			Labels:             metricLabels(es),
			Addresses:          make(map[ID]Address),
			LocalTrafficPolicy: pp.localTrafficPolicy,
		}
	}

	serviceID, err := getEndpointSliceServiceID(es)
	if err != nil {
		pp.log.Errorf("Could not fetch resource service name:%v", err)
	}

	addresses := make(map[ID]Address)
	for _, endpoint := range es.Endpoints {
		if endpoint.Hostname != nil {
			if pp.hostname != "" && pp.hostname != *endpoint.Hostname {
				continue
			}
		}
		if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
			continue
		}

		if endpoint.TargetRef == nil {
			for _, IPAddr := range endpoint.Addresses {
				var authorityOverride string
				if fqName, ok := es.Annotations[consts.RemoteServiceFqName]; ok {
					authorityOverride = net.JoinHostPort(fqName, fmt.Sprintf("%d", pp.srcPort))
				}

				identity := es.Annotations[consts.RemoteGatewayIdentity]
				address, id := pp.newServiceRefAddress(resolvedPort, IPAddr, serviceID.Name, es.Namespace)
				address.Identity, address.AuthorityOverride = identity, authorityOverride

				if endpoint.Hints != nil {
					zones := make([]discovery.ForZone, len(endpoint.Hints.ForZones))
					copy(zones, endpoint.Hints.ForZones)
					address.ForZones = zones
				}
				addresses[id] = address
			}
			continue
		}

		if endpoint.TargetRef.Kind == endpointTargetRefPod {
			for _, IPAddr := range endpoint.Addresses {
				address, id, err := pp.newPodRefAddress(
					resolvedPort,
					es.AddressType,
					IPAddr,
					endpoint.TargetRef.Name,
					endpoint.TargetRef.Namespace,
				)
				if err != nil {
					pp.log.Errorf("Unable to create new address:%v", err)
					continue
				}
				err = SetToServerProtocol(pp.k8sAPI, &address)
				if err != nil {
					pp.log.Errorf("failed to set address OpaqueProtocol: %s", err)
					continue
				}

				address.Zone = endpoint.Zone
				if endpoint.Hints != nil {
					zones := make([]discovery.ForZone, len(endpoint.Hints.ForZones))
					copy(zones, endpoint.Hints.ForZones)
					address.ForZones = zones
				}
				addresses[id] = address
			}
		}

		if endpoint.TargetRef.Kind == endpointTargetRefExternalWorkload {
			for _, IPAddr := range endpoint.Addresses {
				address, id, err := pp.newExtRefAddress(resolvedPort, IPAddr, endpoint.TargetRef.Name, es.Namespace)
				if err != nil {
					pp.log.Errorf("Unable to create new address: %v", err)
					continue
				}

				err = SetToServerProtocolExternalWorkload(pp.k8sAPI, &address)
				if err != nil {
					pp.log.Errorf("failed to set address OpaqueProtocol: %s", err)
					continue
				}

				address.Zone = endpoint.Zone
				if endpoint.Hints != nil {
					zones := make([]discovery.ForZone, len(endpoint.Hints.ForZones))
					copy(zones, endpoint.Hints.ForZones)
					address.ForZones = zones
				}

				addresses[id] = address
			}

		}

	}
	return AddressSet{
		Addresses:          addresses,
		Labels:             metricLabels(es),
		LocalTrafficPolicy: pp.localTrafficPolicy,
	}
}

// endpointSliceToIDs is similar to endpointSliceToAddresses but instead returns
// only the IDs of the endpoints rather than the addresses themselves.
func (pp *portPublisher) endpointSliceToIDs(es *discovery.EndpointSlice) []ID {
	resolvedPort := pp.resolveESTargetPort(es.Ports)
	if resolvedPort == undefinedEndpointPort {
		return []ID{}
	}

	serviceID, err := getEndpointSliceServiceID(es)
	if err != nil {
		pp.log.Errorf("Could not fetch resource service name:%v", err)
	}

	ids := []ID{}
	for _, endpoint := range es.Endpoints {
		if endpoint.Hostname != nil {
			if pp.hostname != "" && pp.hostname != *endpoint.Hostname {
				continue
			}
		}
		if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
			continue
		}

		if endpoint.TargetRef == nil {
			for _, IPAddr := range endpoint.Addresses {
				ids = append(ids, ServiceID{
					Name: strings.Join([]string{
						serviceID.Name,
						IPAddr,
						fmt.Sprint(resolvedPort),
					}, "-"),
					Namespace: es.Namespace,
				})
			}
			continue
		}

		if endpoint.TargetRef.Kind == endpointTargetRefPod {
			ids = append(ids, PodID{
				Name:      endpoint.TargetRef.Name,
				Namespace: endpoint.TargetRef.Namespace,
				IPFamily:  corev1.IPFamily(es.AddressType),
			})
		} else if endpoint.TargetRef.Kind == endpointTargetRefExternalWorkload {
			ids = append(ids, ExternalWorkloadID{
				Name:      endpoint.TargetRef.Name,
				Namespace: endpoint.TargetRef.Namespace,
			})
		}

	}
	return ids
}

func (pp *portPublisher) endpointsToAddresses(endpoints *corev1.Endpoints) AddressSet {
	addresses := make(map[ID]Address)
	for _, subset := range endpoints.Subsets {
		resolvedPort := pp.resolveTargetPort(subset)
		if resolvedPort == undefinedEndpointPort {
			continue
		}
		for _, endpoint := range subset.Addresses {
			if pp.hostname != "" && pp.hostname != endpoint.Hostname {
				continue
			}

			if endpoint.TargetRef == nil {
				var authorityOverride string
				if fqName, ok := endpoints.Annotations[consts.RemoteServiceFqName]; ok {
					authorityOverride = fmt.Sprintf("%s:%d", fqName, pp.srcPort)
				}

				identity := endpoints.Annotations[consts.RemoteGatewayIdentity]
				address, id := pp.newServiceRefAddress(resolvedPort, endpoint.IP, endpoints.Name, endpoints.Namespace)
				address.Identity, address.AuthorityOverride = identity, authorityOverride

				addresses[id] = address
				continue
			}

			if endpoint.TargetRef.Kind == endpointTargetRefPod {
				address, id, err := pp.newPodRefAddress(
					resolvedPort,
					"",
					endpoint.IP,
					endpoint.TargetRef.Name,
					endpoint.TargetRef.Namespace,
				)
				if err != nil {
					pp.log.Errorf("Unable to create new address:%v", err)
					continue
				}
				err = SetToServerProtocol(pp.k8sAPI, &address)
				if err != nil {
					pp.log.Errorf("failed to set address OpaqueProtocol: %s", err)
					continue
				}
				addresses[id] = address
			}
		}
	}
	return AddressSet{
		Addresses: addresses,
		Labels:    metricLabels(endpoints),
	}
}

func (pp *portPublisher) newServiceRefAddress(endpointPort Port, endpointIP, serviceName, serviceNamespace string) (Address, ServiceID) {
	id := ServiceID{
		Name: strings.Join([]string{
			serviceName,
			endpointIP,
			fmt.Sprint(endpointPort),
		}, "-"),
		Namespace: serviceNamespace,
	}

	return Address{IP: endpointIP, Port: endpointPort}, id
}

func (pp *portPublisher) newPodRefAddress(
	endpointPort Port,
	ipFamily discovery.AddressType,
	endpointIP,
	podName,
	podNamespace string,
) (Address, PodID, error) {
	id := PodID{
		Name:      podName,
		Namespace: podNamespace,
		IPFamily:  corev1.IPFamily(ipFamily),
	}
	pod, err := pp.k8sAPI.Pod().Lister().Pods(id.Namespace).Get(id.Name)
	if err != nil {
		return Address{}, PodID{}, fmt.Errorf("unable to fetch pod %v: %w", id, err)
	}
	ownerKind, ownerName, err := pp.metadataAPI.GetOwnerKindAndName(context.Background(), pod, false)
	if err != nil {
		return Address{}, PodID{}, err
	}
	addr := Address{
		IP:        endpointIP,
		Port:      endpointPort,
		Pod:       pod,
		OwnerName: ownerName,
		OwnerKind: ownerKind,
	}

	return addr, id, nil
}

func (pp *portPublisher) newExtRefAddress(endpointPort Port, endpointIP, externalWorkloadName, externalWorkloadNamespace string) (Address, ExternalWorkloadID, error) {
	id := ExternalWorkloadID{
		Name:      externalWorkloadName,
		Namespace: externalWorkloadNamespace,
	}

	ew, err := pp.k8sAPI.ExtWorkload().Lister().ExternalWorkloads(id.Namespace).Get(id.Name)
	if err != nil {
		return Address{}, ExternalWorkloadID{}, fmt.Errorf("unable to fetch ExternalWorkload %v: %w", id, err)
	}

	addr := Address{
		IP:               endpointIP,
		Port:             endpointPort,
		ExternalWorkload: ew,
	}

	ownerRefs := ew.GetOwnerReferences()
	if len(ownerRefs) == 1 {
		parent := ownerRefs[0]
		addr.OwnerName = parent.Name
		addr.OwnerName = strings.ToLower(parent.Kind)
	}

	return addr, id, nil
}

func (pp *portPublisher) resolveESTargetPort(slicePorts []discovery.EndpointPort) Port {
	if slicePorts == nil {
		return undefinedEndpointPort
	}

	switch pp.targetPort.Type {
	case intstr.Int:
		return Port(pp.targetPort.IntVal)
	case intstr.String:
		for _, p := range slicePorts {
			name := ""
			if p.Name != nil {
				name = *p.Name
			}
			if name == pp.targetPort.StrVal {
				return Port(*p.Port)
			}
		}
	}
	return undefinedEndpointPort
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
	return undefinedEndpointPort
}

func (pp *portPublisher) updateLocalTrafficPolicy(localTrafficPolicy bool) {
	pp.localTrafficPolicy = localTrafficPolicy
	pp.addresses.LocalTrafficPolicy = localTrafficPolicy
	for _, listener := range pp.listeners {
		listener.Add(pp.addresses.shallowCopy())
	}
}

func (pp *portPublisher) updatePort(targetPort namedPort) {
	pp.targetPort = targetPort

	if pp.enableEndpointSlices {
		matchLabels := map[string]string{discovery.LabelServiceName: pp.id.Name}
		selector := labels.Set(matchLabels).AsSelector()

		endpointSlices, err := pp.k8sAPI.ES().Lister().EndpointSlices(pp.id.Namespace).List(selector)
		if err == nil {
			pp.addresses = AddressSet{}
			for _, slice := range endpointSlices {
				pp.addEndpointSlice(slice)
			}
		} else {
			pp.log.Errorf("Unable to get EndpointSlices during port update: %s", err)
		}
	} else {
		endpoints, err := pp.k8sAPI.Endpoint().Lister().Endpoints(pp.id.Namespace).Get(pp.id.Name)
		if err == nil {
			pp.updateEndpoints(endpoints)
		} else {
			pp.log.Errorf("Unable to get endpoints during port update: %s", err)
		}
	}
}

func (pp *portPublisher) deleteEndpointSlice(es *discovery.EndpointSlice) {
	addrSet := pp.endpointSliceToAddresses(es)
	for id := range addrSet.Addresses {
		delete(pp.addresses.Addresses, id)
	}

	for _, listener := range pp.listeners {
		listener.Remove(addrSet)
	}

	if len(pp.addresses.Addresses) == 0 {
		pp.noEndpoints(false)
	} else {
		pp.exists = true
		pp.metrics.incUpdates()
		pp.metrics.setPods(len(pp.addresses.Addresses))
		pp.metrics.setExists(true)
	}
}

func (pp *portPublisher) noEndpoints(exists bool) {
	pp.exists = exists
	pp.addresses = AddressSet{}
	for _, listener := range pp.listeners {
		listener.NoEndpoints(exists)
	}

	pp.metrics.incUpdates()
	pp.metrics.setExists(exists)
	pp.metrics.setPods(0)
}

func (pp *portPublisher) subscribe(listener EndpointUpdateListener) {
	if pp.exists {
		if len(pp.addresses.Addresses) > 0 {
			listener.Add(pp.addresses.shallowCopy())
		} else {
			listener.NoEndpoints(true)
		}
	} else {
		listener.NoEndpoints(false)
	}
	pp.listeners = append(pp.listeners, listener)

	pp.metrics.setSubscribers(len(pp.listeners))
}

func (pp *portPublisher) unsubscribe(listener EndpointUpdateListener) {
	for i, e := range pp.listeners {
		if e == listener {
			n := len(pp.listeners)
			pp.listeners[i] = pp.listeners[n-1]
			pp.listeners[n-1] = nil
			pp.listeners = pp.listeners[:n-1]
			break
		}
	}

	pp.metrics.setSubscribers(len(pp.listeners))
}
func (pp *portPublisher) updateServer(oldServer, newServer *v1beta3.Server) {
	updated := false
	for id, address := range pp.addresses.Addresses {

		if pp.isAddressSelected(address, oldServer) || pp.isAddressSelected(address, newServer) {
			if newServer != nil && pp.isAddressSelected(address, newServer) && newServer.Spec.ProxyProtocol == opaqueProtocol {
				address.OpaqueProtocol = true
			} else {
				address.OpaqueProtocol = false
			}
			if pp.addresses.Addresses[id].OpaqueProtocol != address.OpaqueProtocol {
				pp.addresses.Addresses[id] = address
				updated = true
			}
		}
	}
	if updated {
		for _, listener := range pp.listeners {
			listener.Add(pp.addresses.shallowCopy())
		}
		pp.metrics.incUpdates()
	}
}

func (pp *portPublisher) isAddressSelected(address Address, server *v1beta3.Server) bool {
	if server == nil {
		return false
	}

	if address.Pod != nil {
		selector, err := metav1.LabelSelectorAsSelector(server.Spec.PodSelector)
		if err != nil {
			pp.log.Errorf("failed to create Selector: %s", err)
			return false
		}

		if !selector.Matches(labels.Set(address.Pod.Labels)) {
			return false
		}

		switch server.Spec.Port.Type {
		case intstr.Int:
			if server.Spec.Port.IntVal == int32(address.Port) {
				return true
			}
		case intstr.String:
			for _, c := range address.Pod.Spec.Containers {
				for _, p := range c.Ports {
					if p.ContainerPort == int32(address.Port) && p.Name == server.Spec.Port.StrVal {
						return true
					}
				}
			}
		}

	} else if address.ExternalWorkload != nil {
		selector, err := metav1.LabelSelectorAsSelector(server.Spec.ExternalWorkloadSelector)
		if err != nil {
			pp.log.Errorf("failed to create Selector: %s", err)
			return false
		}

		if !selector.Matches(labels.Set(address.ExternalWorkload.Labels)) {
			return false
		}

		switch server.Spec.Port.Type {
		case intstr.Int:
			if server.Spec.Port.IntVal == int32(address.Port) {
				return true
			}
		case intstr.String:
			for _, p := range address.ExternalWorkload.Spec.Ports {
				if p.Port == int32(address.Port) && p.Name == server.Spec.Port.StrVal {
					return true
				}
			}
		}
	}
	return false
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

	if oldAddress.Pod != nil && newAddress.Pod != nil {
		// if these addresses are owned by pods we can check the resource versions
		return oldAddress.Pod.ResourceVersion != newAddress.Pod.ResourceVersion
	}
	return false
}

func diffAddresses(oldAddresses, newAddresses AddressSet) (add, remove AddressSet) {
	// TODO: this detects pods which have been added or removed, but does not
	// detect addresses which have been modified.  A modified address should trigger
	// an add of the new version.
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
		Addresses:          addAddresses,
		Labels:             newAddresses.Labels,
		LocalTrafficPolicy: newAddresses.LocalTrafficPolicy,
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
func SetToServerProtocol(k8sAPI *k8s.API, address *Address) error {
	if address.Pod == nil {
		return fmt.Errorf("endpoint not backed by Pod: %s:%d", address.IP, address.Port)
	}
	servers, err := k8sAPI.Srv().Lister().Servers("").List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list Servers: %w", err)
	}
	for _, server := range servers {
		selector, err := metav1.LabelSelectorAsSelector(server.Spec.PodSelector)
		if err != nil {
			return fmt.Errorf("failed to create Selector: %w", err)
		}
		if server.Spec.ProxyProtocol == opaqueProtocol && selector.Matches(labels.Set(address.Pod.Labels)) {
			var portMatch bool
			switch server.Spec.Port.Type {
			case intstr.Int:
				if server.Spec.Port.IntVal == int32(address.Port) {
					portMatch = true
				}
			case intstr.String:
				for _, c := range address.Pod.Spec.Containers {
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
	servers, err := k8sAPI.Srv().Lister().Servers("").List(labels.Everything())
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
