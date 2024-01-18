package externalworkload

import (
	"context"
	"fmt"
	"sort"

	ewv1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	epsliceutil "k8s.io/endpointslice/util"
	utilnet "k8s.io/utils/net"
)

const (
	// Name of the controller. Used as an annotation value for all created
	// EndpointSlice objects
	managedBy = "linkerd-external-workloads-controller"

	// Max number of endpoints per EndpointSlice
	maxEndpointsQuota = 100

	// Max number of ports supported in a Service
	maxEndpointPortsQuota = 100
)

// endpointsReconciler is a subcomponent of the EndpointsController.
//
// Its main responsibility is to reconcile a service's endpoints (by diffing
// states) and keeping track of any drifts between the informer cache and what
// has been written to the API Server
type endpointsReconciler struct {
	k8sAPI *k8s.API
	log    *logging.Entry
	// Upstream utility component that will internally track the most recent
	// resourceVersion observed for an EndpointSlice
	endpointTracker *epsliceutil.EndpointSliceTracker
}

// newEndpointsReconciler takes an API client and returns a reconciler with
// logging and a tracker set-up
func newEndpointsReconciler(k8sAPI *k8s.API) *endpointsReconciler {
	return &endpointsReconciler{
		k8sAPI,
		logging.WithFields(logging.Fields{
			"component": "external-endpoints-reconciler",
		}),
		epsliceutil.NewEndpointSliceTracker(),
	}

}

// === Reconciler ===

// reconcile is the main entry-point for the reconciler's work.
//
// It accepts a slice of external workloads and their corresponding service.
// Optionally, if the controller has previously created any slices for this
// service, these will also be passed in. The reconciler will:
//
// * Determine what address families (AF) the service supports
// * For each address family, it will determine which slices to process (an
// EndpointSlice is specialised and supports only one AF type)
func (r *endpointsReconciler) reconcile(svc *corev1.Service, ews []*ewv1alpha1.ExternalWorkload, es []*discoveryv1.EndpointSlice) error {
	// segment slices by address type.
	// 1. Find service's supported address types
	// 2. Distribute slices based on what address family they are part of
	// 3. If a slice has an address family that is no longer supported, it has
	// to go.
	addrTypes := getSupportedAddressTypes(svc)
	toDelete := []*discoveryv1.EndpointSlice{}
	ipv4Slices := []*discoveryv1.EndpointSlice{}

	for _, slice := range es {
		_, supported := addrTypes[slice.AddressType]
		if !supported {
			toDelete = append(toDelete, slice)
			continue
		}

		// TODO: this could use a test
		if slice.AddressType == discoveryv1.AddressTypeIPv4 {
			ipv4Slices = append(ipv4Slices, slice)
		}
	}

	// TODO (matei): we only process IPv4 for now. IPv6 support will go in here.
	return r.reconcileIPv4Endpoints(svc, ews, ipv4Slices, toDelete)
}

