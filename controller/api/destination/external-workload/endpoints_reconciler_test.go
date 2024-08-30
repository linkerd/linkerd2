package externalworkload

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	ewv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	"github.com/linkerd/linkerd2/controller/k8s"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
	epsliceutil "k8s.io/endpointslice/util"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/yaml"
)

type IP struct {
	addressType discoveryv1.AddressType
	ip          string
}

var (
	httpUnnamedPort = corev1.ServicePort{
		Port: 8080,
		TargetPort: intstr.IntOrString{
			Type:   intstr.Int,
			IntVal: 8080,
		},
	}

	httpNamedPort = corev1.ServicePort{
		TargetPort: intstr.IntOrString{
			Type:   intstr.String,
			StrVal: "http",
		},
	}

	defaultTestEndpointsQuota = 100

	testControllerName = "test-controller"
)

// === Test create / update / delete ===

// Test that when a service has no endpointslices written to the API Server, reconciling
// with a workload will create new endpointslices (one per IP family)
func TestReconcilerCreatesNewEndpointSlices(t *testing.T) {
	// We do not need to receive anything through the informers so
	// create a client with no cached resources
	k8sAPI, err := k8s.NewFakeAPI([]string{}...)
	if err != nil {
		t.Fatalf("unexpected error when creating Kubernetes clientset: %v", err)
	}

	for _, tc := range []struct {
		app      string
		families []corev1.IPFamily
		IPs      []IP
	}{
		{
			"testIPv4",
			[]corev1.IPFamily{corev1.IPv4Protocol},
			[]IP{
				{discoveryv1.AddressTypeIPv4, "192.0.2.0"},
			},
		},
		{
			"testIPv6",
			[]corev1.IPFamily{corev1.IPv6Protocol},
			[]IP{
				{discoveryv1.AddressTypeIPv6, "2001:db8::8a2e:370:7334"},
			},
		},
		{
			"testDualStack",
			[]corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			[]IP{
				{discoveryv1.AddressTypeIPv4, "192.0.2.0"},
				{discoveryv1.AddressTypeIPv6, "2001:db8::8a2e:370:7334"},
			},
		},
	} {
		t.Run(tc.app, func(t *testing.T) {
			svc := makeService(tc.app, tc.families, map[string]string{"app": tc.app}, []corev1.ServicePort{httpUnnamedPort}, "")

			IPs := []string{}
			for _, ip := range tc.IPs {
				IPs = append(IPs, ip.ip)
			}
			ew := makeExternalWorkload("1", "wlkd-"+tc.app, map[string]string{"app": ""}, map[int32]string{8080: ""}, IPs)

			r := newEndpointsReconciler(k8sAPI, testControllerName, defaultTestEndpointsQuota)
			err = r.reconcile(svc, []*ewv1beta1.ExternalWorkload{ew}, nil)
			if err != nil {
				t.Fatalf("unexpected error when reconciling endpoints: %v", err)
			}

			endpointSlices := fetchEndpointSlices(t, k8sAPI, svc)
			if len(endpointSlices) != len(tc.families) {
				t.Fatalf("expected %d endpointslices after reconciliation, got %d instead", len(tc.families), len(endpointSlices))
			}

			for _, ip := range tc.IPs {
				expectedEndpoint := makeEndpoint([]string{ip.ip}, true, ew)

				var matchingES *discoveryv1.EndpointSlice
				for _, es := range endpointSlices {
					es := es
					if es.AddressType == ip.addressType {
						matchingES = &es
						break
					}
				}
				if matchingES == nil {
					t.Fatalf("expected to find endpointslice for IP family %s, but none was found", ip.addressType)
				}

				if len(matchingES.Endpoints) != 1 {
					t.Fatalf("expected %d endpointslices endpoints after reconciliation, got %d instead", 1, len(matchingES.Endpoints))
				}

				ep := matchingES.Endpoints[0]
				diffEndpoints(t, ep, expectedEndpoint)
			}
		})
	}
}

