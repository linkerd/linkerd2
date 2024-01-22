package externalworkload

import (
	"context"
	"fmt"
	"strings"
	"testing"

	ewv1alpha1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1alpha1"
	"github.com/linkerd/linkerd2/controller/k8s"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	epsliceutil "k8s.io/endpointslice/util"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/yaml"
)

// TestExternalWorkloadIPv4Reconciler tests the main reconcile entrypoint for
// ipv4 stacks.
//
// The reconciler operates on a group of external workloads, their service, and
// any endpointslices that have been currently written.
//
// The test exercises that the reconciler will correctly create / update / delete
// slices
func TestExternalWorkloadIPv4Reconciler(t *testing.T) {
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

	maxTestEndpointsQuota = 10

	testControllerName = "test-controller"
)

func makeEndpointSlice(svc *corev1.Service, ports []discoveryv1.EndpointPort) *discoveryv1.EndpointSlice {
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
	labels, _ := setEndpointSliceLabels(slice, svc, testControllerName)
	slice.Labels = labels
	return slice
}

// === Reconciler module tests ===

// Test that when a service has no endpointslices written to the API Server, reconciling
// with a workload will create a new endpointslice.
func TestReconcilerCreatesNewEndpointSlice(t *testing.T) {
	// We do not need to receive anything through the informers so
	// create a client with no cached resources
	k8sAPI, err := k8s.NewFakeAPI([]string{}...)
	if err != nil {
		t.Fatalf("unexpected error when creating Kubernetes clientset: %v", err)
	}

	svc := makeIPv4Service(map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "", "")
	ew := makeExternalWorkload("wlkd-1", map[string]string{"app": ""}, map[int32]string{8080: ""}, []string{"192.0.2.0"})
	ew.ObjectMeta.UID = types.UID(fmt.Sprintf("%s-%s", ew.Namespace, ew.Name))

	r := newEndpointsReconciler(k8sAPI, "test-controller", 10)
	err = r.reconcile(svc, []*ewv1alpha1.ExternalWorkload{ew}, nil)
	if err != nil {
		t.Fatalf("unexpected error when reconciling endpoints: %v", err)
	}

	expectedEndpoint := makeEndpoint([]string{"192.0.2.0"}, true, "", ew)
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
}

// Test that when a service has an endpointslice written to the API Server,
// reconciling with the two workloads updates the endpointslice
func TestReconcilerUpdatesEndpointSlice(t *testing.T) {
	// Create a service
	svc := makeIPv4Service(map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "", "")

	// Create our existing workload
	ewCreated := makeExternalWorkload("wlkd-1", map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{"192.0.2.1"})
	ewCreated.ObjectMeta.UID = types.UID(fmt.Sprintf("%s-%s", ewCreated.Namespace, ewCreated.Name))

	// Create an endpointslice
	port := int32(8080)
	ports := []discoveryv1.EndpointPort{{
		Port: &port,
	}}
	es := makeEndpointSlice(svc, ports)
	endpoints := []discoveryv1.Endpoint{}
	endpoints = append(endpoints, externalWorkloadToEndpoint(discoveryv1.AddressTypeIPv4, ewCreated, svc))
	es.Endpoints = endpoints
	es.Generation = 1

	// Create our "new" workload
	ewUpdated := makeExternalWorkload("wlkd-2", map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{"192.0.2.0"})
	ewUpdated.ObjectMeta.UID = types.UID(fmt.Sprintf("%s-%s", ewUpdated.Namespace, ewUpdated.Name))

	// Convert endpointslice to string and register with fake client
	k8sAPI, err := k8s.NewFakeAPI(endpointSliceAsYaml(t, es))
	if err != nil {
		t.Fatalf("unexpected error when creating Kubernetes clientset: %v", err)
	}

	r := newEndpointsReconciler(k8sAPI, testControllerName, maxTestEndpointsQuota)
	err = r.reconcile(svc, []*ewv1alpha1.ExternalWorkload{ewCreated, ewUpdated}, []*discoveryv1.EndpointSlice{es})
	if err != nil {
		t.Fatalf("unexpected error when reconciling endpoints: %v", err)
	}

	slice, err := k8sAPI.Client.DiscoveryV1().EndpointSlices(svc.Namespace).Get(context.Background(), es.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error when retrieving endpointslice: %v", err)
	}
	if len(slice.Endpoints) != 2 {
		t.Fatalf("expected %d endpointslices after reconciliation, got %d instead", 2, len(slice.Endpoints))
	}

	if slice.AddressType != discoveryv1.AddressTypeIPv4 {
		t.Fatalf("expected endpointslice to have AF %s, got %s instead", discoveryv1.AddressTypeIPv4, slice.AddressType)
	}

	for _, ep := range slice.Endpoints {
		if ep.TargetRef.Name == ewUpdated.Name {
			expectedEndpoint := makeEndpoint([]string{"192.0.2.0"}, true, "", ewUpdated)
			diffEndpoints(t, ep, expectedEndpoint)
		} else if ep.TargetRef.Name == ewCreated.Name {
			expectedEndpoint := makeEndpoint([]string{"192.0.2.1"}, true, "", ewCreated)
			diffEndpoints(t, ep, expectedEndpoint)
		} else {
			t.Errorf("found unexpected targetRef name %s", ep.TargetRef.Name)
		}
	}
}

