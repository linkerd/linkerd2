package externalworkload

import (
	"fmt"

	ewv1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1alpha1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// getServicesToUpdateOnExternalWorkloadChange will look at an old and an
// updated external workload resource and determine which services need to
// be reconciled. The outcome is determined by what has changed in-between
// resources (selections, spec, or both).
func (ec *EndpointsController) getServicesToUpdateOnExternalWorkloadChange(o, c interface{}) ([]string, error) {
	old, ok := o.(*ewv1alpha1.ExternalWorkload)
	if !ok {
		return nil, fmt.Errorf("couldn't get ExternalWorkload from object %#v", o)
	}

	updated, ok := c.(*ewv1alpha1.ExternalWorkload)
	if !ok {
		return nil, fmt.Errorf("couldn't get ExternalWorkload from object %#v", o)
	}

	labelsChanged := labelsChanged(old, updated)
	specChanged := specChanged(old, updated)
	if !labelsChanged && !specChanged {
		ec.log.Debugf("skipping update; nothing has changed between old rv %s and new rv %s", old.ResourceVersion, updated.ResourceVersion)
		return nil, nil
	}

	newSelection, err := ec.getExternalWorkloadSvcMembership(updated)
	if err != nil {
		return nil, err

	}

	oldSelection, err := ec.getExternalWorkloadSvcMembership(old)
	if err != nil {
		return nil, err
	}

	result := map[string]struct{}{}
	// Determine the list of services we need to update based on the difference
	// between our old and updated workload.
	//
	// Service selections (i.e. services that select a workload through a label
	// selector) may reference an old workload, a new workload, or both,
	// depending on the workload's labels.
	if labelsChanged && specChanged {
		// When the selection has changed, and the workload has changed, all
		// services need to be updated so we consider the union of selections.
		result = toSet(append(newSelection, oldSelection...))
	} else if specChanged {
		// When the workload resource has changed, it is enough to consider
		// either the oldSelection slice or the newSelection slice, since they
		// are equal. We have the same set of services to update since no
		// selection has been changed by the update.
		return newSelection, nil
	} else {
		// When the selection has changed, then we need to consider only
		// services that reference the old workload's labels, or the new
		// workload's labels, but not both. Services that select both are
		// unchanged since the workload has not changed.
		newSelectionSet := toSet(newSelection)
		oldSelectionSet := toSet(oldSelection)

		// Determine selections that reference only the old workload resource
		for _, oldSvc := range oldSelection {
			if _, ok := newSelectionSet[oldSvc]; !ok {
				result[oldSvc] = struct{}{}
			}
		}

		// Determine selections that reference only the new workload resource
		for _, newSvc := range newSelection {
			if _, ok := oldSelectionSet[newSvc]; !ok {
				result[newSvc] = struct{}{}
			}
		}
	}

	var resultSlice []string
	for svc := range result {
		resultSlice = append(resultSlice, svc)
	}

	return resultSlice, nil
}

// getExternalWorkloadSvcMembership accepts a pointer to an external workload
// resource and returns a set of service keys (<namespace>/<name>). The set
// includes all services local to the workload's namespace that match the workload.
func (ec *EndpointsController) getExternalWorkloadSvcMembership(workload *ewv1alpha1.ExternalWorkload) ([]string, error) {
	keys := []string{}
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
			return []string{}, err
		}

		// Check if service selects our ExternalWorkload.
		if labels.ValidatedSetSelector(svc.Spec.Selector).Matches(labels.Set(workload.Labels)) {
			keys = append(keys, key)
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
func (ec *EndpointsController) getExternalWorkloadFromDeleteAction(obj interface{}) *ewv1alpha1.ExternalWorkload {
	if ew, ok := obj.(*ewv1alpha1.ExternalWorkload); ok {
		return ew
	}

	// If we reached here it means the pod was deleted but its final state is unrecorded.
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		ec.log.Errorf("couldn't get object from tombstone %#v", obj)
		return nil
	}

	ew, ok := tombstone.Obj.(*ewv1alpha1.ExternalWorkload)
	if !ok {
		ec.log.Errorf("tombstone contained object that is not a ExternalWorkload: %#v", obj)
		return nil
	}
	return ew
}

// Check whether two label sets are matching
func labelsChanged(old, updated *ewv1alpha1.ExternalWorkload) bool {
	if len(old.Labels) != len(updated.Labels) {
		return true
	}

	for upK, upV := range updated.Labels {
		oldV, ok := old.Labels[upK]
		if !ok || oldV != upV {
			return true
		}
	}

	return false
}

// specChanged will check whether two workload resource specs have changed
//
// Note: we are interested in changes to the ports, ips and readiness fields
// since these are going to influence a change in a service's underlying
// endpoint slice
func specChanged(old, updated *ewv1alpha1.ExternalWorkload) bool {
	if isReady(old) != isReady(updated) {
		return true
	}

	if len(old.Spec.Ports) != len(updated.Spec.Ports) ||
		len(old.Spec.WorkloadIPs) != len(updated.Spec.WorkloadIPs) {
		return true
	}

	// Determine if the ports have changed between workload resources
	portSet := make(map[int32]ewv1alpha1.PortSpec)
	for _, ps := range updated.Spec.Ports {
		portSet[ps.Port] = ps
	}

	for _, oldPs := range old.Spec.Ports {
		// If the port number is present in the new workload but not the old
		// one, then we have a diff and we return early
		newPs, ok := portSet[oldPs.Port]
		if !ok {
			return true
		}

		// If the port is present in both workloads, we check to see if any of
		// the port spec's values have changed, e.g. name or protocol
		if newPs.Name != oldPs.Name || newPs.Protocol != oldPs.Protocol {
			return true
		}
	}

	// Determine if the ips have changed between workload resources. If an IP
	// is documented for one workload but not the other, then we have a diff.
	ipSet := make(map[string]struct{})
	for _, addr := range updated.Spec.WorkloadIPs {
		ipSet[addr.Ip] = struct{}{}
	}

	for _, addr := range old.Spec.WorkloadIPs {
		if _, ok := ipSet[addr.Ip]; !ok {
			return true
		}
	}

	return false
}

func managedByController(es *discoveryv1.EndpointSlice) bool {
	esManagedBy := es.Labels[discoveryv1.LabelManagedBy]
	return managedBy == esManagedBy
}

func managedByChanged(endpointSlice1, endpointSlice2 *discoveryv1.EndpointSlice) bool {
	return managedByController(endpointSlice1) != managedByController(endpointSlice2)
}

func isReady(ew *ewv1alpha1.ExternalWorkload) bool {
	if len(ew.Status.Conditions) == 0 {
		return false
	}

	// Loop through the conditions and look at each condition in turn starting
	// from the top.
	for i := range ew.Status.Conditions {
		cond := ew.Status.Conditions[i]
		// Stop once we find a 'Ready' condition. We expect a resource to only
		// have one 'Ready' type condition.
		if cond.Type == ewv1alpha1.WorkloadReady && cond.Status == ewv1alpha1.ConditionTrue {
			return true
		}
	}

	return false
}

func toSet(s []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, k := range s {
		set[k] = struct{}{}
	}
	return set
}