// Test that when a service has no endpointslices written to the API Server, reconciling
// with a workload will create a new endpointslice. Since it is a headless
// service, we will also get a hostname
func TestReconcilerCreatesNewEndpointSliceHeadless(t *testing.T) {
	// We do not need to receive anything through the informers so
	// create a client with no cached resources
	k8sAPI, err := k8s.NewFakeAPI([]string{}...)
	if err != nil {
		t.Fatalf("unexpected error when creating Kubernetes clientset: %v", err)
	}

	svc := makeService("test-svc", []corev1.IPFamily{corev1.IPv4Protocol}, map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "")
	svc.Spec.ClusterIP = corev1.ClusterIPNone
	ew := makeExternalWorkload("1", "wlkd-1", map[string]string{"app": ""}, map[int32]string{8080: ""}, []string{"192.0.2.0"})
	ew.Namespace = "default"
	ew.ObjectMeta.UID = types.UID(fmt.Sprintf("%s-%s", ew.Namespace, ew.Name))

	r := newEndpointsReconciler(k8sAPI, testControllerName, defaultTestEndpointsQuota)
	err = r.reconcile(svc, []*ewv1beta1.ExternalWorkload{ew}, nil)
	if err != nil {
		t.Fatalf("unexpected error when reconciling endpoints: %v", err)
	}

	expectedEndpoint := makeEndpoint([]string{"192.0.2.0"}, true, ew)
	es := fetchEndpointSlices(t, k8sAPI, svc)
	if len(es) != 1 {
		t.Fatalf("expected %d endpointslices after reconciliation, got %d instead", 1, len(es))
	}

	if len(es[0].Endpoints) != 1 {
		t.Fatalf("expected %d endpointslices after reconciliation, got %d instead", 1, len(es[0].Endpoints))
	}

	if es[0].AddressType != discoveryv1.AddressTypeIPv4 {
		t.Fatalf("expected endpointslice to have AF %s, got %s instead", discoveryv1.AddressTypeIPv4, es[0].AddressType)
	}
	ep := es[0].Endpoints[0]
	diffEndpoints(t, ep, expectedEndpoint)

	if _, ok := es[0].Labels[corev1.IsHeadlessService]; !ok {
		t.Errorf("expected \"%s\" label to be present on the service", corev1.IsHeadlessService)
	}

	if ep.Hostname == nil {
		t.Fatalf("expected endpoint to have a hostname")
	}

	if *ep.Hostname != ew.Name {
		t.Errorf("expected \"%s\" as a hostname, got: %s", ew.Name, *ep.Hostname)
	}

}

// Test that when a service has an endpointslice written to the API Server,
// reconciling with the two workloads updates the endpointslice
func TestReconcilerUpdatesEndpointSlice(t *testing.T) {
	// Create a service
	svc := makeService("test-svc", []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol}, map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "")

	// Create our existing workload
	ewCreated := makeExternalWorkload("1", "wlkd-1", map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{"192.0.2.1", "2001:db8::8a2e:370:7333"})

	// Create endpointslices for IPv4 and IPv6
	port := int32(8080)
	ports := []discoveryv1.EndpointPort{{
		Port: &port,
	}}
	esIPv4, esIPv6 := makeDualEndpointSlices(svc, ports)
	endpointsIPv4 := []discoveryv1.Endpoint{externalWorkloadToEndpoint(discoveryv1.AddressTypeIPv4, ewCreated, svc)}
	esIPv4.Endpoints = endpointsIPv4
	esIPv4.Generation = 1
	endpointsIPv6 := []discoveryv1.Endpoint{externalWorkloadToEndpoint(discoveryv1.AddressTypeIPv6, ewCreated, svc)}
	esIPv6.Endpoints = endpointsIPv6
	esIPv6.Generation = 1

	// Create our "new" workloads
	ewUpdatedIPv4 := makeExternalWorkload("1", "wlkd-2", map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{"192.0.2.0"})
	ewUpdatedIPv6 := makeExternalWorkload("1", "wlkd-3", map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{"2001:db8::8a2e:370:7334"})

	// Convert endpointslice to string and register with fake client
	k8sAPI, err := k8s.NewFakeAPI(endpointSliceAsYaml(t, esIPv4), endpointSliceAsYaml(t, esIPv6))
	if err != nil {
		t.Fatalf("unexpected error when creating Kubernetes clientset: %v", err)
	}

	r := newEndpointsReconciler(k8sAPI, testControllerName, defaultTestEndpointsQuota)
	err = r.reconcile(svc, []*ewv1beta1.ExternalWorkload{ewCreated, ewUpdatedIPv4, ewUpdatedIPv6}, []*discoveryv1.EndpointSlice{esIPv4, esIPv6})
	if err != nil {
		t.Fatalf("unexpected error when reconciling endpoints: %v", err)
	}

	for _, ep := range getEndpoints(t, k8sAPI, svc, esIPv4) {
		if ep.TargetRef.Name == ewUpdatedIPv4.Name {
			expectedEndpoint := makeEndpoint([]string{"192.0.2.0"}, true, ewUpdatedIPv4)
			diffEndpoints(t, ep, expectedEndpoint)
		} else if ep.TargetRef.Name == ewCreated.Name {
			expectedEndpoint := makeEndpoint([]string{"192.0.2.1"}, true, ewCreated)
			diffEndpoints(t, ep, expectedEndpoint)
		} else {
			t.Errorf("found unexpected targetRef name %s", ep.TargetRef.Name)
		}
	}

	for _, ep := range getEndpoints(t, k8sAPI, svc, esIPv6) {
		if ep.TargetRef.Name == ewUpdatedIPv6.Name {
			expectedEndpoint := makeEndpoint([]string{"2001:db8::8a2e:370:7334"}, true, ewUpdatedIPv6)
			diffEndpoints(t, ep, expectedEndpoint)
		} else if ep.TargetRef.Name == ewCreated.Name {
			expectedEndpoint := makeEndpoint([]string{"2001:db8::8a2e:370:7333"}, true, ewCreated)
			diffEndpoints(t, ep, expectedEndpoint)
		} else {
			t.Errorf("found unexpected targetRef name %s", ep.TargetRef.Name)
		}
	}
}

