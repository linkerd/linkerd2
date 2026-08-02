package watcher

import (
	"context"
	"fmt"
	"maps"
	"net"
	"strings"

	"github.com/linkerd/linkerd2/controller/gen/apis/server/v1beta3"
	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type (
	// portPublisher represents a service along with a port and optionally a
	// hostname.  Multiple listeners may be subscribed to a portPublisher.
	// portPublisher maintains the current state of the address set and
	// publishes diffs to all listeners when updates come from either the
	// endpoints API or the service API.
	portPublisher struct {
		id ServiceID

		targetPort           namedPort
		srcPort              Port
		log                  *logging.Entry
		k8sAPI               *k8s.API
		metadataAPI          *k8s.MetadataAPI
		enableEndpointSlices bool
		enableIPv6           bool
		exists               bool
		addresses            AddressSet
		filteredListeners    map[FilterKey]*filteredListenerGroup
		cluster              string
		localTrafficPolicy   bool
	}
)

// Note that portPublishers methods are generally NOT thread-safe.  You should
// hold the parent servicePublisher's mutex before calling methods on a
// portPublisher.

func (pp *portPublisher) updateEndpoints(endpoints *corev1.Endpoints) {
	newAddressSet := pp.endpointsToAddresses(endpoints)
	if len(newAddressSet.Addresses) == 0 {
		pp.publishNoEndpoints(true)
	} else {
		pp.publishAddressChange(newAddressSet)
	}
	pp.exists = true
	pp.addresses = newAddressSet
}

func (pp *portPublisher) addEndpointSlice(slice *discovery.EndpointSlice) {
	newAddressSet := pp.endpointSliceToAddresses(slice)
	for id, addr := range pp.addresses.Addresses {
		if _, ok := newAddressSet.Addresses[id]; !ok {
			newAddressSet.Addresses[id] = addr
		}
	}

	pp.publishAddressChange(newAddressSet)

	// even if the ES doesn't have addresses yet we need to create a new
	// pp.addresses entry with the appropriate Labels and LocalTrafficPolicy,
	// which isn't going to be captured during the ES update event when
	// addresses get added

	pp.addresses = newAddressSet
	pp.exists = true
}

func (pp *portPublisher) updateEndpointSlice(oldSlice *discovery.EndpointSlice, newSlice *discovery.EndpointSlice) {
	updatedAddressSet := AddressSet{
		Addresses: make(map[ID]Address),
		Labels:    pp.addresses.Labels,
	}
	maps.Copy(updatedAddressSet.Addresses, pp.addresses.Addresses)

	for _, id := range pp.endpointSliceToIDs(oldSlice) {
		delete(updatedAddressSet.Addresses, id)
	}

	newAddressSet := pp.endpointSliceToAddresses(newSlice)
	maps.Copy(updatedAddressSet.Addresses, newAddressSet.Addresses)
	pp.publishAddressChange(updatedAddressSet)

	pp.addresses = updatedAddressSet
	pp.exists = true
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
			Labels:    metricLabels(es),
			Addresses: make(map[ID]Address),
		}
	}

	serviceID, err := getEndpointSliceServiceID(es)
	if err != nil {
		pp.log.Errorf("Could not fetch resource service name:%v", err)
	}

	addresses := make(map[ID]Address)
	for _, endpoint := range es.Endpoints {
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
				address, id := pp.newServiceRefAddress(resolvedPort, IPAddr, endpoint.Hostname, serviceID.Name, es.Namespace)
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
					endpoint.Hostname,
					endpoint.TargetRef.Name,
					endpoint.TargetRef.Namespace,
				)
				if err != nil {
					pp.log.Errorf("Unable to create new address:%v", err)
					continue
				}
				err = SetToServerProtocol(pp.k8sAPI, &address, pp.log)
				if err != nil {
					pp.log.Errorf("failed to set address OpaqueProtocol: %s", err)
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
				address, id, err := pp.newExtRefAddress(resolvedPort, IPAddr, endpoint.Hostname, endpoint.TargetRef.Name, es.Namespace)
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
		Addresses: addresses,
		Labels:    metricLabels(es),
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
		if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
			continue
		}

		if endpoint.TargetRef == nil {
			for _, IPAddr := range endpoint.Addresses {
				nameParts := []string{
					serviceID.Name,
					IPAddr,
					fmt.Sprint(resolvedPort),
				}
				if endpoint.Hostname != nil && *endpoint.Hostname != "" {
					nameParts = append(nameParts, *endpoint.Hostname)
				}
				ids = append(ids, ServiceID{
					Name:      strings.Join(nameParts, "-"),
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
			hostname := endpoint.Hostname
			if endpoint.TargetRef == nil {
				var authorityOverride string
				if fqName, ok := endpoints.Annotations[consts.RemoteServiceFqName]; ok {
					authorityOverride = fmt.Sprintf("%s:%d", fqName, pp.srcPort)
				}

				identity := endpoints.Annotations[consts.RemoteGatewayIdentity]
				address, id := pp.newServiceRefAddress(resolvedPort, endpoint.IP, &hostname, endpoints.Name, endpoints.Namespace)
				address.Identity, address.AuthorityOverride = identity, authorityOverride

				addresses[id] = address
				continue
			}

			if endpoint.TargetRef.Kind == endpointTargetRefPod {
				address, id, err := pp.newPodRefAddress(
					resolvedPort,
					"",
					endpoint.IP,
					&hostname,
					endpoint.TargetRef.Name,
					endpoint.TargetRef.Namespace,
				)
				if err != nil {
					pp.log.Errorf("Unable to create new address:%v", err)
					continue
				}
				err = SetToServerProtocol(pp.k8sAPI, &address, pp.log)
				if err != nil {
					pp.log.Errorf("failed to set address OpaqueProtocol: %s", err)
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

func (pp *portPublisher) newServiceRefAddress(endpointPort Port, endpointIP string, hostname *string, serviceName, serviceNamespace string) (Address, ServiceID) {
	nameParts := []string{
		serviceName,
		endpointIP,
		fmt.Sprint(endpointPort),
	}
	if hostname != nil && *hostname != "" {
		nameParts = append(nameParts, *hostname)
	}

	id := ServiceID{
		Name:      strings.Join(nameParts, "-"),
		Namespace: serviceNamespace,
	}

	return Address{IP: endpointIP, Port: endpointPort, Hostname: hostname}, id
}

func (pp *portPublisher) newPodRefAddress(
	endpointPort Port,
	ipFamily discovery.AddressType,
	endpointIP string,
	hostname *string,
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
		Hostname:  hostname,
	}

	return addr, id, nil
}

func (pp *portPublisher) newExtRefAddress(
	endpointPort Port,
	endpointIP string,
	hostname *string,
	externalWorkloadName,
	externalWorkloadNamespace string,
) (Address, ExternalWorkloadID, error) {
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
		Hostname:         hostname,
	}

	ownerRefs := ew.GetOwnerReferences()
	if len(ownerRefs) == 1 {
		parent := ownerRefs[0]
		addr.OwnerName = parent.Name
		addr.OwnerKind = strings.ToLower(parent.Kind)
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
	for _, group := range pp.filteredListeners {
		group.updateLocalTrafficPolicy(localTrafficPolicy)
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
	updatedAddressSet := AddressSet{
		Addresses: make(map[ID]Address),
		Labels:    pp.addresses.Labels,
	}
	for id, address := range pp.addresses.Addresses {
		updatedAddressSet.Addresses[id] = address
	}

	addrSet := pp.endpointSliceToAddresses(es)
	for id := range addrSet.Addresses {
		delete(updatedAddressSet.Addresses, id)
	}

	pp.publishAddressChange(updatedAddressSet)
	pp.addresses = updatedAddressSet

	if len(pp.addresses.Addresses) == 0 {
		pp.noEndpoints(false)
	} else {
		pp.exists = true
	}
}

func (pp *portPublisher) noEndpoints(exists bool) {
	pp.exists = exists
	pp.addresses = AddressSet{}
	pp.publishNoEndpoints(exists)
}

func (pp *portPublisher) subscribe(listener EndpointUpdateListener, filterKey FilterKey) error {
	group, err := pp.filteredListenerGroup(filterKey)
	if err != nil {
		return err
	}
	if pp.exists {
		if len(pp.addresses.Addresses) > 0 {
			group.availableEndpoints = pp.addresses
			filteredSet := group.filterAddresses(pp.addresses)
			group.snapshot = filteredSet
			if len(filteredSet.Addresses) > 0 {
				listener.Add(filteredSet.shallowCopy())
			}
		}
	}
	group.listeners = append(group.listeners, listener)
	group.metrics.setSubscribers(len(group.listeners))

	return nil
}

func (pp *portPublisher) unsubscribe(listener EndpointUpdateListener, filterKey FilterKey, withRemove bool) {
	group, ok := pp.filteredListeners[filterKey]
	if ok {
		if withRemove {
			listener.Remove(group.snapshot)
		}

		for i, existing := range group.listeners {
			if existing == listener {
				n := len(group.listeners)
				group.listeners[i] = group.listeners[n-1]
				group.listeners[n-1] = nil
				group.listeners = group.listeners[:n-1]
				break
			}
		}
		if len(group.listeners) == 0 {
			endpointsVecs.unregister(endpointsLabels(
				pp.cluster, pp.id.Namespace, pp.id.Name, fmt.Sprintf("%d", pp.srcPort), filterKey.Hostname, filterKey.NodeName,
			))
			delete(pp.filteredListeners, filterKey)
		}

		group.metrics.setSubscribers(len(group.listeners))
	}
}
func (pp *portPublisher) updateServer(oldServer, newServer *v1beta3.Server) {
	updated := false
	for id, address := range pp.addresses.Addresses {

		if pp.isAddressSelected(address, oldServer) || pp.isAddressSelected(address, newServer) {
			oldOpaque := address.OpaqueProtocol
			if newServer != nil && pp.isAddressSelected(address, newServer) && newServer.Spec.ProxyProtocol == opaqueProtocol {
				address.OpaqueProtocol = true
			} else {
				address.OpaqueProtocol = false
			}
			if oldOpaque != address.OpaqueProtocol {
				pp.addresses.Addresses[id] = address
				updated = true
			}
		}
	}
	if updated {
		pp.publishFilteredSnapshots()
	}
}

func (pp *portPublisher) filteredListenerGroup(filterKey FilterKey) (*filteredListenerGroup, error) {
	group, ok := pp.filteredListeners[filterKey]
	if !ok {
		nodeTopologyZone := ""
		if filterKey.EnableEndpointFiltering && filterKey.NodeName != "" {
			node, err := pp.metadataAPI.Get(k8s.Node, filterKey.NodeName)
			if err != nil {
				pp.log.Errorf("Unable to get node %s: %s", filterKey.NodeName, err)
			} else {
				nodeTopologyZone = node.Labels[corev1.LabelTopologyZone]
			}
		}

		metrics, err := endpointsVecs.newEndpointsMetrics(endpointsLabels(pp.cluster, pp.id.Namespace, pp.id.Name, fmt.Sprintf("%d", pp.srcPort), filterKey.Hostname, filterKey.NodeName))
		if err != nil {
			return nil, err
		}
		group = newFilteredListenerGroup(filterKey, nodeTopologyZone, pp.enableIPv6, pp.localTrafficPolicy, metrics)
		pp.filteredListeners[filterKey] = group
	}
	return group, nil
}

func (pp *portPublisher) publishAddressChange(newAddressSet AddressSet) {
	for _, group := range pp.filteredListeners {
		group.publishDiff(newAddressSet)
	}
}

func (pp *portPublisher) publishFilteredSnapshots() {
	for _, group := range pp.filteredListeners {
		group.publishDiff(pp.addresses)
	}
}

func (pp *portPublisher) publishNoEndpoints(exists bool) {
	for _, group := range pp.filteredListeners {
		group.publishNoEndpoints(exists)
	}
}

func (pp *portPublisher) isAddressSelected(address Address, server *v1beta3.Server) bool {
	if server == nil {
		return false
	}

	if address.Pod != nil {
		if address.Pod.Namespace != server.Namespace {
			return false
		}

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
			for _, c := range append(address.Pod.Spec.InitContainers, address.Pod.Spec.Containers...) {
				for _, p := range c.Ports {
					if p.ContainerPort == int32(address.Port) && p.Name == server.Spec.Port.StrVal {
						return true
					}
				}
			}
		}

	} else if address.ExternalWorkload != nil {
		if address.ExternalWorkload.Namespace != server.Namespace {
			return false
		}

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

func (pp *portPublisher) totalListeners() int {
	total := 0
	for _, group := range pp.filteredListeners {
		total += len(group.listeners)
	}
	return total
}
