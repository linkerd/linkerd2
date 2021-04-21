package watcher

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
)

const (
	kubeSystem = "kube-system"

	// metrics labels
	service                = "service"
	namespace              = "namespace"
	targetCluster          = "target_cluster"
	targetService          = "target_service"
	targetServiceNamespace = "target_service_namespace"
)

const endpointTargetRefPod = "Pod"

// TODO: prom metrics for all the queues/caches
// https://github.com/linkerd/linkerd2/issues/2204

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
		OwnerName         string
		OwnerKind         string
		Identity          string
		AuthorityOverride string
		TopologyLabels    map[string]string
	}

	// AddressSet is a set of Address, indexed by ID.
	AddressSet struct {
		Addresses       map[ID]Address
		Labels          map[string]string
		TopologicalPref []string
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
		publishers map[ServiceID]*servicePublisher
		k8sAPI     *k8s.API

		log                  *logging.Entry
		enableEndpointSlices bool
		sync.RWMutex         // This mutex protects modification of the map itself.
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
		enableEndpointSlices bool

		TopologyPref []string
		ports        map[portAndHostname]*portPublisher
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
		enableEndpointSlices bool
		TopologyPref         []string

		exists    bool
		addresses AddressSet
		listeners []EndpointUpdateListener
		metrics   endpointsMetrics
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

// NewEndpointsWatcher creates an EndpointsWatcher and begins watching the
// k8sAPI for pod, service, and endpoint changes. An EndpointsWatcher will
// watch on Endpoints or EndpointSlice resources, depending on cluster configuration.
func NewEndpointsWatcher(k8sAPI *k8s.API, log *logging.Entry, enableEndpointSlices bool) *EndpointsWatcher {
	ew := &EndpointsWatcher{
		publishers:           make(map[ServiceID]*servicePublisher),
		k8sAPI:               k8sAPI,
		enableEndpointSlices: enableEndpointSlices,
		log: log.WithFields(logging.Fields{
			"component": "endpoints-watcher",
		}),
	}

	k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    ew.addService,
		DeleteFunc: ew.deleteService,
		UpdateFunc: func(_, obj interface{}) { ew.addService(obj) },
	})

	if ew.enableEndpointSlices {
		ew.log.Debugf("Watching EndpointSlice resources")
		k8sAPI.ES().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    ew.addEndpointSlice,
			DeleteFunc: ew.deleteEndpointSlice,
			UpdateFunc: ew.updateEndpointSlice,
		})
	} else {
		ew.log.Debugf("Watching Endpoints resources")
		k8sAPI.Endpoint().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    ew.addEndpoints,
			DeleteFunc: ew.deleteEndpoints,
			UpdateFunc: func(_, obj interface{}) { ew.addEndpoints(obj) },
		})
	}
	return ew
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
		ew.log.Infof("Establishing watch on endpoint [%s:%d]", id, port)
	} else {
		ew.log.Infof("Establishing watch on endpoint [%s.%s:%d]", hostname, id, port)
	}

	sp := ew.getOrNewServicePublisher(id)

	sp.subscribe(port, hostname, listener)
	return nil
}

// Unsubscribe removes a listener from the subscribers list for this authority.
func (ew *EndpointsWatcher) Unsubscribe(id ServiceID, port Port, hostname string, listener EndpointUpdateListener) {
	if hostname == "" {
		ew.log.Infof("Stopping watch on endpoint [%s:%d]", id, port)
	} else {
		ew.log.Infof("Stopping watch on endpoint [%s.%s:%d]", hostname, id, port)
	}

	sp, ok := ew.getServicePublisher(id)
	if !ok {
		ew.log.Errorf("Cannot unsubscribe from unknown service [%s:%d]", id, port)
		return
	}
	sp.unsubscribe(port, hostname, listener)
}