func getEndpoints(
	t *testing.T,
	k8sAPI *k8s.API,
	svc *corev1.Service,
	es *discoveryv1.EndpointSlice,
) []discoveryv1.Endpoint {
	t.Helper()
	slice, err := k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Get(context.Background(), es.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error when retrieving endpointslice: %v", err)
	}
	if len(slice.Endpoints) != 2 {
		t.Fatalf("expected %d endpointslices after reconciliation, got %d instead", 2, len(slice.Endpoints))
	}

	if slice.AddressType != es.AddressType {
		t.Fatalf("expected endpointslice to have AF %s, got %s instead", es.AddressType, slice.AddressType)
	}

	return slice.Endpoints
}

// When an endpoint has changed, we should see the endpointslice change its
// endpoint
func TestReconcilerUpdatesEndpointSliceInPlace(t *testing.T) {
	// Create a service
	svc := makeService("test-svc", []corev1.IPFamily{corev1.IPv4Protocol}, map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "")

	// Create our existing workload
	ewCreated := makeExternalWorkload("1", "wlkd-1", map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{"192.0.2.1"})

	// Create an endpointslice
	port := int32(8080)
	ports := []discoveryv1.EndpointPort{{
		Port: &port,
	}}
	es := makeEndpointSlice(svc, discoveryv1.AddressTypeIPv4, ports)
	endpoints := []discoveryv1.Endpoint{}
	endpoints = append(endpoints, externalWorkloadToEndpoint(discoveryv1.AddressTypeIPv4, ewCreated, svc))
	es.Endpoints = endpoints
	es.Generation = 1

	// Convert endpointslice to string and register with fake client
	k8sAPI, err := k8s.NewFakeAPI(endpointSliceAsYaml(t, es))
	if err != nil {
		t.Fatalf("unexpected error when creating Kubernetes clientset: %v", err)
	}

	if err != nil {
		t.Fatalf("unexpected error when retrieving endpointslice: %v", err)
	}

	// Change the workload
	ewCreated.Labels = map[string]string{
		corev1.LabelTopologyZone: "zone1",
	}

	r := newEndpointsReconciler(k8sAPI, testControllerName, defaultTestEndpointsQuota)
	err = r.reconcile(svc, []*ewv1beta1.ExternalWorkload{ewCreated, ewCreated}, []*discoveryv1.EndpointSlice{es})
	if err != nil {
		t.Fatalf("unexpected error when reconciling endpoints: %v", err)
	}

	slice, err := k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Get(context.Background(), es.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error when retrieving endpointslice: %v", err)
	}
	if len(slice.Endpoints) != 1 {
		t.Fatalf("expected %d endpointslices after reconciliation, got %d instead", 1, len(slice.Endpoints))
	}

	if slice.AddressType != discoveryv1.AddressTypeIPv4 {
		t.Fatalf("expected endpointslice to have AF %s, got %s instead", discoveryv1.AddressTypeIPv4, slice.AddressType)
	}

	if slice.Generation == 1 {
		t.Fatalf("expected endpointslice to have its generation bumped after update")
	}

	if *slice.Endpoints[0].Zone != "zone1" {
		t.Fatalf("expected endpoint to be updated with new zone topology")
	}
}

// === Test ports ===

