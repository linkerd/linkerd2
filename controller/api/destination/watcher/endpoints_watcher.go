package watcher

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/prometheus/client_golang/prometheus"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	}

	// AddressSet is a set of Address, indexed by ID.
	AddressSet struct {
		Addresses map[ID]Address
		Labels    map[string]string
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

		log          *logging.Entry
		sync.RWMutex // This mutex protects modification of the map itself.
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
		id     ServiceID
		log    *logging.Entry
		k8sAPI *k8s.API

		ports map[portAndHostname]*portPublisher
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
		id         ServiceID
		targetPort namedPort
		srcPort    Port
		hostname   string
		log        *logging.Entry
		k8sAPI     *k8s.API

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
	endpoints := obj.(*corev1.Endpoints)
	if endpoints.Namespace == kubeSystem {
		return
	}
	id := ServiceID{
		Namespace: endpoints.Namespace,
		Name:      endpoints.Name,
	}

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
			k8sAPI: ew.k8sAPI,
			ports:  make(map[portAndHostname]*portPublisher),
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

func (sp *servicePublisher) updateService(newService *corev1.Service) {
	sp.Lock()
	defer sp.Unlock()
	sp.log.Debugf("Updating service for %s", sp.id)

	for key, port := range sp.ports {
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
		listeners:  []EndpointUpdateListener{},
		targetPort: targetPort,
		srcPort:    srcPort,
		hostname:   hostname,
		exists:     exists,
		k8sAPI:     sp.k8sAPI,
		log:        log,
		metrics:    endpointsVecs.newEndpointsMetrics(sp.metricsLabels(srcPort, hostname)),
	}

	endpoints, err := sp.k8sAPI.Endpoint().Lister().Endpoints(sp.id.Namespace).Get(sp.id.Name)
	if err != nil && !apierrors.IsNotFound(err) {
		sp.log.Errorf("error getting endpoints: %s", err)
	}
	if err == nil {
		port.updateEndpoints(endpoints)
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
	pp.exists = true
	pp.addresses = newAddressSet

	pp.metrics.incUpdates()
	pp.metrics.setPods(len(pp.addresses.Addresses))
	pp.metrics.setExists(true)
}

func (pp *portPublisher) endpointsToAddresses(endpoints *corev1.Endpoints) AddressSet {
	addresses := make(map[ID]Address)
	for _, subset := range endpoints.Subsets {
		resolvedPort := pp.resolveTargetPort(subset)
		for _, endpoint := range subset.Addresses {
			if pp.hostname != "" && pp.hostname != endpoint.Hostname {
				continue
			}
			if endpoint.TargetRef == nil {
				id := ServiceID{
					Name: strings.Join([]string{
						endpoints.ObjectMeta.Name,
						endpoint.IP,
						fmt.Sprint(resolvedPort),
					}, "-"),
					Namespace: endpoints.ObjectMeta.Namespace,
				}

				var authorityOverride string
				if fqName, ok := endpoints.Annotations[consts.RemoteServiceFqName]; ok {
					authorityOverride = fmt.Sprintf("%s:%d", fqName, pp.srcPort)
				}

				addresses[id] = Address{
					IP:                endpoint.IP,
					Port:              resolvedPort,
					Identity:          endpoints.Annotations[consts.RemoteGatewayIdentity],
					AuthorityOverride: authorityOverride,
				}
				continue
			}
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
				ownerKind, ownerName := pp.k8sAPI.GetOwnerKindAndName(pod, false)
				addresses[id] = Address{
					IP:        endpoint.IP,
					Port:      resolvedPort,
					Pod:       pod,
					OwnerName: ownerName,
					OwnerKind: ownerKind,
				}
			}
		}
	}
	return AddressSet{
		Addresses: addresses,
		Labels:    map[string]string{"service": endpoints.Name, "namespace": endpoints.Namespace},
	}
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

func diffAddresses(oldAddresses, newAddresses AddressSet) (add, remove AddressSet) {
	// TODO: this detects pods which have been added or removed, but does not
	// detect addresses which have been modified.  A modified address should trigger
	// an add of the new version.
	addAddesses := make(map[ID]Address)
	removeAddresses := make(map[ID]Address)
	for id, address := range newAddresses.Addresses {
		if _, ok := oldAddresses.Addresses[id]; !ok {
			addAddesses[id] = address
		}
	}
	for id, address := range oldAddresses.Addresses {
		if _, ok := newAddresses.Addresses[id]; !ok {
			removeAddresses[id] = address
		}
	}
	add = AddressSet{
		Addresses: addAddesses,
		Labels:    newAddresses.Labels,
	}
	remove = AddressSet{
		Addresses: removeAddresses,
	}
	return
}
