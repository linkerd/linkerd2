package externalworkload

import (
	"context"
	"fmt"
	"sort"

	ewv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	epsliceutil "k8s.io/endpointslice/util"
	utilnet "k8s.io/utils/net"
)

// endpointsReconciler is a subcomponent of the EndpointsController.
//
// Its main responsibility is to reconcile a service's endpoints (by diffing
// states) and keeping track of any drifts between the informer cache and what
// has been written to the API Server
type endpointsReconciler struct {
	k8sAPI         *k8s.API
	log            *logging.Entry
	controllerName string
	// Upstream utility component that will internally track the most recent
	// resourceVersion observed for an EndpointSlice
	endpointTracker *epsliceutil.EndpointSliceTracker
	maxEndpoints    int
	// TODO (matei): add metrics around events
}

// endpointMeta is a helper struct that incldues attributes slices will be
// grouped on (i.e. ports and the address family supported).
//
// Note: this is inspired from the upstream EndpointSlice controller impl.
type endpointMeta struct {
	ports       []discoveryv1.EndpointPort
	addressType discoveryv1.AddressType
}

// newEndpointsReconciler takes an API client and returns a reconciler with
// logging and a tracker set-up
func newEndpointsReconciler(k8sAPI *k8s.API, controllerName string, maxEndpoints int) *endpointsReconciler {
	return &endpointsReconciler{
		k8sAPI,
		logging.WithFields(logging.Fields{
			"component": "external-endpoints-reconciler",
		}),
		controllerName,
		epsliceutil.NewEndpointSliceTracker(),
		maxEndpoints,
	}

}

// === Reconciler ===