// reconcileIPv4Endpoints operates on a set of external workloads, their
// service, and any endpointslices that have been created by the controller. It
// will compute the diff that needs to be written to the API Server.
func (r *endpointsReconciler) reconcileIPv4Endpoints(svc *corev1.Service, extWorkloads []*ewv1alpha1.ExternalWorkload, epSlices []*discoveryv1.EndpointSlice, toDelete []*discoveryv1.EndpointSlice) error {

	// We start the reconciliation by checking ownerRefs
	//
	// Note: in the upstream implementation, this codepath is different. Instead
	// of appending to a slice of "endpoints to look at", they build a set of
	// endpointslices keyed off the set of ports included in that endpointslice.
	//
	// Each slice may include a maximum of 100 ports. If a service includes more
	// than 100 ports, then the slice won't be able to be filled. As a result,
	// they segment slices by port sets.
	// see: https://github.com/kubernetes/kubernetes/issues/99382
	//
	// We do not segment slices based on ports since we do not support a service
	// with more than 100 ports.
	toReconcile := []*discoveryv1.EndpointSlice{}
	for _, slice := range epSlices {
		// Loop through the endpointslices and figure out which endpointslice
		// does not have an ownerRef set to the service. If a slice has been
		// selected but does not point to the service, we delete it.
		if !ownedBy(slice, svc) {
			toDelete = append(toDelete, slice)
		} else {
			toReconcile = append(toReconcile, slice)
		}
	}

	// Build a list of endpoints we want to create / update. This will be based
	// off the external workloads we have read.
	// We use an EndpointSet from the upstream util library. Each Endpoint we
	// add will be hashed internally.
	desiredEndpoints := epsliceutil.EndpointSet{}

	// A PortMapKey is an upstream type that when created creates a hash of a
	// port list. We use a map to ensure we don't add ports twice (i.e. we add
	// them to a set).
	desiredEndpointPortsSet := map[epsliceutil.PortMapKey][]discoveryv1.EndpointPort{}
	for _, extWorkload := range extWorkloads {
		// We skip workloads with no IPs
		if len(extWorkload.Spec.WorkloadIPs) == 0 {
			continue
		}

		// Find which ports a service selects (or maps to) on an external workload
		ports, err := r.findEndpointPorts(svc, extWorkload)
		// Since we require a workload to document its ports, if a service's
		// targetPort does not match any ports on a workload, we skip it.
		//
		// This is different to the upstream where a container port needn't be
		// documented
		if err != nil {
			r.log.Debugf("skipping workload; failed to remap port for service %s/%s and workload %s/%s: %v", svc.Namespace, svc.Name, extWorkload.Namespace, extWorkload.Name, err)
			continue
		}

		portHash := epsliceutil.NewPortMapKey(ports)
		if _, ok := desiredEndpointPortsSet[portHash]; !ok {
			desiredEndpointPortsSet[portHash] = ports
		}

		ep := externalWorkloadToEndpoint(discoveryv1.AddressTypeIPv4, extWorkload, svc)
		desiredEndpoints.Insert(&ep)
	}

	desiredEndpointPorts := []discoveryv1.EndpointPort{}
	for _, ports := range desiredEndpointPortsSet {
		desiredEndpointPorts = append(desiredEndpointPorts, ports...)
	}

	// If there are any slices whose ports no longer match what we want in our
	// current reconciliation, delete them
	//
	// Note: in the upstream they run some more complicated diffing before
	// applying to ensure creates & deletes turn into updates. We simplify and
	// instead choose to re-create the slice if ports have changed.
	for _, currentSlice := range epSlices {
		portHash := epsliceutil.NewPortMapKey(currentSlice.Ports)
		if _, ok := desiredEndpointPortsSet[portHash]; !ok {
			toDelete = append(toDelete, currentSlice)
		}
	}

	if len(desiredEndpointPorts) > maxEndpointPortsQuota {
		// TODO: we should ensure this is bubbled up and visible to users, not
		// sure the error msg makes sense.
		r.log.Errorf("too many ports detected on the spec")
		// Return nil to avoid requeues
		return nil
	}

	reconResult := r.reconcileEndpoints(svc, toReconcile, desiredEndpoints, desiredEndpointPorts)
	r.log.Debugf("Reconciliation result for %s/%s: %d to add, %d to update, %d to remove", svc.Namespace, svc.Name, len(reconResult.toCreate), len(reconResult.toUpdate), len(reconResult.toDelete))

	// Create EndpointSlices only if the service has not been marked for
	// deletion; according to the upstream implementation not doing so has the
	// potential to cause race conditions
	if svc.DeletionTimestamp == nil {
		// TODO: context with timeout
		for _, slice := range reconResult.toCreate {
			createdSlice, err := r.k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Create(context.TODO(), slice, metav1.CreateOptions{})
			if err != nil {
				// If the namespace  is terminating, operations will not
				// succeed. Drop the entire reconiliation effort
				if errors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
					return nil
				}

				return err
			}
			r.endpointTracker.Update(createdSlice)
		}
	}

	for _, slice := range reconResult.toUpdate {
		updatedSlice, err := r.k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Update(context.TODO(), slice, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		r.endpointTracker.Update(updatedSlice)
	}

	reconResult.toDelete = append(reconResult.toDelete, toDelete...)
	for _, slice := range reconResult.toDelete {
		err := r.k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Delete(context.TODO(), slice.Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
		r.endpointTracker.ExpectDeletion(slice)
	}

	return nil
}

// reconciliationResult is a helper type that bundles together all of the
// updates that need to be performed
type reconciliationResult struct {
	toCreate []*discoveryv1.EndpointSlice
	toUpdate []*discoveryv1.EndpointSlice
	toDelete []*discoveryv1.EndpointSlice
}

// reconcileEndpoints will take a service, a set of slices that apply for that
// service's address family and a list of endpoints that the APIServer should be
// aware of.
//
// It is possible for some of the desired endpoints to already exist, in which
// case, the function computes if they need to be updated.
func (r *endpointsReconciler) reconcileEndpoints(svc *corev1.Service, currentSlices []*discoveryv1.EndpointSlice, desiredEps epsliceutil.EndpointSet, desiredPorts []discoveryv1.EndpointPort) reconciliationResult {
	// This function is heavily inspired by the upstream counterpart with some
	// simplifications around using sets.
	//
	// There are three stages to a reconciliation:
	// 1. Decide what state needs to be deleted / updated.
	// 2. If there are endpoints that do not yet exist, decide if there are any
	// slices that have not met their quota that can hold them.
	// 3. If we still have endpoints that we need to add, write them.
	toDelete := []*discoveryv1.EndpointSlice{}
	toUpdate := []*discoveryv1.EndpointSlice{}
	unchangedSlices := []*discoveryv1.EndpointSlice{}

	// 1. Figure out which endpoints are no longer required in the existing
	// slices, and update endpoints that have changed
	for _, currentSlice := range currentSlices {
		keepEndpoints := []discoveryv1.Endpoint{}
		epUpdated := false
		// Note: we operate with an index to avoid implicit memory aliasing
		for i := range currentSlice.Endpoints {
			found := desiredEps.Get(&currentSlice.Endpoints[i])
			// If the endpoint is desired (i.e. a workload exists with an IP and
			// we want to add it to the service's endpoints), then we should
			// keep it.
			if found != nil {
				keepEndpoints = append(keepEndpoints, *found)
				// We know the slice already contains an endpoint we want, but
				// has the endpoint changed? If yes, we need to persist it
				if !epsliceutil.EndpointsEqualBeyondHash(found, &currentSlice.Endpoints[i]) {
					epUpdated = true
				}

				// Once an endpoint has been found in a slice, we can delete it
				desiredEps.Delete(&currentSlice.Endpoints[i])
			}
		}

		// Re-generate labels and see whether service's labels have changed
		labels, labelsChanged := setEndpointSliceLabels(currentSlice, svc)

		// Consider what kind of reconciliation we should proceed with:
		//
		// 1. We can have a set of endpoints that have changed; this can either
		// mean we need to update the endpoints, or it can also mean we have no
		// endpoints to keep.
		// 2. We need to update the slice's metadata because labels have
		// changed.
		// 3. Slice remains unchanged so we have a noop on our hands
		if epUpdated || len(currentSlice.Endpoints) != len(keepEndpoints) {
			if len(keepEndpoints) == 0 {
				// When there are no endpoints to keep, then the slice should be
				// deleted
				toDelete = append(toDelete, currentSlice)
			} else {
				// There is at least one endpoint to keep / update
				slice := currentSlice.DeepCopy()
				slice.Labels = labels
				slice.Endpoints = keepEndpoints
				toUpdate = append(toUpdate, slice)
			}
		} else if labelsChanged {
			slice := currentSlice.DeepCopy()
			slice.Labels = labels
			toUpdate = append(toUpdate, slice)
		} else {
			// Unchanged, we save it for later.
			// unchanged slices may receive new endpoints that are leftover if
			// they're not past their quotaca
			unchangedSlices = append(unchangedSlices, currentSlice)
		}
	}

	// 2. If we still have desired endpoints left, but they haven't matched any
	// endpoint that already exists in a slice, we need to add it somewhere.
	//
	// We start by adding our leftover endpoints to the list of endpoints we
	// will update anyway (to save a write).
	if desiredEps.Len() > 0 && len(toUpdate) > 0 {
		// Sort in descending order of capacity; fullest first.
		sort.Slice(toUpdate, func(i, j int) bool {
			return len(toUpdate[i].Endpoints) > len(toUpdate[j].Endpoints)
		})

		// Iterate and fill up the slices
		for _, slice := range toUpdate {
			for desiredEps.Len() > 0 && len(slice.Endpoints) < maxEndpointsQuota {
				ep, _ := desiredEps.PopAny()
				slice.Endpoints = append(slice.Endpoints, *ep)
			}
		}
	}

	// Deal with any remaining endpoints by:
	// (a) adding to unchanged slices first
	if desiredEps.Len() > 0 {
		for _, unchangedSlice := range unchangedSlices {
			slice := unchangedSlice.DeepCopy()
			for desiredEps.Len() > 0 && len(slice.Endpoints) < maxEndpointsQuota {
				ep, _ := desiredEps.PopAny()
				slice.Endpoints = append(slice.Endpoints, *ep)
			}
			// Now add it to the list of slices to update since it's been
			// changed
			toUpdate = append(toUpdate, slice)
		}
	}

	// (b) creating new slices second (if needed)
	toCreate := []*discoveryv1.EndpointSlice{}
	for desiredEps.Len() > 0 {
		slice := newEndpointSlice(svc, desiredPorts)
		for desiredEps.Len() > 0 && len(slice.Endpoints) < maxEndpointsQuota {
			ep, _ := desiredEps.PopAny()
			slice.Endpoints = append(slice.Endpoints, *ep)
		}
		toCreate = append(toCreate, slice)

	}

	return reconciliationResult{
		toCreate,
		toUpdate,
		toDelete,
	}

}

// === Utility ===

// Creates a new endpointslice object
func newEndpointSlice(svc *corev1.Service, ports []discoveryv1.EndpointPort) *discoveryv1.EndpointSlice {
	// We need an ownerRef to point to our service
	ownerRef := metav1.NewControllerRef(svc, schema.GroupVersionKind{Version: "v1", Kind: "Service"})
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:    fmt.Sprintf("linkerd-external-%s", svc.Name),
			Namespace:       svc.Namespace,
			Labels:          map[string]string{},
			OwnerReferences: []metav1.OwnerReference{*ownerRef},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   []discoveryv1.Endpoint{},
		Ports:       ports,
	}
	labels, _ := setEndpointSliceLabels(slice, svc)
	slice.Labels = labels
	return slice
}