// A named port on a service can target a different port on a workload
func TestReconcileEndpointSlicesNamedPorts(t *testing.T) {
	svc := makeService("test-svc", []corev1.IPFamily{corev1.IPv4Protocol}, map[string]string{"app": "test"}, []corev1.ServicePort{httpNamedPort}, "192.0.2.1")
	ews := []*ewv1beta1.ExternalWorkload{}
	// Generate a large number of external workloads
	// randomise ports so that a named port maps to different target values
	for i := 0; i < 300; i++ {
		ready := !(i%3 == 0)
		offset := i % 5
		genIp := fmt.Sprintf("192.%d.%d.%d", i%5, i%3, i%2)
		genPort := int32(8080 + offset)
		ew := makeExternalWorkload("1", fmt.Sprintf("wlkd-%d", i), map[string]string{"app": "test"}, map[int32]string{genPort: "http"}, []string{genIp})
		ew.Status.Conditions = []ewv1beta1.WorkloadCondition{newStatusCondition(ready)}
		ews = append(ews, ew)
	}

	k8sAPI, err := k8s.NewFakeAPI([]string{}...)
	if err != nil {
		t.Fatalf("unexpected error when initializing API client: %v", err)
	}

	// Start with 100 endpoints max quota. Since we have 5 possible ports
	// mapping to name 'http' we will generate 5 slices
	r := newEndpointsReconciler(k8sAPI, testControllerName, defaultTestEndpointsQuota)
	r.reconcile(svc, ews, []*discoveryv1.EndpointSlice{})
	slices := fetchEndpointSlices(t, k8sAPI, svc)
	expectedNumSlices := 5
	if len(slices) != expectedNumSlices {
		t.Fatalf("expected %d slices to be created, got %d instead", expectedNumSlices, len(slices))
	}

	// We should have 5 slices with 60 endpoints each
	expectSlicesWithLengths(t, []int{60, 60, 60, 60, 60}, slices)
	expectedSlices := []discoveryv1.EndpointSlice{}
	for i := range slices {
		port := int32(8080 + i)
		expectedSlices = append(expectedSlices, discoveryv1.EndpointSlice{
			Ports: []discoveryv1.EndpointPort{
				{
					Port: &port,
				},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
		})
	}

	// Diff the ports
	diffEndpointSlicePorts(t, expectedSlices, slices)
}

// === Test packing logic ===

// a simple use case with 250 workloads matching a service and no existing slices
// reconcile should create 3 slices, completely filling 2 of them
func TestReconcileManyWorkloads(t *testing.T) {
	svc := makeService("test-svc", []corev1.IPFamily{corev1.IPv4Protocol}, map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "10.0.2.1")
	// start with 250 workloads
	ews := []*ewv1beta1.ExternalWorkload{}
	for i := 0; i < 250; i++ {
		ready := !(i%3 == 0)
		genIp := fmt.Sprintf("192.%d.%d.%d", i%5, i%3, i%2)
		ew := makeExternalWorkload("1", fmt.Sprintf("wlkd-%d", i), map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{genIp})

		ew.Status.Conditions = []ewv1beta1.WorkloadCondition{newStatusCondition(ready)}
		ews = append(ews, ew)
	}

	k8sAPI, actions := newClientset(t, []string{})
	r := newEndpointsReconciler(k8sAPI, testControllerName, defaultTestEndpointsQuota)
	r.reconcile(svc, ews, []*discoveryv1.EndpointSlice{})
	expectActions(t, actions(), 3, "create", "endpointslices")

	slices := fetchEndpointSlices(t, k8sAPI, svc)
	expectSlicesWithLengths(t, []int{100, 100, 50}, slices)
}

// Test with preexisting slices. 250 pods matching a service:
// * First es: 62 endpoints (all desired)
// * Second es: 61 endpoints (all desired)
// We have 127 leftover to add.
//
// We will drop 27 in the first slice closest to full
func TestReconcileEndpointSlicesSomePreexisting(t *testing.T) {
	svc := makeService("test-svc", []corev1.IPFamily{corev1.IPv4Protocol}, map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "10.0.2.1")
	// start with 250 workloads
	ews := []*ewv1beta1.ExternalWorkload{}
	for i := 0; i < 250; i++ {
		ready := !(i%3 == 0)
		genIp := fmt.Sprintf("192.%d.%d.%d", i%5, i%3, i%2)
		ew := makeExternalWorkload("1", fmt.Sprintf("wlkd-%d", i), map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{genIp})

		ew.Status.Conditions = []ewv1beta1.WorkloadCondition{newStatusCondition(ready)}
		ews = append(ews, ew)
	}

	// Create an endpointslice
	port := int32(8080)
	esPorts := []discoveryv1.EndpointPort{{
		Port: &port,
	}}

	es1 := makeEndpointSlice(svc, discoveryv1.AddressTypeIPv4, esPorts)
	// Take a quarter of workloads in the first slice
	for i := 1; i < len(ews)-4; i += 4 {
		addrs := []string{ews[i].Spec.WorkloadIPs[0].Ip}
		isReady := IsEwReady(ews[i])
		es1.Endpoints = append(es1.Endpoints, makeEndpoint(addrs, isReady, ews[i]))
	}

	es2 := makeEndpointSlice(svc, discoveryv1.AddressTypeIPv4, esPorts)
	// Take a quarter of workloads in the second slice
	for i := 3; i < len(ews)-4; i += 4 {
		addrs := []string{ews[i].Spec.WorkloadIPs[0].Ip}
		isReady := IsEwReady(ews[i])
		es2.Endpoints = append(es2.Endpoints, makeEndpoint(addrs, isReady, ews[i]))
	}

	existingSlices := []*discoveryv1.EndpointSlice{es1, es2}
	cmc := newCacheMutationCheck(existingSlices)
	k8sAPI, actions := newClientset(t, []string{})
	for _, slice := range existingSlices {
		_, err := k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Create(context.TODO(), slice, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("unexpected error when creating Kubernetes obj: %v", err)
		}
	}

	r := newEndpointsReconciler(k8sAPI, testControllerName, defaultTestEndpointsQuota)
	r.reconcile(svc, ews, existingSlices)
	expectActions(t, actions(), 2, "update", "endpointslices")

	slices := fetchEndpointSlices(t, k8sAPI, svc)
	expectSlicesWithLengths(t, []int{100, 100, 50}, slices)

	// ensure cache mutation has not occurred
	cmc.Check(t)
}

// Ensure reconciler updates everything in-place when a service requires a
// change. That means we expect to only see updates, no creates.
func TestReconcileEndpointSlicesUpdatingSvc(t *testing.T) {
	svc := makeService("test-svc", []corev1.IPFamily{corev1.IPv4Protocol}, map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "10.0.2.1")
	// start with 250 workloads
	ews := []*ewv1beta1.ExternalWorkload{}
	for i := 0; i < 250; i++ {
		ready := !(i%3 == 0)
		genIp := fmt.Sprintf("192.%d.%d.%d", i%5, i%3, i%2)
		ew := makeExternalWorkload("1", fmt.Sprintf("wlkd-%d", i), map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{genIp})

		ew.Status.Conditions = []ewv1beta1.WorkloadCondition{newStatusCondition(ready)}
		ews = append(ews, ew)
	}

	k8sAPI, actions := newClientset(t, []string{})
	r := newEndpointsReconciler(k8sAPI, testControllerName, defaultTestEndpointsQuota)
	r.reconcile(svc, ews, []*discoveryv1.EndpointSlice{})

	slices := fetchEndpointSlices(t, k8sAPI, svc)
	expectSlicesWithLengths(t, []int{100, 100, 50}, slices)
	for _, ew := range ews {
		ew.Spec.Ports[0].Port = int32(81)
	}
	svc.Spec.Ports[0].TargetPort.IntVal = 81

	r.reconcile(svc, ews, []*discoveryv1.EndpointSlice{&slices[0], &slices[1], &slices[2]})
	expectActions(t, actions(), 3, "update", "endpointslices")
	slices = fetchEndpointSlices(t, k8sAPI, svc)
	expectSlicesWithLengths(t, []int{100, 100, 50}, slices)
	for _, slice := range slices {
		if *slice.Ports[0].Port != 81 {
			t.Errorf("expected targetPort value to be 81, got: %d", slice.Ports[0].Port)
		}
	}
}

// When service labels update, all slices will require a change.
//
// This test will ensure that we update slices with the appropriate labels when
// a service has changed.
func TestReconcileEndpointSlicesLabelsUpdatingSvc(t *testing.T) {
	svc := makeService("test-svc", []corev1.IPFamily{corev1.IPv4Protocol}, map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "10.0.2.1")
	// start with 250 workloads
	ews := []*ewv1beta1.ExternalWorkload{}
	for i := 0; i < 250; i++ {
		ready := !(i%3 == 0)
		genIp := fmt.Sprintf("192.%d.%d.%d", i%5, i%3, i%2)
		ew := makeExternalWorkload("1", fmt.Sprintf("wlkd-%d", i), map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{genIp})

		ew.Status.Conditions = []ewv1beta1.WorkloadCondition{newStatusCondition(ready)}
		ews = append(ews, ew)
	}

	k8sAPI, actions := newClientset(t, []string{})
	r := newEndpointsReconciler(k8sAPI, testControllerName, defaultTestEndpointsQuota)
	r.reconcile(svc, ews, []*discoveryv1.EndpointSlice{})

	slices := fetchEndpointSlices(t, k8sAPI, svc)
	expectSlicesWithLengths(t, []int{100, 100, 50}, slices)

	// update service with new labels
	svc.Labels = map[string]string{"foo": "bar"}
	r.reconcile(svc, ews, []*discoveryv1.EndpointSlice{&slices[0], &slices[1], &slices[2]})
	expectActions(t, actions(), 3, "update", "endpointslices")

	slices = fetchEndpointSlices(t, k8sAPI, svc)
	expectSlicesWithLengths(t, []int{100, 100, 50}, slices)
	// check that the labels were updated
	for _, slice := range slices {
		w, ok := slice.Labels["foo"]
		if !ok {
			t.Errorf("expected label \"foo\" from parent service not found")
		} else if "bar" != w {
			t.Errorf("expected EndpointSlice to have parent service labels: have %s value, expected bar", w)
		}
	}
}

// In some cases, such as service labels updates, all slices for that service will require a change
// However, this should not happen for reserved labels
func TestReconcileEndpointSlicesReservedLabelsSvc(t *testing.T) {
	svc := makeService("test-svc", []corev1.IPFamily{corev1.IPv4Protocol}, map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "10.0.2.1")
	// start with 250 workloads
	ews := []*ewv1beta1.ExternalWorkload{}
	for i := 0; i < 250; i++ {
		ready := !(i%3 == 0)
		genIp := fmt.Sprintf("192.%d.%d.%d", i%5, i%3, i%2)
		ew := makeExternalWorkload("1", fmt.Sprintf("wlkd-%d", i), map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{genIp})

		ew.Status.Conditions = []ewv1beta1.WorkloadCondition{newStatusCondition(ready)}
		ews = append(ews, ew)
	}

	k8sAPI, actions := newClientset(t, []string{})
	r := newEndpointsReconciler(k8sAPI, testControllerName, defaultTestEndpointsQuota)
	r.reconcile(svc, ews, []*discoveryv1.EndpointSlice{})
	numActionExpected := 3

	slices := fetchEndpointSlices(t, k8sAPI, svc)
	expectSlicesWithLengths(t, []int{100, 100, 50}, slices)
	numActionExpected++

	// update service with new labels
	svc.Labels = map[string]string{discoveryv1.LabelServiceName: "bad", discoveryv1.LabelManagedBy: "actor", corev1.IsHeadlessService: "invalid"}
	r.reconcile(svc, ews, []*discoveryv1.EndpointSlice{&slices[0], &slices[1], &slices[2]})
	slices = fetchEndpointSlices(t, k8sAPI, svc)
	numActionExpected++
	if len(actions()) != numActionExpected {
		t.Errorf("expected %d actions, got %d instead", numActionExpected, len(actions()))
	}

	expectSlicesWithLengths(t, []int{100, 100, 50}, slices)
	// check that the labels were updated
	for _, slice := range slices {
		if v := slice.Labels[discoveryv1.LabelServiceName]; v == "bad" {
			t.Errorf("unexpected label value \"%s\" from parent service found on slice", "bad")
		}

		if v := slice.Labels[discoveryv1.LabelManagedBy]; v == "actor" {
			t.Errorf("unexpected label value \"%s\" from parent service found on slice", "actor")
		}

		if v := slice.Labels[corev1.IsHeadlessService]; v == "invalid" {
			t.Errorf("unexpected label value \"%s\" from parent service found on slice", "invalid")
		}
	}
}

func TestEndpointSlicesAreRecycled(t *testing.T) {
	svc := makeService("test-svc", []corev1.IPFamily{corev1.IPv4Protocol}, map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "10.0.2.1")
	// start with 250 workloads
	ews := []*ewv1beta1.ExternalWorkload{}
	for i := 0; i < 300; i++ {
		ready := !(i%3 == 0)
		genIp := fmt.Sprintf("192.%d.%d.%d", i%5, i%3, i%2)
		ew := makeExternalWorkload("1", fmt.Sprintf("wlkd-%d", i), map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{genIp})

		ew.Status.Conditions = []ewv1beta1.WorkloadCondition{newStatusCondition(ready)}
		ews = append(ews, ew)
	}

	// Create an endpointslice
	port := int32(8080)
	esPorts := []discoveryv1.EndpointPort{{
		Port: &port,
	}}

	// generate 10 existing slices with 30 endpoints each
	existingSlices := []*discoveryv1.EndpointSlice{}
	for i, ew := range ews {
		sliceNum := i / 30
		if i%30 == 0 {
			existingSlices = append(existingSlices, makeEndpointSlice(svc, discoveryv1.AddressTypeIPv4, esPorts))
		}

		addrs := []string{ews[i].Spec.WorkloadIPs[0].Ip}
		isReady := IsEwReady(ews[i])
		existingSlices[sliceNum].Endpoints = append(existingSlices[sliceNum].Endpoints, makeEndpoint(addrs, isReady, ew))
	}

	cmc := newCacheMutationCheck(existingSlices)
	k8sAPI, err := k8s.NewFakeAPI([]string{}...)
	if err != nil {
		t.Fatalf("unexpected error when creating Kubernetes clientset: %v", err)
	}

	for _, slice := range existingSlices {
		_, err := k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Create(context.TODO(), slice, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("unexpected error when creating Kubernetes obj: %v", err)
		}
	}

	for _, ew := range ews {
		ew.Spec.Ports[0].Port = int32(81)
	}

	// changing a service port should require all slices to be updated, time for a repack
	svc.Spec.Ports[0].TargetPort.IntVal = 81
	r := newEndpointsReconciler(k8sAPI, testControllerName, defaultTestEndpointsQuota)
	r.reconcile(svc, ews, existingSlices)

	slices := fetchEndpointSlices(t, k8sAPI, svc)
	expectSlicesWithLengths(t, []int{100, 100, 100}, slices)
	// ensure cache mutation has not occurred
	cmc.Check(t)
}

func newClientset(t *testing.T, k8sConfigs []string) (*k8s.API, func() []k8stesting.Action) {
	k8sAPI, actions, err := k8s.NewFakeAPIWithActions(k8sConfigs...)

	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	return k8sAPI, actions
}

func makeDualEndpointSlices(svc *corev1.Service, ports []discoveryv1.EndpointPort) (*discoveryv1.EndpointSlice, *discoveryv1.EndpointSlice) {
	esIPv4 := makeEndpointSlice(svc, discoveryv1.AddressTypeIPv4, ports)
	esIPv6 := makeEndpointSlice(svc, discoveryv1.AddressTypeIPv6, ports)
	return esIPv4, esIPv6
}

func makeEndpointSlice(svc *corev1.Service, addrType discoveryv1.AddressType, ports []discoveryv1.EndpointPort) *discoveryv1.EndpointSlice {
	// We need an ownerRef to point to our service
	ownerRef := metav1.NewControllerRef(svc, schema.GroupVersionKind{Version: "v1", Kind: "Service"})
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("linkerd-external-%s-%s", svc.Name, rand.String(8)),
			Namespace:       svc.Namespace,
			Labels:          map[string]string{},
			OwnerReferences: []metav1.OwnerReference{*ownerRef},
		},
		AddressType: addrType,
		Endpoints:   []discoveryv1.Endpoint{},
		Ports:       ports,
	}
	labels, _ := setEndpointSliceLabels(slice, svc, testControllerName)
	slice.Labels = labels
	return slice
}