func (ew *EndpointsWatcher) addService(obj interface{}) {
	service := obj.(*corev1.Service)
	if service.Namespace == kubeSystem {
		return
	}
	id := ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	sp := ew.getOrNewServicePublisher(id)

	sp.updateService(service)
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

	if service.Namespace == kubeSystem {
		return
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

	if endpoints.Namespace == kubeSystem {
		return
	}
	id := ServiceID{endpoints.Namespace, endpoints.Name}
	sp := ew.getOrNewServicePublisher(id)
	sp.updateEndpoints(endpoints)
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

	if endpoints.Namespace == kubeSystem {
		return
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

	if newSlice.Namespace == kubeSystem {
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

	if newSlice.Namespace == kubeSystem {
		return
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

	if es.Namespace == kubeSystem {
		return
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
			TopologyPref:         make([]string, 0),
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
	sp.log.Debugf("Adding EndpointSlice for %s", sp.id)
	for _, port := range sp.ports {
		port.addEndpointSlice(newSlice)
	}
}

func (sp *servicePublisher) updateEndpointSlice(oldSlice *discovery.EndpointSlice, newSlice *discovery.EndpointSlice) {
	sp.Lock()
	defer sp.Unlock()
	sp.log.Debugf("Updating EndpointSlice for %s", sp.id)
	for _, port := range sp.ports {
		port.updateEndpointSlice(oldSlice, newSlice)
	}
}

func (sp *servicePublisher) deleteEndpointSlice(es *discovery.EndpointSlice) {
	sp.Lock()
	defer sp.Unlock()
	sp.log.Debugf("Deleting EndpointSlice for %s", sp.id)
	for _, port := range sp.ports {
		port.deleteEndpointSlice(es)
	}
}

func (sp *servicePublisher) updateService(newService *corev1.Service) {
	sp.Lock()
	defer sp.Unlock()
	sp.log.Debugf("Updating service for %s", sp.id)

	if sp.enableEndpointSlices {
		sp.TopologyPref = make([]string, len(newService.Spec.TopologyKeys))
		copy(sp.TopologyPref, newService.Spec.TopologyKeys)
	}

	for key, port := range sp.ports {
		if sp.enableEndpointSlices {
			port.TopologyPref = sp.TopologyPref
			port.updateTopologyPreference()
		}

		newTargetPort := getTargetPort(newService, key.port)
		if newTargetPort != port.targetPort {
			port.updatePort(newTargetPort)
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
		log:                  log,
		metrics:              endpointsVecs.newEndpointsMetrics(sp.metricsLabels(srcPort, hostname)),
		enableEndpointSlices: sp.enableEndpointSlices,
		TopologyPref:         sp.TopologyPref,
	}

	if port.enableEndpointSlices {
		matchLabels := map[string]string{discovery.LabelServiceName: sp.id.Name}
		selector := k8slabels.Set(matchLabels).AsSelector()

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
	return endpointsLabels(sp.id.Namespace, sp.id.Name, strconv.Itoa(int(port)), hostname)
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
		newAddressSet.Addresses[id] = addr
	}

	add, _ := diffAddresses(pp.addresses, newAddressSet)
	if len(add.Addresses) > 0 {
		for _, listener := range pp.listeners {
			listener.Add(add)
		}
	}

	pp.addresses = newAddressSet
	pp.exists = true
	pp.metrics.incUpdates()
	pp.metrics.setPods(len(pp.addresses.Addresses))
	pp.metrics.setExists(true)
}

func (pp *portPublisher) updateEndpointSlice(oldSlice *discovery.EndpointSlice, newSlice *discovery.EndpointSlice) {
	updatedAddressSet := AddressSet{
		Addresses:       make(map[ID]Address),
		Labels:          pp.addresses.Labels,
		TopologicalPref: pp.TopologyPref,
	}

	for id, address := range pp.addresses.Addresses {
		updatedAddressSet.Addresses[id] = address
	}

	oldAddressSet := pp.endpointSliceToAddresses(oldSlice)
	for id := range oldAddressSet.Addresses {
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

	if hasRemoteClusterName && hasServiceFqn {
		// this means we are looking at Endpoints created for the purpose of mirroring
		// an out of cluster service.
		labels[targetCluster] = remoteClusterName

		fqParts := strings.Split(serviceFqn, ".")
		if len(fqParts) >= 2 {
			labels[targetService] = fqParts[0]
			labels[targetServiceNamespace] = fqParts[1]
		}
	}
	return labels
}

func (pp *portPublisher) endpointSliceToAddresses(es *discovery.EndpointSlice) AddressSet {
	addressSet := AddressSet{
		TopologicalPref: pp.TopologyPref,
		Labels:          metricLabels(es),
		Addresses:       make(map[ID]Address),
	}

	resolvedPort := pp.resolveESTargetPort(es.Ports)
	if resolvedPort == undefinedEndpointPort {
		return addressSet
	}

	serviceID, err := getEndpointSliceServiceID(es)
	if err != nil {
		pp.log.Errorf("Could not fetch resource service name:%v", err)
	}

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
					authorityOverride = fmt.Sprintf("%s:%d", fqName, pp.srcPort)
				}

				identity := es.Annotations[consts.RemoteGatewayIdentity]
				address, id := pp.newServiceRefAddress(resolvedPort, IPAddr, serviceID.Name, es.Namespace)
				address.Identity, address.AuthorityOverride = authorityOverride, identity

				for k, v := range endpoint.Topology {
					address.TopologyLabels[k] = v
				}

				addressSet.Addresses[id] = address
			}

			continue
		}

		if endpoint.TargetRef.Kind == endpointTargetRefPod {
			for _, IPAddr := range endpoint.Addresses {
				address, id, err := pp.newPodRefAddress(resolvedPort, IPAddr, endpoint.TargetRef.Name, endpoint.TargetRef.Namespace)
				if err != nil {
					pp.log.Errorf("Unable to create new address:%v", err)
					continue
				}

				for k, v := range endpoint.Topology {
					address.TopologyLabels[k] = v
				}

				addressSet.Addresses[id] = address
			}
		}

	}

	return addressSet
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
				address, id, err := pp.newPodRefAddress(resolvedPort, endpoint.IP, endpoint.TargetRef.Name, endpoint.TargetRef.Namespace)
				if err != nil {
					pp.log.Errorf("Unable to create new address:%v", err)
					continue
				}
				if err != nil {
					pp.log.Errorf("failed to set opaque port annotation on pod: %s", err)
				}
				addresses[id] = address
			}
		}
	}
	return AddressSet{
		Addresses:       addresses,
		Labels:          metricLabels(endpoints),
		TopologicalPref: []string{},
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

	return Address{IP: endpointIP, Port: endpointPort, TopologyLabels: make(map[string]string)}, id
}

func (pp *portPublisher) newPodRefAddress(endpointPort Port, endpointIP, podName, podNamespace string) (Address, PodID, error) {
	id := PodID{
		Name:      podName,
		Namespace: podNamespace,
	}
	pod, err := pp.k8sAPI.Pod().Lister().Pods(id.Namespace).Get(id.Name)
	if err != nil {
		return Address{}, PodID{}, fmt.Errorf("unable to fetch pod %v:%v", id, err)
	}
	ownerKind, ownerName := pp.k8sAPI.GetOwnerKindAndName(context.Background(), pod, false)
	addr := Address{
		IP:             endpointIP,
		Port:           endpointPort,
		Pod:            pod,
		TopologyLabels: make(map[string]string),
		OwnerName:      ownerName,
		OwnerKind:      ownerKind,
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
			if *p.Name == pp.targetPort.StrVal {
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

func (pp *portPublisher) updatePort(targetPort namedPort) {
	pp.targetPort = targetPort

	if pp.enableEndpointSlices {
		matchLabels := map[string]string{discovery.LabelServiceName: pp.id.Name}
		selector := k8slabels.Set(matchLabels).AsSelector()

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

// updateTopologyPreference is used when a service's topology preference changes. This method
// propagates the changes to the portPublisher, the portPublisher's AddressSet and triggers
// an (empty) update for all of its listeners to reflect the new preference changes.
func (pp *portPublisher) updateTopologyPreference() {
	pp.addresses.TopologicalPref = pp.TopologyPref

	updatedAddrSet := AddressSet{
		Addresses:       make(map[ID]Address),
		Labels:          make(map[string]string),
		TopologicalPref: pp.TopologyPref,
	}
	for _, listener := range pp.listeners {
		listener.Add(updatedAddrSet)
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

	svcExists := len(pp.addresses.Addresses) > 0
	pp.noEndpoints(svcExists)
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
			listener.Add(pp.addresses)
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

////////////
/// util ///
////////////

// WithPort sets the port field in all addresses of an address set.
func (as *AddressSet) WithPort(port Port) AddressSet {
	wp := AddressSet{
		Addresses: map[PodID]Address{},
		Labels:    as.Labels,
	}
	for id, addr := range as.Addresses {
		addr.Port = port
		wp.Addresses[id] = addr
	}
	return wp
}

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
		Addresses:       addAddresses,
		Labels:          newAddresses.Labels,
		TopologicalPref: newAddresses.TopologicalPref,
	}
	remove = AddressSet{
		Addresses:       removeAddresses,
		TopologicalPref: newAddresses.TopologicalPref,
	}
	return add, remove
}

func getEndpointSliceServiceID(es *discovery.EndpointSlice) (ServiceID, error) {
	if !isValidSlice(es) {
		return ServiceID{}, fmt.Errorf("EndpointSlice [%s/%s] is invalid", es.Namespace, es.Name)
	}

	if svc, ok := es.Labels[discovery.LabelServiceName]; ok {
		return ServiceID{es.Namespace, svc}, nil
	}

	for _, ref := range es.OwnerReferences {
		if ref.Kind == "Service" && ref.Name != "" {
			return ServiceID{es.Namespace, ref.Name}, nil
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