// setEndpointSliceLabels returns a new map with the new endpoint slice labels,
// and returns true if there was an update.
//
// Slice labels should always be equivalent to Service labels, except for a
// reserved IsHeadlessService, LabelServiceName, and LabelManagedBy. If any
// reserved labels have changed on the service, they are not copied over.
//
// copied from https://github.com/kubernetes/endpointslice/commit/a09c1c9580d13f5020248d25c7fd11f5dde6dd9b
// copyright 2019 The Kubernetes Authors
func setEndpointSliceLabels(es *discoveryv1.EndpointSlice, service *corev1.Service) (map[string]string, bool) {
	isReserved := func(label string) bool {
		if label == discoveryv1.LabelServiceName ||
			label == discoveryv1.LabelManagedBy ||
			label == corev1.IsHeadlessService {
			return true
		}
		return false
	}

	updated := false
	epLabels := make(map[string]string)
	svcLabels := make(map[string]string)

	// check if the endpoint slice and the service have the same labels
	// clone current slice labels except the reserved labels
	for key, value := range es.Labels {
		if isReserved(key) {
			continue
		}
		// copy endpoint slice labels
		epLabels[key] = value
	}

	for key, value := range service.Labels {
		if isReserved(key) {
			continue
		}
		// copy service labels
		svcLabels[key] = value
	}

	// if the labels are not identical update the slice with the corresponding service labels
	for epLabelKey, epLabelVal := range svcLabels {
		svcLabelVal, found := svcLabels[epLabelKey]
		if !found {
			updated = true
			break
		}

		if svcLabelVal != epLabelVal {
			updated = true
			break
		}
	}

	// add or remove headless label depending on the service Type
	if service.Spec.ClusterIP == corev1.ClusterIPNone {
		svcLabels[corev1.IsHeadlessService] = ""
	} else {
		delete(svcLabels, corev1.IsHeadlessService)
	}

	// override endpoint slices reserved labels
	svcLabels[discoveryv1.LabelServiceName] = service.Name
	svcLabels[discoveryv1.LabelManagedBy] = managedBy

	return svcLabels, updated
}