// reconcile is the main entry-point for the reconciler's work.
//
// It accepts a slice of external workloads and their corresponding service.
// Optionally, if the controller has previously created any slices for this
// service, these will also be passed in. The reconciler will:
//
// * Determine what address types the service supports
// * For each address type, it will determine which slices to process (an
// EndpointSlice is specialised and supports only one type)
func (r *endpointsReconciler) reconcile(svc *corev1.Service, ews []*ewv1beta1.ExternalWorkload, existingSlices []*discoveryv1.EndpointSlice) error {
	toDelete := []*discoveryv1.EndpointSlice{}
	slicesByAddrType := make(map[discoveryv1.AddressType][]*discoveryv1.EndpointSlice)
	errs := []error{}

	// Get the list of supported address types for the service
	supportedAddrTypes := getSupportedAddressTypes(svc)
	for _, slice := range existingSlices {
		// If a slice has an address type that the service does not support, then
		// it should be deleted
		if _, supported := supportedAddrTypes[slice.AddressType]; !supported {
			toDelete = append(toDelete, slice)
			continue
		}

		// If this is the first time we see this address type, create the list
		// in the set.
		if _, ok := slicesByAddrType[slice.AddressType]; !ok {
			slicesByAddrType[slice.AddressType] = []*discoveryv1.EndpointSlice{}
		}

		slicesByAddrType[slice.AddressType] = append(slicesByAddrType[slice.AddressType], slice)
	}

	// For each supported address type, reconcile endpoint slices that match the
	// given type
	for addrType := range supportedAddrTypes {
		existingSlices := slicesByAddrType[addrType]
		err := r.reconcileByAddressType(svc, ews, existingSlices, addrType)
		if err != nil {
			errs = append(errs, err)
		}
	}

	// delete services whose address type is no longer supported by the service
	for _, slice := range toDelete {
		err := r.k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Delete(context.TODO(), slice.Name, metav1.DeleteOptions{})
		if err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}

// reconcileByAddressType operates on a set of external workloads, their
// service, and any endpointslices that have been created by the controller. It
// will compute the diff that needs to be written to the API Server.
func (r *endpointsReconciler) reconcileByAddressType(svc *corev1.Service, extWorkloads []*ewv1beta1.ExternalWorkload, existingSlices []*discoveryv1.EndpointSlice, addrType discoveryv1.AddressType) error {
	slicesToCreate := []*discoveryv1.EndpointSlice{}
	slicesToUpdate := []*discoveryv1.EndpointSlice{}
	slicesToDelete := []*discoveryv1.EndpointSlice{}

	// We start the reconciliation by checking ownerRefs
	//
	// We follow the upstream here and look at our existing slices and segment
	// by ports.
	existingSlicesByPorts := map[epsliceutil.PortMapKey][]*discoveryv1.EndpointSlice{}
	for _, slice := range existingSlices {
		// Loop through the endpointslices and figure out which endpointslice
		// does not have an ownerRef set to the service. If a slice has been
		// selected but does not point to the service, we delete it.
		if ownedBy(slice, svc) {
			hash := epsliceutil.NewPortMapKey(slice.Ports)
			existingSlicesByPorts[hash] = append(existingSlicesByPorts[hash], slice)
		} else {
			slicesToDelete = append(slicesToDelete, slice)
		}
	}

	// desiredEndpointsByPortMap represents a set of endpoints grouped together
	// by the list of ports they use. These are the endpoints that we will keep
	// and write to the API server.
	desiredEndpointsByPortMap := map[epsliceutil.PortMapKey]epsliceutil.EndpointSet{}
	// desiredMetaByPortMap represents grouping metadata keyed off by the same
	// hashed port list as the endpoints.
	desiredMetaByPortMap := map[epsliceutil.PortMapKey]*endpointMeta{}

	for _, extWorkload := range extWorkloads {
		// We skip workloads with no IPs.
		//
		// Note: workloads only have a 'Ready' status so we do not care about
		// other status conditions.
		if len(extWorkload.Spec.WorkloadIPs) == 0 {
			continue
		}

		// Find which ports a service selects (or maps to) on an external workload
		// Note: we require all workload ports are documented. Pods do not have
		// to document all of their container ports.
		ports := r.findEndpointPorts(svc, extWorkload)
		portHash := epsliceutil.NewPortMapKey(ports)
		if _, ok := desiredMetaByPortMap[portHash]; !ok {
			desiredMetaByPortMap[portHash] = &endpointMeta{ports, addrType}
		}

		if _, ok := desiredEndpointsByPortMap[portHash]; !ok {
			desiredEndpointsByPortMap[portHash] = epsliceutil.EndpointSet{}
		}

		ep := externalWorkloadToEndpoint(addrType, extWorkload, svc)
		if len(ep.Addresses) > 0 {
			desiredEndpointsByPortMap[portHash].Insert(&ep)
		}
	}

	for portKey, desiredEndpoints := range desiredEndpointsByPortMap {
		create, update, del := r.reconcileEndpointsByPortMap(svc, existingSlicesByPorts[portKey], desiredEndpoints, desiredMetaByPortMap[portKey])
		slicesToCreate = append(slicesToCreate, create...)
		slicesToUpdate = append(slicesToUpdate, update...)
		slicesToDelete = append(slicesToDelete, del...)
	}

	// If there are any slices whose ports no longer match what we want in our
	// current reconciliation, delete them
	for portHash, existingSlices := range existingSlicesByPorts {
		if _, ok := desiredEndpointsByPortMap[portHash]; !ok {
			slicesToDelete = append(slicesToDelete, existingSlices...)
		}
	}

	return r.finalize(svc, slicesToCreate, slicesToUpdate, slicesToDelete)
}

// reconcileEndpointsByPortMap will compute the state diff to be written to the
// API Server for a service. The function takes into account any existing
// endpoint slices and any external workloads matched by the service.
// The function works on slices and workloads that have been already grouped by
// a common set of ports.
func (r *endpointsReconciler) reconcileEndpointsByPortMap(svc *corev1.Service, existingSlices []*discoveryv1.EndpointSlice, desiredEps epsliceutil.EndpointSet, desiredMeta *endpointMeta) ([]*discoveryv1.EndpointSlice, []*discoveryv1.EndpointSlice, []*discoveryv1.EndpointSlice) {
	slicesByName := map[string]*discoveryv1.EndpointSlice{}
	sliceNamesUnchanged := map[string]struct{}{}
	sliceNamesToUpdate := map[string]struct{}{}
	sliceNamesToDelete := map[string]struct{}{}

	// 1. Figure out which endpoints are no longer required in the existing
	// slices, and update endpoints that have changed
	for _, existingSlice := range existingSlices {
		slicesByName[existingSlice.Name] = existingSlice
		keepEndpoints := []discoveryv1.Endpoint{}
		epUpdated := false
		for _, endpoint := range existingSlice.Endpoints {
			endpoint := endpoint // pin
			found := desiredEps.Get(&endpoint)
			// If the endpoint is desired (i.e. a workload exists with an IP and
			// we want to add it to the service's endpoints), then we should
			// keep it.
			if found != nil {
				keepEndpoints = append(keepEndpoints, *found)
				// We know the slice already contains an endpoint we want, but
				// has the endpoint changed? If yes, we need to persist it
				if !epsliceutil.EndpointsEqualBeyondHash(found, &endpoint) {
					epUpdated = true
				}

				// Once an endpoint has been found in a slice, we can delete it
				desiredEps.Delete(&endpoint)
			}
		}

		// Re-generate labels and see whether service's labels have changed
		labels, labelsChanged := setEndpointSliceLabels(existingSlice, svc, r.controllerName)

		// Consider what kind of reconciliation we should proceed with:
		//
		// 1. We can have a set of endpoints that have changed; this can either
		// mean we need to update the endpoints, or it can also mean we have no
		// endpoints to keep.
		// 2. We need to update the slice's metadata because labels have
		// changed.
		// 3. Slice remains unchanged so we have a noop on our hands
		if epUpdated || len(existingSlice.Endpoints) != len(keepEndpoints) {
			if len(keepEndpoints) == 0 {
				// When there are no endpoints to keep, then the slice should be
				// deleted
				sliceNamesToDelete[existingSlice.Name] = struct{}{}
			} else {
				// There is at least one endpoint to keep / update
				slice := existingSlice.DeepCopy()
				slice.Labels = labels
				slice.Endpoints = keepEndpoints
				sliceNamesToUpdate[slice.Name] = struct{}{}
				slicesByName[slice.Name] = slice
			}
		} else if labelsChanged {
			slice := existingSlice.DeepCopy()
			slice.Labels = labels
			sliceNamesToUpdate[slice.Name] = struct{}{}
			slicesByName[slice.Name] = slice
		} else {
			// Unchanged, we save it for later.
			// unchanged slices may receive new endpoints that are leftover if
			// they're not past their quotaca
			sliceNamesUnchanged[existingSlice.Name] = struct{}{}
		}
	}

	// 2. If we still have desired endpoints left, but they haven't matched any
	// endpoint that already exists in a slice, we need to add it somewhere.
	//
	// We start by adding our leftover endpoints to the list of endpoints we
	// will update anyway (to save a write).
	if desiredEps.Len() > 0 && len(sliceNamesToUpdate) > 0 {
		slices := []*discoveryv1.EndpointSlice{}
		for sliceName := range sliceNamesToUpdate {
			slices = append(slices, slicesByName[sliceName])
		}

		// Sort in descending order of capacity; fullest first.
		sort.Slice(slices, func(i, j int) bool {
			return len(slices[i].Endpoints) > len(slices[j].Endpoints)
		})

		// Iterate and fill up the slices
		for _, slice := range slices {
			for desiredEps.Len() > 0 && len(slice.Endpoints) < r.maxEndpoints {
				ep, _ := desiredEps.PopAny()
				slice.Endpoints = append(slice.Endpoints, *ep)
			}
		}
	}

	// If we have remaining endpoints, we need to deal with them
	// by using unchanged slices or creating new ones
	slicesToCreate := []*discoveryv1.EndpointSlice{}
	for desiredEps.Len() > 0 {
		var sliceToFill *discoveryv1.EndpointSlice

		// Deal with any remaining endpoints by:
		// (a) adding to unchanged slices first
		if desiredEps.Len() < r.maxEndpoints && len(sliceNamesUnchanged) > 0 {
			unchangedSlices := []*discoveryv1.EndpointSlice{}
			for unchangedSlice := range sliceNamesUnchanged {
				unchangedSlices = append(unchangedSlices, slicesByName[unchangedSlice])
			}

			sliceToFill = getSliceToFill(unchangedSlices, desiredEps.Len(), r.maxEndpoints)
		}

		// If we have no unchanged slice to fill, then
		// (b) create a new slice
		if sliceToFill == nil {
			sliceToFill = newEndpointSlice(svc, desiredMeta, r.controllerName)
		} else {
			// deep copy required to mutate slice
			sliceToFill = sliceToFill.DeepCopy()
			slicesByName[sliceToFill.Name] = sliceToFill
		}

		// Fill out the slice
		for desiredEps.Len() > 0 && len(sliceToFill.Endpoints) < r.maxEndpoints {
			ep, _ := desiredEps.PopAny()
			sliceToFill.Endpoints = append(sliceToFill.Endpoints, *ep)
		}

		// Figure out what kind of slice we just filled and update the diffed
		// state
		if sliceToFill.Name != "" {
			sliceNamesToUpdate[sliceToFill.Name] = struct{}{}
			delete(sliceNamesUnchanged, sliceToFill.Name)
		} else {
			slicesToCreate = append(slicesToCreate, sliceToFill)
		}
	}

	slicesToUpdate := []*discoveryv1.EndpointSlice{}
	for name := range sliceNamesToUpdate {
		slicesToUpdate = append(slicesToUpdate, slicesByName[name])
	}

	slicesToDelete := []*discoveryv1.EndpointSlice{}
	for name := range sliceNamesToDelete {
		slicesToDelete = append(slicesToDelete, slicesByName[name])
	}

	return slicesToCreate, slicesToUpdate, slicesToDelete
}

// finalize performs writes to the API Server to update the state after it's
// been diffed.
func (r *endpointsReconciler) finalize(svc *corev1.Service, slicesToCreate, slicesToUpdate, slicesToDelete []*discoveryv1.EndpointSlice) error {
	// If there are slices to create and delete, change the creates to updates
	// of the slices that would otherwise be deleted.
	for i := 0; i < len(slicesToDelete); {
		if len(slicesToCreate) == 0 {
			break
		}
		sliceToDelete := slicesToDelete[i]
		slice := slicesToCreate[len(slicesToCreate)-1]
		// Only update EndpointSlices that are owned by this Service and have
		// the same AddressType. We need to avoid updating EndpointSlices that
		// are being garbage collected for an old Service with the same name.
		// The AddressType field is immutable. Since Services also consider
		// IPFamily immutable, the only case where this should matter will be
		// the migration from IP to IPv4 and IPv6 AddressTypes, where there's a
		// chance EndpointSlices with an IP AddressType would otherwise be
		// updated to IPv4 or IPv6 without this check.
		if sliceToDelete.AddressType == slice.AddressType && ownedBy(sliceToDelete, svc) {
			slice.Name = sliceToDelete.Name
			slicesToCreate = slicesToCreate[:len(slicesToCreate)-1]
			slicesToUpdate = append(slicesToUpdate, slice)
			slicesToDelete = append(slicesToDelete[:i], slicesToDelete[i+1:]...)
		} else {
			i++
		}
	}

	r.log.Debugf("reconciliation result for %s/%s: %d to add, %d to update, %d to remove", svc.Namespace, svc.Name, len(slicesToCreate), len(slicesToUpdate), len(slicesToDelete))

	// Create EndpointSlices only if the service has not been marked for
	// deletion; according to the upstream implementation not doing so has the
	// potential to cause race conditions
	if svc.DeletionTimestamp == nil {
		// TODO: context with timeout
		for _, slice := range slicesToCreate {
			r.log.Tracef("starting create: %s/%s", slice.Namespace, slice.Name)
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
			r.log.Tracef("finished creating: %s/%s", createdSlice.Namespace, createdSlice.Name)
		}
	}

	for _, slice := range slicesToUpdate {
		r.log.Tracef("starting update: %s/%s", slice.Namespace, slice.Name)
		updatedSlice, err := r.k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Update(context.TODO(), slice, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		r.endpointTracker.Update(updatedSlice)
		r.log.Tracef("finished updating: %s/%s", updatedSlice.Namespace, updatedSlice.Name)
	}

	for _, slice := range slicesToDelete {
		r.log.Tracef("starting delete: %s/%s", slice.Namespace, slice.Name)
		err := r.k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Delete(context.TODO(), slice.Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
		r.endpointTracker.ExpectDeletion(slice)
		r.log.Tracef("finished deleting: %s/%s", slice.Namespace, slice.Name)
	}

	return nil
}

// === Utility ===

// Creates a new endpointslice object
func newEndpointSlice(svc *corev1.Service, meta *endpointMeta, controllerName string) *discoveryv1.EndpointSlice {
	// We need an ownerRef to point to our service
	ownerRef := metav1.NewControllerRef(svc, schema.GroupVersionKind{Version: "v1", Kind: "Service"})
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:    fmt.Sprintf("linkerd-external-%s-", svc.Name),
			Namespace:       svc.Namespace,
			Labels:          map[string]string{},
			OwnerReferences: []metav1.OwnerReference{*ownerRef},
		},
		AddressType: meta.addressType,
		Endpoints:   []discoveryv1.Endpoint{},
		Ports:       meta.ports,
	}
	labels, _ := setEndpointSliceLabels(slice, svc, controllerName)
	slice.Labels = labels
	return slice
}

// getSliceToFill will return an endpoint slice from a list of endpoint slices
// whose capacity is closest to being full when numEndpoints are added. If no
// slice fits the criteria a nil pointer is returned
func getSliceToFill(slices []*discoveryv1.EndpointSlice, numEndpoints, maxEndpoints int) *discoveryv1.EndpointSlice {
	closestDiff := maxEndpoints
	var closestSlice *discoveryv1.EndpointSlice
	for _, slice := range slices {
		diff := maxEndpoints - (numEndpoints + len(slice.Endpoints))
		if diff >= 0 && diff < closestDiff {
			closestDiff = diff
			closestSlice = slice
			if closestDiff == 0 {
				return closestSlice
			}
		}
	}
	return closestSlice
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
func setEndpointSliceLabels(es *discoveryv1.EndpointSlice, service *corev1.Service, controllerName string) (map[string]string, bool) {
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
	for svcLabelKey, svcLabelVal := range svcLabels {
		epLabelVal, found := epLabels[svcLabelKey]
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
	svcLabels[discoveryv1.LabelManagedBy] = controllerName

	return svcLabels, updated
}

func externalWorkloadToEndpoint(addrType discoveryv1.AddressType, ew *ewv1beta1.ExternalWorkload, svc *corev1.Service) discoveryv1.Endpoint {
	// Note: an ExternalWorkload does not have the same lifecycle as a pod; we
	// do not mark a workload as "Terminating". Because of that, our code is
	// simpler than the upstream and we never have to consider:
	// * publishNotReadyAddresses (found on a service)
	// * deletionTimestamps (found normally on a pod)
	// * or a terminating flag on the endpoint
	serving := IsEwReady(ew)

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
func (r *endpointsReconciler) findEndpointPorts(svc *corev1.Service, ew *ewv1beta1.ExternalWorkload) []discoveryv1.EndpointPort {
	epPorts := []discoveryv1.EndpointPort{}
	// If we are dealing with a headless service, upstream implementation allows
	// the service not to have any ports
	if len(svc.Spec.Ports) == 0 && svc.Spec.ClusterIP == corev1.ClusterIPNone {
		return epPorts
	}

	for _, svcPort := range svc.Spec.Ports {
		svcPort := svcPort // pin
		portNum, err := findWorkloadPort(ew, &svcPort)
		if err != nil {
			r.log.Errorf("failed to find port for service %s/%s: %v", svc.Namespace, svc.Name, err)
			continue
		}

		portName := &svcPort.Name
		if *portName == "" {
			portName = nil
		}
		portProto := &svcPort.Protocol
		if *portProto == "" {
			portProto = nil
		}
		epPorts = append(epPorts, discoveryv1.EndpointPort{
			Name:     portName,
			Port:     &portNum,
			Protocol: portProto,
		})
	}

	return epPorts
}

// findWorkloadPort is provided a service port and an external workload and
// checks whether the workload documents in its spec the target port referenced
// by the service.
//
// adapted from copied from k8s.io/kubernetes/pkg/api/v1/pod
func findWorkloadPort(ew *ewv1beta1.ExternalWorkload, svcPort *corev1.ServicePort) (int32, error) {
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
		// it returns the int value).
		for _, wPort := range ew.Spec.Ports {
			port := int32(targetPort.IntValue())
			if wPort.Port == port && wPort.Protocol == svcPort.Protocol {
				return port, nil
			}
		}
	}
	return 0, fmt.Errorf("no suitable port for targetPort %s on workload %s/%s", targetPort.String(), ew.Namespace, ew.Name)
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
