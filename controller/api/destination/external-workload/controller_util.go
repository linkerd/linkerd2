package externalworkload

import (
	"reflect"

	ewv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
)

func (ec *EndpointsController) getServicesToUpdateOnExternalWorkloadChange(old, cur interface{}) sets.Set[string] {
	newEw, newEwOk := cur.(*ewv1beta1.ExternalWorkload)
	oldEw, oldEwOk := old.(*ewv1beta1.ExternalWorkload)

	if !oldEwOk {
		ec.log.Errorf("Expected (cur) to be an EndpointSlice in getServicesToUpdateOnExternalWorkloadChange(), got type: %T", cur)
		return sets.Set[string]{}
	}

	if !newEwOk {
		ec.log.Errorf("Expected (old) to be an EndpointSlice in getServicesToUpdateOnExternalWorkloadChange(), got type: %T", old)
		return sets.Set[string]{}
	}

	if newEw.ResourceVersion == oldEw.ResourceVersion {
		// Periodic resync will send update events for all known ExternalWorkloads.
		// Two different versions of the same pod will always have different RVs
		return sets.Set[string]{}
	}

	ewChanged, labelsChanged := ewEndpointsChanged(oldEw, newEw)
	if !ewChanged && !labelsChanged {
		ec.log.Errorf("skipping update; nothing has changed between old rv %s and new rv %s", oldEw.ResourceVersion, newEw.ResourceVersion)
		return sets.Set[string]{}
	}

	services, err := ec.getExternalWorkloadSvcMembership(newEw)
	if err != nil {
		ec.log.Errorf("unable to get pod %s/%s's service memberships: %v", newEw.Namespace, newEw.Name, err)
		return sets.Set[string]{}
	}

	if labelsChanged {
		oldServices, err := ec.getExternalWorkloadSvcMembership(oldEw)
		if err != nil {
			ec.log.Errorf("unable to get pod %s/%s's service memberships: %v", oldEw.Namespace, oldEw.Name, err)
		}
		services = determineNeededServiceUpdates(oldServices, services, ewChanged)
	}

	return services
}

func determineNeededServiceUpdates(oldServices, services sets.Set[string], specChanged bool) sets.Set[string] {
	if specChanged {
		// if the labels and spec changed, all services need to be updated
		services = services.Union(oldServices)
	} else {
		// if only the labels changed, services not common to both the new
		// and old service set (the disjuntive union) need to be updated
		services = services.Difference(oldServices).Union(oldServices.Difference(services))
	}
	return services
}

// getExternalWorkloadSvcMembership accepts a pointer to an external workload
// resource and returns a set of service keys (<namespace>/<name>). The set
// includes all services local to the workload's namespace that match the workload.
func (ec *EndpointsController) getExternalWorkloadSvcMembership(workload *ewv1beta1.ExternalWorkload) (sets.Set[string], error) {
	keys := sets.Set[string]{}
	services, err := ec.k8sAPI.Svc().Lister().Services(workload.Namespace).List(labels.Everything())
	if err != nil {
		return keys, err
	}

	for _, svc := range services {
		if svc.Spec.Selector == nil {
			continue
		}

		// Taken from upstream k8s code, this checks whether a given object has
		// a deleted state before returning a `namespace/name` key. This is
		// important since we do not want to consider a service that has been
		// deleted and is waiting for cache eviction
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(svc)
		if err != nil {
			return sets.Set[string]{}, err
		}

		// Check if service selects our ExternalWorkload.
		if labels.ValidatedSetSelector(svc.Spec.Selector).Matches(labels.Set(workload.Labels)) {
			keys.Insert(key)
		}
	}

	return keys, nil
}

// getEndpointSliceFromDeleteAction parses an EndpointSlice from a delete action.
func (ec *EndpointsController) getEndpointSliceFromDeleteAction(obj interface{}) *discoveryv1.EndpointSlice {
	if endpointSlice, ok := obj.(*discoveryv1.EndpointSlice); ok {
		// Enqueue all the services that the pod used to be a member of.
		// This is the same thing we do when we add a pod.
		return endpointSlice
	}
	// If we reached here it means the pod was deleted but its final state is unrecorded.
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		ec.log.Errorf("Couldn't get object from tombstone")
		return nil
	}
	endpointSlice, ok := tombstone.Obj.(*discoveryv1.EndpointSlice)
	if !ok {
		ec.log.Errorf("Tombstone contained object that is not a EndpointSlice")
		return nil
	}
	return endpointSlice
}