// Helper function that tests a set of slices matches a list of expected lengths
// for number of endpoints
func expectSlicesWithLengths(t *testing.T, expectedLengths []int, es []discoveryv1.EndpointSlice) {
	t.Helper()
	noMatch := []string{}
	for _, slice := range es {
		epLen := len(slice.Endpoints)
		matched := false
		for i := 0; i < len(expectedLengths); i++ {
			if epLen == expectedLengths[i] {
				matched = true
				expectedLengths = append(expectedLengths[:i], expectedLengths[i+1:]...)
				break
			}
		}

		if !matched {
			noMatch = append(noMatch, fmt.Sprintf("%s/%s (%d)", slice.Namespace, slice.Name, len(slice.Endpoints)))
		}
	}

	if len(noMatch) > 0 {
		t.Fatalf("slices %s did not match the required lengths, unmatched lengths: %v", strings.Join(noMatch, ", "), expectedLengths)
	}
}

func diffEndpointSlicePorts(t *testing.T, expected, actual []discoveryv1.EndpointSlice) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Fatalf("expected %d slices, got %d instead", len(expected), len(actual))
	}

	unmatched := []discoveryv1.EndpointSlice{}
	for _, actualSlice := range actual {
		matched := false
		for i := 0; i < len(expected); i++ {
			expectedSlice := expected[i]
			expectedHash := epsliceutil.NewPortMapKey(expectedSlice.Ports)
			actualHash := epsliceutil.NewPortMapKey(actualSlice.Ports)

			if (actualSlice.AddressType == expectedSlice.AddressType) &&
				(actualHash == expectedHash) {
				matched = true
				expected = append(expected[:i], expected[i+1:]...)
				break
			}
		}

		if !matched {
			unmatched = append(unmatched, actualSlice)
		}
	}

	if len(expected) != 0 {
		t.Errorf("expected slices not found in actual list of EndpointSlices")
	}

	if len(unmatched) > 0 {
		t.Errorf("found %d slices that do not match expected ports", len(unmatched))
	}
}

