package watcher

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/davecgh/go-spew/spew"
	eev1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/externalendpoint/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"k8s.io/apimachinery/pkg/util/intstr"
)

/*
var log = logging.WithFields(logging.Fields{
	"component": "server",
})
*/

const ManagedByController = "linkerd-alien-watcher"

type ServiceKeySet = map[string]struct{}

// ExternalReconciler transforms current EndpointSlice state into a desired
// state by indexing and reacting to changes in services and externalendpoint
// objects
type ExternalReconciler struct {
	k8sAPI  *k8s.API
	log     *logging.Entry
	updates chan string
	stop    chan struct{}
}

func NewExternalReconciler(k8sAPI *k8s.API, stopCh chan struct{}) (*ExternalReconciler, error) {
	er := &ExternalReconciler{
		k8sAPI:  k8sAPI,
		updates: make(chan string, 200),
		stop:    stopCh,
		log: logging.WithFields(logging.Fields{
			"component": "external-reconciler",
		}),
	}

	_, err := k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    er.addService,
		DeleteFunc: er.deleteService,
		UpdateFunc: er.updateService,
	})
	if err != nil {
		return nil, err
	}

	_, err = k8sAPI.EE().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    er.addExternal,
		DeleteFunc: er.deleteExternal,
		UpdateFunc: er.updateExternal,
	})

	return er, nil
}

func (er *ExternalReconciler) Start() {
	go func() {
		for {
			select {
			case update := <-er.updates:
				er.processUpdate(update)
			case <-er.stop:
				return
			}
		}
	}()
}