func externalWorkloadToEndpoint(addrType discoveryv1.AddressType, ew *ewv1alpha1.ExternalWorkload, svc *corev1.Service) discoveryv1.Endpoint {
	// Note: an ExternalWorkload does not have the same lifecycle as a pod; we
	// do not mark a workload as "Terminating". Because of that, our code is
	// simpler than the upstream and we never have to consider:
	// * publishNotReadyAddresses (found on a service)
	// * deletionTimestamps (found normally on a pod)
	// * or a terminating flag on the endpoint
	serving := isReady(ew)

	addresses := []string{}
	// We assume the workload has been validated beforehand and contains a valid
	// IP address regardless of its address family.
	for _, addr := range ew.Spec.WorkloadIPs {
		ip := addr.Ip
		isIPv6 := utilnet.IsIPv6String(ip)
		if isIPv6 && addrType == discoveryv1.AddressTypeIPv6 {
			addresses = append(addresses, ip)
		} else if !isIPv6 && addrType == discoveryv1.AddressTypeIPv4 {
			addresses = append(addresses, ip)
		}
	}

	terminating := false
	ep := discoveryv1.Endpoint{
		Addresses: addresses,
		Conditions: discoveryv1.EndpointConditions{
			Ready:       &serving,
			Serving:     &serving,
			Terminating: &terminating,
		},
		TargetRef: &corev1.ObjectReference{
			Kind:      "ExternalWorkload",
			Namespace: ew.Namespace,
			Name:      ew.Name,
			UID:       ew.UID,
		},
	}

	zone, ok := ew.Labels[corev1.LabelTopologyZone]
	if ok {
		ep.Zone = &zone
	}

	// Add a hostname conditionally
	// Note: upstream does this a bit differently; pods may include a hostname
	// as part of their spec. We consider a hostname as long as the service is
	// headless since that's what we would use a hostname for when routing in
	// linkerd (we care about DNS record creation)
	if svc.Spec.ClusterIP == corev1.ClusterIPNone && ew.Namespace == svc.Namespace {
		ep.Hostname = &ew.Name
	}

	return ep
}