// === Test utilities ===

// Modify a slice's name in-place since the fake API server does not support
// generated names
func endpointSliceAsYaml(t *testing.T, es *discoveryv1.EndpointSlice) string {
	if es.Name == "" {
		es.Name = fmt.Sprintf("%s-%s", es.ObjectMeta.GenerateName, rand.String(5))
		es.GenerateName = ""
	}
	es.TypeMeta = metav1.TypeMeta{
		APIVersion: "discovery.k8s.io/v1",
		Kind:       "EndpointSlice",
	}

	b, err := yaml.Marshal(es)
	if err != nil {
		t.Fatalf("unexpected error when serializing endpointslices to yaml")
	}

	return string(b)

}

func makeService(
	name string,
	ipFamilies []corev1.IPFamily,
	selector map[string]string,
	ports []corev1.ServicePort,
	clusterIP string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       types.UID(name),
		},
		Spec: corev1.ServiceSpec{
			Ports:      ports,
			Selector:   selector,
			ClusterIP:  clusterIP,
			IPFamilies: ipFamilies,
		},
		Status: corev1.ServiceStatus{},
	}
}

func makeEndpoint(addrs []string, isReady bool, ew *ewv1beta1.ExternalWorkload) discoveryv1.Endpoint {
	rdy := &isReady
	term := !isReady
	ep := discoveryv1.Endpoint{
		Addresses: addrs,
		Conditions: discoveryv1.EndpointConditions{
			Ready:       rdy,
			Serving:     rdy,
			Terminating: &term,
		},
		TargetRef: &corev1.ObjectReference{
			Kind:      ew.Kind,
			Namespace: ew.Namespace,
			Name:      ew.Name,
			UID:       ew.UID,
		},
	}
	return ep
}