// getExternalWorkloadFromDeleteAction parses an ExternalWorkload from a delete action.
func (ec *EndpointsController) getExternalWorkloadFromDeleteAction(obj interface{}) *ewv1beta1.ExternalWorkload {
	if ew, ok := obj.(*ewv1beta1.ExternalWorkload); ok {
		return ew
	}

	// If we reached here it means the pod was deleted but its final state is unrecorded.
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		ec.log.Errorf("couldn't get object from tombstone %#v", obj)
		return nil
	}

	ew, ok := tombstone.Obj.(*ewv1beta1.ExternalWorkload)
	if !ok {
		ec.log.Errorf("tombstone contained object that is not a ExternalWorkload: %#v", obj)
		return nil
	}
	return ew
}

// ewEndpointsChanged returns two boolean values. The first is true if the ExternalWorkload has
// changed in a way that may change existing endpoints. The second value is true if the
// ExternalWorkload has changed in a way that may affect which Services it matches.
func ewEndpointsChanged(oldEw, newEw *ewv1beta1.ExternalWorkload) (bool, bool) {
	// Check if the ExternalWorkload labels have changed, indicating a possible
	// change in the service membership
	labelsChanged := false
	if !reflect.DeepEqual(newEw.Labels, oldEw.Labels) {
		labelsChanged = true
	}

	// If the ExternalWorkload's deletion timestamp is set, remove endpoint from ready address.
	if newEw.DeletionTimestamp != oldEw.DeletionTimestamp {
		return true, labelsChanged
	}
	// If the ExternalWorkload's readiness has changed, the associated endpoint address
	// will move from the unready endpoints set to the ready endpoints.
	// So for the purposes of an endpoint, a readiness change on an ExternalWorkload
	// means we have a changed ExternalWorkload.
	if IsEwReady(oldEw) != IsEwReady(newEw) {
		return true, labelsChanged
	}

	// Check if the ExternalWorkload IPs have changed
	if len(oldEw.Spec.WorkloadIPs) != len(newEw.Spec.WorkloadIPs) {
		return true, labelsChanged
	}
	for i := range oldEw.Spec.WorkloadIPs {
		if oldEw.Spec.WorkloadIPs[i].Ip != newEw.Spec.WorkloadIPs[i].Ip {
			return true, labelsChanged
		}
	}

	// Check if the Ports  have changed
	if len(oldEw.Spec.Ports) != len(newEw.Spec.Ports) {
		return true, labelsChanged
	}

	// Determine if the ports have changed between workload resources
	portSet := make(map[int32]ewv1beta1.PortSpec)
	for _, ps := range newEw.Spec.Ports {
		portSet[ps.Port] = ps
	}

	for _, oldPs := range oldEw.Spec.Ports {
		// If the port number is present in the new workload but not the old
		// one, then we have a diff and we return early
		newPs, ok := portSet[oldPs.Port]
		if !ok {
			return true, labelsChanged
		}

		// If the port is present in both workloads, we check to see if any of
		// the port spec's values have changed, e.g. name or protocol
		if newPs.Name != oldPs.Name || newPs.Protocol != oldPs.Protocol {
			return true, labelsChanged
		}
	}

	return false, labelsChanged
}

func managedByController(es *discoveryv1.EndpointSlice) bool {
	esManagedBy := es.Labels[discoveryv1.LabelManagedBy]
	return managedBy == esManagedBy
}

func managedByChanged(endpointSlice1, endpointSlice2 *discoveryv1.EndpointSlice) bool {
	return managedByController(endpointSlice1) != managedByController(endpointSlice2)
}

func IsEwReady(ew *ewv1beta1.ExternalWorkload) bool {
	if len(ew.Status.Conditions) == 0 {
		return false
	}

	// Loop through the conditions and look at each condition in turn starting
	// from the top.
	for i := range ew.Status.Conditions {
		cond := ew.Status.Conditions[i]
		// Stop once we find a 'Ready' condition. We expect a resource to only
		// have one 'Ready' type condition.
		if cond.Type == ewv1beta1.WorkloadReady && cond.Status == ewv1beta1.ConditionTrue {
			return true
		}
	}

	return false
}