func (er *ExternalReconciler) processUpdate(key string) {
	// Upstream tracks how long it took to process. Thought this was cool and I
	// added it in
	startTime := time.Now()
	defer func() {
		er.log.Infof("Finished syncing endpoint slices for service %s. Elapsed %d", key, time.Since(startTime))
	}()

	// Use provide cache function to compute ns and name from key
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		er.log.Errorf("Failed to process key %s: %v", key, err)
		return
	}

	svc, err := er.k8sAPI.Svc().Lister().Services(ns).Get(name)
	if err != nil {
		if !kerrors.IsNotFound(err) {
			er.log.Errorf("Failed to process update; key %s not found", key)
		}

		// Delete service here. Issue appropriate requests (if any).
		// EndpointSlices have ownerRefs so they should be deleted automatically
		return
	}

	if svc.Spec.Selector == nil {
		// Should really be debug but its to noisy to run during tests
		er.log.Infof("Skipped update; service %s has no selector", key)
		return
	}

	if svc.Spec.Type != v1.ServiceTypeClusterIP {
		er.log.Infof("Skipped update; service %s has incompatible type %s", svc.Spec.Type)
		return
	}

	er.log.Infof("Updating EndpointSlices for %s", key)
	// No validation for labels. Assume API Server takes care of that (e.g.
	// client side validation when creating the svc)
	selector := labels.Set(svc.Spec.Selector).AsSelectorPreValidated()
	// Get all external endpoints selected over by this svc
	ees, err := er.k8sAPI.EE().Lister().List(selector)
	if err != nil {
		er.log.Errorf("Failed to list ExternalEndpoints for %s: %v", key, err)
		return
	}

	sliceSelector := labels.Set(map[string]string{
		discovery.LabelServiceName: svc.Name,
		discovery.LabelManagedBy:   ManagedByController,
	}).AsSelectorPreValidated()
	slices, err := er.k8sAPI.ES().Lister().List(sliceSelector)
	if err != nil {
		er.log.Errorf("Failed to list EndpointSlices for %s: %v", key, err)
		return
	}

	// Shortcut: we'd have to check whether slices have been marked for deletion
	// (GC) and some other edge cases that might result in races and/or
	// staleness. At least upstream does

	// Only consider IPv4 for now. Dual-stack would require us to add some more
	// logic (slices can be one or the other unlike Endpoints which are dual
	// stack)
	// NOTE: we'd need to perhaps ensure our address types for the svc are
	// compatible with this

	deleteSlices := make(map[string]struct{})
	createSlices := []*discovery.EndpointSlice{}
	updateSlices := []*discovery.EndpointSlice{}

	// NOTE: in upstream they hash over ports and then build a set where each
	// port hash has a corresponding slice.
	//
	// a service can expose multiple ports, and there is a limit to the number
	// of entries you can have in a slice. Slices can be created per port, so
	// it's important to keep it in mind.
	//
	// We simplify here and don't do any of that.
	// see: https://github.com/kubernetes/api/blob/master/discovery/v1/types.go#L169
	// also this issue: https://github.com/kubernetes/kubernetes/issues/99382
	// (service with 100 ports wth)
	//
	// Also, a service can select over any number of pods. If a port has:
	//	p1: 8080
	//	p2: 9090
	//
	//	And we have one pod exposing p1 and another exposing p2, they _could_ be
	//	logically grouped into separate slices. In my experiments that was not
	//	the case...
	//
	//	For the prototype we acknowledge
	//	this but treat all workloads as exposing the same set of ports.

	// Ports!
	portsMap := make(map[int32]discovery.EndpointPort)
	epSet := make(map[endpointHash]*discovery.Endpoint)
	for _, ee := range ees {
		er.log.Infof("Looking at workload %s/%s", ee.Namespace, ee.Name)
		// Don't bother if it's terminmating
		if !ShouldInclude(ee) {
			continue

		}

		for i := range svc.Spec.Ports {
			svcPort := &svc.Spec.Ports[i]
			// Ignore port names for now, treat everything as an integer
			proto := svcPort.Protocol
			portNum, err := getPort(ee, svcPort)
			if err != nil {
				er.log.Errorf("Failed to get port when updating slice for %s: %v", key, err)
			}

			i32Port := int32(portNum)
			portsMap[i32Port] = discovery.EndpointPort{
				Name:        &svcPort.Name,
				Port:        &i32Port,
				Protocol:    &proto,
				AppProtocol: svcPort.AppProtocol,
			}

		}

		// Create endpoint
		serving := IsReady(ee)
		terminating := true
		// Would also have to check 'Terminating' here, usually done through a
		// deletion timestamp

		// Ready should never be "true" if a pod is terminating unless 'publishNotReadyAddresses'
		// is set. We don't have a way to signal 'Terminating' only 'Deleted'.
		// ready := svc.Spec.PublishNotReadyAddresses || (serving &&
		// !terminating)

		addresses := []string{}
		for _, addr := range ee.Spec.WorkloadIPs {
			addresses = append(addresses, addr.Ip)
		}

		// There are more fields to fill out here, e.g. topology, hostnames,
		// etc.
		ep := discovery.Endpoint{
			Addresses: addresses,
			Conditions: discovery.EndpointConditions{
				Ready:       &serving,
				Serving:     &serving,
				Terminating: &terminating,
			},
			TargetRef: &v1.ObjectReference{
				Kind:      "ExternalEndpoint",
				Namespace: ee.Namespace,
				Name:      ee.Name,
				UID:       ee.UID,
			},
		}
		hash := hashEndpoint(&ep)
		er.log.Infof("Hashed %s/%s to %s", ee.Namespace, ee.Name, hash)
		epSet[hash] = &ep
	}

	ports := []discovery.EndpointPort{}
	for _, val := range portsMap {
		ports = append(ports, val)
	}

	visitedSlices := []*discovery.EndpointSlice{}
	for _, slice := range slices {
		visitedSlices = append(visitedSlices, slice)
		updatedEndpoints := []discovery.Endpoint{}
		for _, ep := range slice.Endpoints {
			visitedHash := hashEndpoint(&ep)
			visited, ok := epSet[visitedHash]
			if !ok {
				// Remove later
				continue
			}

			updatedEndpoints = append(updatedEndpoints, *visited)
			// There are some smarter things we can do here such as comparing
			// the states to see whether changes _have_ to be written. For now,
			// just write it all

			// If an endpoint's been found, we can "mark it as visited", i.e.
			// yeet it out
			er.log.Infof("Visited %s", visitedHash)
			delete(epSet, visitedHash)
		}

		// Clone service labels on to endpoint slice labels. We only check if
		// managed-by is on since it's important.
		labels := map[string]string{
			discovery.LabelManagedBy:   ManagedByController,
			discovery.LabelServiceName: svc.Name,
		}

		if len(updatedEndpoints) != len(slice.Endpoints) {
			if len(updatedEndpoints) == 0 {
				// Delete
				deleteSlices[slice.Name] = struct{}{}
				continue
			}

			updatedSlice := slice.DeepCopy()
			updatedSlice.Labels = labels
			updatedSlice.Endpoints = updatedEndpoints
			updateSlices = append(updateSlices, updatedSlice)
		}

	}

	if len(epSet) > 0 && len(updateSlices) > 0 {
		// We'd have to figure out limits here, for now just add to the first
		// one. We won't hit 1k endpoints in the prototype just yet
		slice := updateSlices[0]
		for k, v := range epSet {
			slice.Endpoints = append(slice.Endpoints, *v)
			er.log.Infof("Deleting hash %s from set (update slice)", k)
			delete(epSet, k)
		}
	}

	if len(epSet) > 0 && len(visitedSlices) > 0 {
		slice := visitedSlices[0]
		for k, v := range epSet {
			slice.Endpoints = append(slice.Endpoints, *v)
			er.log.Infof("Deleting hash %s from set (visited slice)", k)
			delete(epSet, k)
		}
		updateSlices = append(updateSlices, slice)

	} else if len(epSet) > 0 {
		// Either we have hit our cap (impossible we don't check it) or we never had
		// a slice to begin with!
		ownerRef := metav1.NewControllerRef(svc, schema.GroupVersionKind{Version: "v1", Kind: "Service"})
		sliceToCreate := discovery.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName:               fmt.Sprintf("%s-", svc.Name),
				Namespace:                  svc.Namespace,
				CreationTimestamp:          metav1.Time{},
				DeletionTimestamp:          &metav1.Time{},
				DeletionGracePeriodSeconds: new(int64),
				Labels: map[string]string{
					discovery.LabelManagedBy:   ManagedByController,
					discovery.LabelServiceName: svc.Name,
				},
				Annotations:     map[string]string{},
				OwnerReferences: []metav1.OwnerReference{*ownerRef},
			},
			AddressType: discovery.AddressTypeIPv4,
			Endpoints:   []discovery.Endpoint{},
			Ports:       ports,
		}
		for k, v := range epSet {
			sliceToCreate.Endpoints = append(sliceToCreate.Endpoints, *v)
			er.log.Infof("Deleting hash %s from set (create slice)", k)
			delete(epSet, k)
		}

		createSlices = append(createSlices, &sliceToCreate)
	}

	er.log.Infof("Reconciliation final step; added %d - deleted %d - updated %d", len(createSlices), len(updateSlices), len(deleteSlices))
	for name := range deleteSlices {
		er.log.Infof("Deleting slice %s", name)
		err := er.k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
		if err != nil {
			er.log.Errorf("Failed to delete slice %s/%s: %v", svc.Namespace, name, err)
		}
	}

	for _, slice := range updateSlices {
		er.log.Infof("Updating slice %s", slice.Name)
		_, err := er.k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Update(context.TODO(), slice, metav1.UpdateOptions{})
		if err != nil {
			er.log.Errorf("Failed to update slice %s/%s: %s", svc.Namespace, slice.Name)
			er.log.Infof("Slice %v", slice)
		}
	}

	for _, slice := range createSlices {
		er.log.Infof("Creating slice %s", slice.Name)
		_, err := er.k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Create(context.TODO(), slice, metav1.CreateOptions{})
		if err != nil {
			er.log.Errorf("Failed to create slice %s/%s: %s", svc.Namespace, slice.Name, err)
			er.log.Infof("Slice %v", slice)
		}
	}

}