func fetchEndpointSlices(t *testing.T, k8sAPI *k8s.API, svc *corev1.Service) []discoveryv1.EndpointSlice {
	t.Helper()
	selector := labels.Set(map[string]string{
		discoveryv1.LabelServiceName: svc.Name,
		discoveryv1.LabelManagedBy:   testControllerName,
	}).AsSelectorPreValidated()
	fetchedSlices, err := k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		t.Fatalf("unexpected error when fetching endpointslices: %v", err)
	}

	return fetchedSlices.Items
}

func diffEndpoints(t *testing.T, actual, expected discoveryv1.Endpoint) {
	t.Helper()
	if len(actual.Addresses) != len(expected.Addresses) {
		t.Errorf("expected %d addresses, got %d instead", len(expected.Addresses), len(actual.Addresses))
	}

	if actual.Conditions.Ready != nil && expected.Conditions.Ready != nil {
		if *actual.Conditions.Ready != *expected.Conditions.Ready {
			t.Errorf("expected \"ready\" condition to be %t, got %t instead", *expected.Conditions.Ready, *actual.Conditions.Ready)
		}
	}

	if actual.Conditions.Serving != nil && expected.Conditions.Serving != nil {
		if *actual.Conditions.Serving != *expected.Conditions.Serving {
			t.Errorf("expected \"serving\" condition to be %t, got %t instead", *expected.Conditions.Serving, *actual.Conditions.Serving)
		}
	}

	if actual.Conditions.Terminating != nil && expected.Conditions.Terminating != nil {
		if *actual.Conditions.Terminating != *expected.Conditions.Terminating {
			t.Errorf("expected \"terminating\" condition to be %t, got %t instead", *expected.Conditions.Terminating, *actual.Conditions.Terminating)
		}
	}

	if actual.Zone != nil && expected.Zone != nil {
		if *actual.Zone != *expected.Zone {
			t.Errorf("expected \"zone=%s\", got \"zone=%s\" instead", *expected.Zone, *actual.Zone)
		}
	}

	actualAddrs := toSet(actual.Addresses)
	expAddrs := toSet(expected.Addresses)
	for actualAddr := range actualAddrs {
		if _, found := expAddrs[actualAddr]; !found {
			t.Errorf("found unexpected address %s in the actual endpoint", actualAddr)
		}
	}

	for expAddr := range expAddrs {
		if _, found := actualAddrs[expAddr]; !found {
			t.Errorf("expected to find address %s in the actual endpoint", expAddr)
		}
	}

	expRef := expected.TargetRef
	actRef := actual.TargetRef
	if expRef.UID != actRef.UID {
		t.Errorf("expected targetRef with UID %s; got %s instead", expRef.UID, actRef.UID)
	}

	if expRef.Name != actRef.Name {
		t.Errorf("expected targetRef with name %s; got %s instead", expRef.Name, actRef.Name)
	}

}