// When an endpoint has changed, we should see the endpointslice change its
// endpoint
func TestReconcilerUpdatesEndpointSliceInPlace(t *testing.T) {
	// Create a service
	svc := makeIPv4Service(map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "", "")

	// Create our existing workload
	ewCreated := makeExternalWorkload("wlkd-1", map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{"192.0.2.1"})
	ewCreated.ObjectMeta.UID = types.UID(fmt.Sprintf("%s-%s", ewCreated.Namespace, ewCreated.Name))

	// Create an endpointslice
	port := int32(8080)
	ports := []discoveryv1.EndpointPort{{
		Port: &port,
	}}
	es := makeEndpointSlice(svc, ports)
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

	r := newEndpointsReconciler(k8sAPI, testControllerName, maxTestEndpointsQuota)
	err = r.reconcile(svc, []*ewv1alpha1.ExternalWorkload{ewCreated, ewCreated}, []*discoveryv1.EndpointSlice{es})
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

// Test that when a service has an endpointslice written to the API Server, and
// the endpoint is updated, the update is propagated through

// Test our packing algorithm and logic to ensure correctness.
//
// We run a table-driven test with some input parameters that decide the current
// state of a service's membership. We want to see if all of the endpoints will
// be filled out correctly.
func TestReconcilerHandlesExceededCapacity(t *testing.T) {
	for _, tt := range []struct {
		name              string
		numWorkloads      int
		numExistingSlices int
		expectedNumSlices int
		expectedLengths   []int
		stepBy            int
	}{
		{
			// Base case: can we handle exceeded capacity when we process a big
			// update
			name:              "handles exceeded capacity",
			numWorkloads:      35,
			numExistingSlices: 0,
			expectedNumSlices: 4,
			expectedLengths:   []int{10, 10, 10, 5},
			stepBy:            0,
		},
		{
			// Can we handle capacity when we have slices that are already
			// created
			name:              "handles exceeded capacity with existing slices by creating",
			numWorkloads:      41,
			numExistingSlices: 4,
			expectedNumSlices: 5,
			expectedLengths:   []int{10, 10, 10, 10, 1},
			stepBy:            10,
		},
		// With a max capacity of 10, and with each endpoint (except the last)
		// having 7 endpoints (last one will contain 10), we will have a
		// leftover of 10. We except to see each slice be filled.
		{
			name:              "handles exceeded capacity with existing slices by updating only",
			numWorkloads:      48,
			numExistingSlices: 5,
			expectedNumSlices: 5,
			expectedLengths:   []int{10, 10, 10, 10, 8},
			stepBy:            7,
		},
	} {
		tt := tt // pin
		t.Run(tt.name, func(t *testing.T) {
			// Create a service. We do not care about the service's particular
			// fields in this test so it's not parametrised in our test table
			svc := makeIPv4Service(map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "", "")

			// Generate random workloads
			ews := []*ewv1alpha1.ExternalWorkload{}
			for i := 0; i < tt.numWorkloads; i++ {
				// Generate a workload selected by the service
				name := fmt.Sprintf("wlkd-%d", i)
				ew := makeExternalWorkload(name, map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{fmt.Sprintf("192.0.2.%d", i)})
				ews = append(ews, ew)
			}

			// Generate preexisting slices
			preexistingSlices := []*discoveryv1.EndpointSlice{}
			i := 0
			// Step by will take a chunk of already created workloads and move
			// them into an EndpointSlice
			step := 0
			port := int32(8080)
			allocatedEws := []*ewv1alpha1.ExternalWorkload{}
			for i < tt.numExistingSlices {
				ports := []discoveryv1.EndpointPort{{Port: &port}}
				es := makeEndpointSlice(svc, ports)
				eps := []discoveryv1.Endpoint{}
				// Take a chunk according to the parametrised stepBy
				if step+tt.stepBy < tt.numWorkloads {
					allocatedEws = ews[step:(step + tt.stepBy)]
				} else {
					allocatedEws = ews[step:]
				}
				for _, ew := range allocatedEws {
					eps = append(eps, externalWorkloadToEndpoint(discoveryv1.AddressTypeIPv4, ew, svc))
				}
				es.Endpoints = eps
				step = step + tt.stepBy
				i++
				preexistingSlices = append(preexistingSlices, es)
			}

			// Generate JSON strings, if required to have existing slices
			k8sResources := []string{}
			for _, res := range preexistingSlices {
				k8sResources = append(k8sResources, endpointSliceAsYaml(t, res))
			}

			k8sAPI, err := k8s.NewFakeAPI(k8sResources...)
			if err != nil {
				t.Fatalf("unexpected error when creating Kubernetes clientset: %v", err)
			}

			r := newEndpointsReconciler(k8sAPI, testControllerName, maxTestEndpointsQuota)
			err = r.reconcile(svc, ews, preexistingSlices)
			if err != nil {
				t.Fatalf("unexpected error when reconciling endpoints: %v", err)
			}

			es := fetchEndpointSlices(t, k8sAPI, svc)
			if len(es) != tt.expectedNumSlices {
				t.Fatalf("expected %d endpointslices after reconciliation, got %d instead", tt.expectedNumSlices, len(es))
			}

			expectedLen := tt.expectedLengths
			noMatch := []string{}
			for _, slice := range es {
				epLen := len(slice.Endpoints)
				matched := false
				for i := 0; i < len(expectedLen); i++ {
					if epLen == expectedLen[i] {
						matched = true
						expectedLen = append(expectedLen[:i], expectedLen[i+1:]...)
						break
					}
				}

				if !matched {
					noMatch = append(noMatch, fmt.Sprintf("%s/%s", slice.Namespace, slice.Name))
				}
			}

			if len(noMatch) > 0 {
				t.Fatalf("slices %s did not match the required lengths, unmatched lengths: %v", strings.Join(noMatch, ", "), expectedLen)

			}
		})
	}
}

// Check ports are processed correctly.
func TestReconcilerSegmentsPortsProperly(t *testing.T) {
	// We declare some services beforehand to create the endpointslices
	httpNamedPortSvc := makeIPv4Service(map[string]string{"app": "test"}, []corev1.ServicePort{httpNamedPort}, "", "")
	httpUnnamedPortSvc := makeIPv4Service(map[string]string{"app": "test"}, []corev1.ServicePort{httpUnnamedPort}, "", "")

	for _, tt := range []struct {
		name              string
		service           *corev1.Service
		existingWorkloads []*ewv1alpha1.ExternalWorkload
		existingSlice     *discoveryv1.EndpointSlice
		appliedWorkloads  []*ewv1alpha1.ExternalWorkload
		expectedSlices    int
	}{
		{
			name:    "named targetPort should result in multiple slices being created when it differs between workloads",
			service: httpNamedPortSvc,
			existingWorkloads: []*ewv1alpha1.ExternalWorkload{
				makeExternalWorkload("wlkd-1", map[string]string{"app": "test"}, map[int32]string{8080: "http"}, []string{"192.0.2.1"}),
			},
			existingSlice: makeEndpointSlice(httpNamedPortSvc, []discoveryv1.EndpointPort{makeEndpointPort(8080, "http")}),
			appliedWorkloads: []*ewv1alpha1.ExternalWorkload{
				makeExternalWorkload("wlkd-2", map[string]string{"app": "test"}, map[int32]string{80: "http"}, []string{"192.0.3.1"}),
			},
			expectedSlices: 2,
		},
		{
			name:              "named targetPort from service should be written to an endpointslice",
			service:           httpNamedPortSvc,
			existingWorkloads: []*ewv1alpha1.ExternalWorkload{},
			existingSlice:     nil,
			appliedWorkloads: []*ewv1alpha1.ExternalWorkload{
				makeExternalWorkload("wlkd-1", map[string]string{"app": "test"}, map[int32]string{80: "http"}, []string{"192.0.3.1"}),
			},
			expectedSlices: 1,
		},
		{
			name:    "named targetPort is shared by multiple workloads",
			service: httpNamedPortSvc,
			existingWorkloads: []*ewv1alpha1.ExternalWorkload{
				makeExternalWorkload("wlkd-1", map[string]string{"app": "test"}, map[int32]string{8080: "http"}, []string{"192.0.2.1"}),
			},
			existingSlice: makeEndpointSlice(httpNamedPortSvc, []discoveryv1.EndpointPort{makeEndpointPort(8080, "http")}),
			appliedWorkloads: []*ewv1alpha1.ExternalWorkload{
				makeExternalWorkload("wlkd-2", map[string]string{"app": "test"}, map[int32]string{8080: "http"}, []string{"192.0.3.1"}),
			},
			expectedSlices: 1,
		},
		{
			name:    "named targetPort changes to unnamed port and workloads are unioned",
			service: httpUnnamedPortSvc,
			existingWorkloads: []*ewv1alpha1.ExternalWorkload{
				makeExternalWorkload("wlkd-1", map[string]string{"app": "test"}, map[int32]string{8080: "http"}, []string{"192.0.2.1"}),
			},
			existingSlice: makeEndpointSlice(httpNamedPortSvc, []discoveryv1.EndpointPort{makeEndpointPort(8080, "http")}),
			appliedWorkloads: []*ewv1alpha1.ExternalWorkload{
				makeExternalWorkload("wlkd-2", map[string]string{"app": "test"}, map[int32]string{80: "http", 8080: ""}, []string{"192.0.3.1"}),
			},
			expectedSlices: 1,
		},
		{
			name:    "when named targetPort is no longer selected by workloads, ports are unset",
			service: httpNamedPortSvc,
			existingWorkloads: []*ewv1alpha1.ExternalWorkload{
				makeExternalWorkload("wlkd-1", map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{"192.0.2.1"}),
				makeExternalWorkload("wlkd-2", map[string]string{"app": "test"}, map[int32]string{8080: ""}, []string{"192.0.3.1"}),
			},
			existingSlice: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-123",
					Namespace: "default",
					UID:       "default-test-123",
					Labels: map[string]string{
						discoveryv1.LabelServiceName: httpNamedPortSvc.Name,
						discoveryv1.LabelManagedBy:   managedBy,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Service",
							Name:       "test-svc",
							UID:        "default-test-svc",
						},
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Ports: []discoveryv1.EndpointPort{
					makeEndpointPort(8080, "http"),
				},
			},
			appliedWorkloads: []*ewv1alpha1.ExternalWorkload{},
			expectedSlices:   1,
		},
	} {
		tt := tt // pin
		t.Run(tt.name, func(t *testing.T) {
			// First, create the endpointslice that is supposed to "exist" when we
			// initialise the API Server
			var k8sAPI *k8s.API
			var err error
			existingSlices := []*discoveryv1.EndpointSlice{}
			if tt.existingSlice != nil {
				endpoints := []discoveryv1.Endpoint{}
				for _, ew := range tt.existingWorkloads {
					endpoints = append(endpoints, externalWorkloadToEndpoint(discoveryv1.AddressTypeIPv4, ew, httpNamedPortSvc))
				}
				es := tt.existingSlice
				es.Endpoints = endpoints
				es.Generation = 1
				existingSlices = append(existingSlices, es)

				// Convert endpointslice to string and register with fake client
				k8sAPI, err = k8s.NewFakeAPI(endpointSliceAsYaml(t, es))
				if err != nil {
					t.Fatalf("unexpected error when creating Kubernetes clientset: %v", err)
				}
			} else {
				k8sAPI, err = k8s.NewFakeAPI()
				if err != nil {
					t.Fatalf("unexpected error when creating Kubernetes clientset: %v", err)
				}
			}

			ews := []*ewv1alpha1.ExternalWorkload{}
			for _, ew := range tt.existingWorkloads {
				ews = append(ews, ew)
			}

			for _, ew := range tt.appliedWorkloads {
				ews = append(ews, ew)
			}

			// Collect all of the ports by hashing them. We'll check the
			// generated slices at the end to see whether all of the ports have
			// been written successfully
			r := newEndpointsReconciler(k8sAPI, testControllerName, maxTestEndpointsQuota)
			expectedPorts := map[epsliceutil.PortMapKey]struct{}{}

			err = r.reconcile(tt.service, ews, existingSlices)
			if err != nil {
				t.Fatalf("unexpected error when reconciling endpoints: %v", err)
			}

			slices := fetchEndpointSlices(t, k8sAPI, httpNamedPortSvc)
			if len(slices) != tt.expectedSlices {
				t.Errorf("expected %d endpointslices after reconciliation, got %d instead", tt.expectedSlices, len(slices))
				for _, slice := range slices {
					t.Logf("%s\n---\n", endpointSliceAsYaml(t, &slice))
				}

			}

			for _, ew := range ews {
				ports := r.findEndpointPorts(httpNamedPortSvc, ew)
				expectedPorts[epsliceutil.NewPortMapKey(ports)] = struct{}{}
			}
			for _, slice := range slices {
				hash := epsliceutil.NewPortMapKey(slice.Ports)
				if _, ok := expectedPorts[hash]; !ok {
					t.Errorf("expected slice %s/%s to contain one of the expected port sets", slice.Namespace, slice.Name)
				}
			}
		})

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

func makeIPv4Service(selector map[string]string, ports []corev1.ServicePort, clusterIP string, svcType string) *corev1.Service {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-svc",
			Namespace: "default",
			UID:       "default-test-svc",
		},
		Spec: corev1.ServiceSpec{
			Ports:      ports,
			Selector:   selector,
			ClusterIP:  clusterIP,
			IPFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
		},
		Status: corev1.ServiceStatus{},
	}
	if svcType != "" {
		svc.Spec.Type = corev1.ServiceType(svcType)
	} else {
		svc.Spec.Type = corev1.ServiceTypeClusterIP
	}

	return svc
}