// === Callbacks ===

func (er *ExternalReconciler) addService(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		er.log.Infof("Failed to get key for obj %v: %v", obj, err)
	}

	// WARNING: if channel is full this blocks
	er.updates <- key

}

func (er *ExternalReconciler) updateService(_, newS interface{}) {
	er.addService(newS)
}

func (er *ExternalReconciler) deleteService(obj interface{}) {
	er.addService(obj)
}

func (er *ExternalReconciler) addExternal(obj interface{}) {
	ee := obj.(*eev1alpha1.ExternalEndpoint)
	services, err := GetServiceMembership(er.k8sAPI.Svc().Lister(), ee)
	if err != nil {
		er.log.Errorf("Failed to get service membership for %s/%s: %v", ee.Namespace, ee.Name, err)
	}

	// WARNING: if channel is full this blocks
	for key := range services {
		er.updates <- key
	}
}

func (er *ExternalReconciler) updateExternal(oldE, newE interface{}) {
	old := oldE.(*eev1alpha1.ExternalEndpoint)
	cur := newE.(*eev1alpha1.ExternalEndpoint)
	if cur.ResourceVersion == old.ResourceVersion {
		// Periodic resyncs send updates, nothing to see here. But investigate
		// when we do the real thing maybe
		return
	}

	if !hasMembershipChanged(er.log, old, cur) && !hasEndpointChanged(old, cur) {
		return
	}

	services, err := GetServiceMembership(er.k8sAPI.Svc().Lister(), cur)
	if err != nil {
		er.log.Errorf("Failed to get service membership for %s/%s: %v", cur.Namespace, cur.Name, err)
	}

	// WARNING: if channel is full this blocks
	for key := range services {
		er.updates <- key
	}

}