// === impl cache mutation check

// Code originally forked from:
//
// https://github.com/kubernetes/endpointslice/commit/a09c1c9580d13f5020248d25c7fd11f5dde6dd9b

// cacheMutationCheck helps ensure that cached objects have not been changed
// in any way throughout a test run.
type cacheMutationCheck struct {
	objects []cacheObject
}

// cacheObject stores a reference to an original object as well as a deep copy
// of that object to track any mutations in the original object.
type cacheObject struct {
	original runtime.Object
	deepCopy runtime.Object
}

// newCacheMutationCheck initializes a cacheMutationCheck with EndpointSlices.
func newCacheMutationCheck(endpointSlices []*discoveryv1.EndpointSlice) cacheMutationCheck {
	cmc := cacheMutationCheck{}
	for _, endpointSlice := range endpointSlices {
		cmc.Add(endpointSlice)
	}
	return cmc
}

// Add appends a runtime.Object and a deep copy of that object into the
// cacheMutationCheck.
func (cmc *cacheMutationCheck) Add(o runtime.Object) {
	cmc.objects = append(cmc.objects, cacheObject{
		original: o,
		deepCopy: o.DeepCopyObject(),
	})
}

// Check verifies that no objects in the cacheMutationCheck have been mutated.
func (cmc *cacheMutationCheck) Check(t *testing.T) {
	for _, o := range cmc.objects {
		if !reflect.DeepEqual(o.original, o.deepCopy) {
			// Cached objects can't be safely mutated and instead should be deep
			// copied before changed in any way.
			t.Errorf("Cached object was unexpectedly mutated. Original: %+v, Mutated: %+v", o.deepCopy, o.original)
		}
	}
}

func toSet(s []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, k := range s {
		set[k] = struct{}{}
	}
	return set
}