func ownedBy(slice *discoveryv1.EndpointSlice, svc *corev1.Service) bool {
	for _, o := range slice.OwnerReferences {
		if o.UID == svc.UID && o.Kind == "Service" && o.APIVersion == "v1" {
			return true
		}
	}
	return false
}

// findEndpointPorts is a utility function that will return a list of ports
// that are documented on an external workload and selected by a service
func (r *endpointsReconciler) findEndpointPorts(svc *corev1.Service, ew *ewv1alpha1.ExternalWorkload) ([]discoveryv1.EndpointPort, error) {
	epPorts := []discoveryv1.EndpointPort{}
	// If we are dealing with a headless service, upstream implementation allows
	// the service not to have any ports
	if len(svc.Spec.Ports) == 0 && svc.Spec.ClusterIP == corev1.ClusterIPNone {
		return epPorts, nil
	}

	for _, svcPort := range svc.Spec.Ports {
		svcPort := svcPort // pin
		portNum, err := findWorkloadPort(ew, &svcPort)
		if err != nil {
			return nil, err
		}

		portName := svcPort.Name
		portProto := svcPort.Protocol
		epPorts = append(epPorts, discoveryv1.EndpointPort{
			Name:     &portName,
			Port:     &portNum,
			Protocol: &portProto,
		})
	}

	return epPorts, nil
}

// findWorkloadPort is provided a service port and an external workload and
// checks whether the workload documents in its spec the target port referenced
// by the service.
//
// adapted from copied from k8s.io/kubernetes/pkg/api/v1/pod
func findWorkloadPort(ew *ewv1alpha1.ExternalWorkload, svcPort *corev1.ServicePort) (int32, error) {
	targetPort := svcPort.TargetPort
	switch targetPort.Type {
	case intstr.String:
		name := targetPort.StrVal
		for _, wPort := range ew.Spec.Ports {
			if wPort.Name == name && wPort.Protocol == svcPort.Protocol {
				return wPort.Port, nil
			}
		}
	case intstr.Int:
		// Ensure the port is documented in the workload spec, since we
		// require it.
		// Upstream version allows for undocumented container ports here (i.e.
		// it returns the int value)
		for _, wPort := range ew.Spec.Ports {
			port := int32(targetPort.IntValue())
			if wPort.Port == port && wPort.Protocol == svcPort.Protocol {
				return port, nil
			}
		}
	}

	return 0, fmt.Errorf("no suitable port for workload %s/%s", ew.Namespace, ew.Name)
}

// getSupportedAddressTypes will return a set of address families (AF) supported
// by this service. A service may be IPv4 or IPv6 only, or it may be dual-stack.
func getSupportedAddressTypes(svc *corev1.Service) map[discoveryv1.AddressType]struct{} {
	afs := map[discoveryv1.AddressType]struct{}{}
	// Field only applies to LoadBalancer, ClusterIP and NodePort services. A
	// headless service will not receive any IP families; it may hold max 2
	// entries and can be mutated (although the 'primary' choice is never
	// removed).
	// See client-go type documentation for more info.
	for _, af := range svc.Spec.IPFamilies {
		if af == corev1.IPv4Protocol {
			afs[discoveryv1.AddressTypeIPv4] = struct{}{}
		} else if af == corev1.IPv6Protocol {
			afs[discoveryv1.AddressTypeIPv6] = struct{}{}
		}
	}

	if len(afs) > 0 {
		// If we appended at least one address family, it means we didn't have
		// to deal with a headless service.
		return afs
	}

	// Note: our logic will differ from the upstream Kubernetes controller.
	// Specifically, our minimum k8s version is greater than v1.20. Upstream
	// controller needs to handle an upgrade path from v1.19 to newer APIs,
	// which we disregard since we can assume all services will see contain the
	// `IPFamilies` field
	//
	// Our only other option is to have a headless service. Our ExternalWorkload
	// CRD is generic over the AF used so we may create slices for both AF_INET
	// and AF_INET6
	afs[discoveryv1.AddressTypeIPv4] = struct{}{}
	afs[discoveryv1.AddressTypeIPv6] = struct{}{}
	return afs
}