func makeEndpoint(addrs []string, isReady bool, hostname string, ew *ewv1alpha1.ExternalWorkload) discoveryv1.Endpoint {
	rdy := &isReady
	term := !isReady
	ep := discoveryv1.Endpoint{
		Addresses: addrs,
		Conditions: discoveryv1.EndpointConditions{
			Ready:       rdy,
			Serving:     rdy,
			Terminating: &term,
		},
		Hostname: &hostname,
		TargetRef: &corev1.ObjectReference{
			Kind:      ew.Kind,
			Namespace: ew.Namespace,
			Name:      ew.Name,
			UID:       ew.UID,
		},
	}
	return ep
}

func makeEndpointPort(val int, name string) discoveryv1.EndpointPort {
	port := int32(val)
	return discoveryv1.EndpointPort{
		Port: &port,
		Name: &name,
	}
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
		return []discoveryv1.EndpointSlice{}
	}
	return fetchedSlices.Items
}

func diffEndpoints(t *testing.T, actual, expected discoveryv1.Endpoint) {
	t.Helper()
	if len(actual.Addresses) != len(expected.Addresses) {
		t.Errorf("expected %d addresses, got %d instead", len(expected.Addresses), len(actual.Addresses))
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

// TestReconcileEndpoints will test the diffing logic in isolation of the rest
// of the reconciler machinery.
func TestReconcileEndpoints(t *testing.T) {
	for _, tt := range []struct {
		name string
	}{
		{
			name: "test endpoint is marked as serving and ready when status is ready",
		},
		{
			name: "test endpoint is marked as unready when status is unready",
		},
		{
			name: "test endpoint is marked as unready when status is missing",
		},

		{
			name: "test service port change with no workload match deletes slices",
		},
		{
			name: "test service port change results in new slices being created",
		},
		{
			name: "test headless service is created and entry receives hostname",
		},
		{
			name: "test an existing endpointslice is deleted when no workloads exist",
		},
	} {
		tt := tt // pin
		t.Run(tt.name, func(t *testing.T) {

		})
	}
}