func (er *ExternalReconciler) deleteExternal(obj interface{}) {
	var ee *eev1alpha1.ExternalEndpoint
	if ee, ok := obj.(*eev1alpha1.ExternalEndpoint); ok {
		er.addExternal(ee)
		return
	}

	tomb, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		return
	}

	ee, ok = tomb.Obj.(*eev1alpha1.ExternalEndpoint)
	if !ok {
		return
	}

	er.addExternal(ee)

}

// === Util functions ===

// Uniquely identify an endpoint. According to upstream, only including a subset
// of properties (e.g. targetRefs and addresses) optimised for in-place updates
// if topology / conditions change
type endpointHash string
type hashObj struct {
	Addresses []string
	Name      string
	Namespace string
}

// Assume TargetRef is always present
func hashEndpoint(endpoint *discovery.Endpoint) endpointHash {
	sort.Strings(endpoint.Addresses)
	obj := hashObj{Addresses: endpoint.Addresses, Name: endpoint.TargetRef.Name, Namespace: endpoint.TargetRef.Namespace}
	return endpointHash(deepHashObj(obj))
}

// copied from k8s.io/kubernetes/pkg/util/hash
//
// writes object to hash using spew library (follows pointers and prints values
// of nested objects => hash won't change when a pointer does)
func deepHashObj(obj interface{}) string {
	hasher := md5.New()
	hasher.Reset()
	printer := spew.ConfigState{
		Indent:         " ",
		DisableMethods: true,
		SortKeys:       true,
		SpewKeys:       true,
	}
	printer.Fprintf(hasher, "%#v", obj)
	return hex.EncodeToString(hasher.Sum(nil)[0:])
}

// GetServiceMembership uses a cached lister to retrieve all services in a
// namespace that match a given ExternalEndpoint
func GetServiceMembership(lister v1listers.ServiceLister, ee *eev1alpha1.ExternalEndpoint) (ServiceKeySet, error) {
	set := make(map[string]struct{})
	services, err := lister.Services(ee.Namespace).List(labels.Everything())
	if err != nil {
		return set, err
	}

	for _, svc := range services {
		if svc.Spec.Selector == nil {
			continue
		}

		// Taken from official k8s code, this checks whether a given object has
		// a deleted state before returning a `namespace/name` key. This is
		// important since we do not want to consider a service that has been
		// deleted and is waiting for cache eviction
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(svc)
		if err != nil {
			return map[string]struct{}{}, err
		}

		// Check if service selects our ee.
		if labels.ValidatedSetSelector(svc.Spec.Selector).Matches(labels.Set(ee.Labels)) {
			set[key] = struct{}{}
		}
	}

	return set, nil
}

// ShouldInclude returns true if an external endpoint should be part of the
// EndpointSlice object. Generally, we're concerned with status checksh ere.
// NOTE (matei): upstream they also have the option to include _terminating_
// pods.
func ShouldInclude(ee *eev1alpha1.ExternalEndpoint) bool {
	// if we have a 'Terminating' condition, it needs to be included here. For
	// example, pods that belong to jobs would have a 'Succeeded' phase.
	// ExternalEndpoints don't have that.
	//
	if len(ee.Spec.WorkloadIPs) == 0 {
		return false
	}

	// For simplicity, assume we have a condition that tells us the endpoint has
	// to be deleted. We need  to figure out how to delete & gc these resources.
	if IsDeleted(ee) {
		return false
	}

	return true
}

func hasEndpointChanged(oldE, newE *eev1alpha1.ExternalEndpoint) bool {
	if IsDeleted(oldE) != IsDeleted(newE) {
		// Something has changed, rm from 'Ready'
		return true
	}

	if IsReady(oldE) != IsReady(newE) {
		return true
	}

	// if len(oldE.Spec.WorkloadIPs) != len(newE.Spec.WorkloadIPs)  => No IPs
	// changed at runtime.
	//
	// Should also check identity? Unclear _what_ can change at runtime yet
	return false
}

func hasMembershipChanged(l *logging.Entry, oldE, newE *eev1alpha1.ExternalEndpoint) bool {
	// Resisting urge to use runtime reflection
	if len(newE.Labels) != len(oldE.Labels) {
		return false
	}

	for k, v := range newE.Labels {
		l.Infof("Looking at ExternalEndpoint %s/%s labels: %s=%s", newE.Namespace, newE.Name, k, v)
		if oldV, ok := oldE.Labels[k]; ok {
			if v != oldV {
				l.Infof("Old EE %s/%s labels don't match: %s!=%s", oldE.Namespace, oldE.Name, oldV, v)
				return false
			}
		} else {
			l.Infof("Old EE %s/%s labels has no key %s", oldE.Namespace, oldE.Name, k)
			return false
		}
	}

	return true
}

func IsReady(ee *eev1alpha1.ExternalEndpoint) bool {
	cond := getReadyCondition(&ee.Status)
	return cond != nil && cond.Status == eev1alpha1.ConditionTrue
}

func IsDeleted(ee *eev1alpha1.ExternalEndpoint) bool {
	cond := getDeletedCondition(&ee.Status)
	return cond != nil && cond.Status == eev1alpha1.ConditionTrue
}

func getReadyCondition(status *eev1alpha1.ExternalEndpointStatus) *eev1alpha1.WorkloadCondition {
	if status == nil || status.Conditions == nil {
		return nil
	}

	for i := range status.Conditions {
		if status.Conditions[i].Type == eev1alpha1.WorkloadReady {
			return &status.Conditions[i]
		}
	}

	return nil
}

func getDeletedCondition(status *eev1alpha1.ExternalEndpointStatus) *eev1alpha1.WorkloadCondition {
	if status == nil || status.Conditions == nil {
		return nil
	}

	for i := range status.Conditions {
		if status.Conditions[i].Type == eev1alpha1.WorkloadDeleted {
			return &status.Conditions[i]
		}
	}

	return nil
}

// Credit to upstream
func ownedBy(endpointSlice *discovery.EndpointSlice, svc *v1.Service) bool {
	for _, o := range endpointSlice.OwnerReferences {
		if o.UID == svc.UID && o.Kind == "Service" && o.APIVersion == "v1" {
			return true
		}
	}
	return false
}

// Mostly here to get us named ports
func getPort(ee *eev1alpha1.ExternalEndpoint, svcPort *v1.ServicePort) (int, error) {
	switch svcPort.TargetPort.Type {
	case intstr.String:
		// We'd have to loop and find container(s) with named port. Skip
		return 0, errors.New("named ports are todo!()")
	case intstr.Int:
		return svcPort.TargetPort.IntValue(), nil
	}

	return 0, fmt.Errorf("no suitable port for ee %s/%s", ee.Namespace, ee.Name)
}
