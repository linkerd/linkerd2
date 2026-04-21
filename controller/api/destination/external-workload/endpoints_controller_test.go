package externalworkload

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	ewv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	"github.com/linkerd/linkerd2/controller/k8s"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
)

type endpointSliceController struct {
	*EndpointsController
	endpointSliceStore     cache.Store
	externalWorkloadsStore cache.Store
	serviceStore           cache.Store
}

func newController(t *testing.T) (*k8s.API, func() []k8stesting.Action, *endpointSliceController) {
	t.Helper()

	k8sAPI, actions, err := k8s.NewFakeAPIWithActions()
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	esController, err := NewEndpointsController(k8sAPI, "hostname", "linkerd", make(chan struct{}), false)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	return k8sAPI, actions, &endpointSliceController{
		esController,
		k8sAPI.ES().Informer().GetStore(),
		k8sAPI.ExtWorkload().Informer().GetStore(),
		k8sAPI.Svc().Informer().GetStore(),
	}

}

func newExternalWorkload(n int, namespace string, ready bool, terminating bool) *ewv1beta1.ExternalWorkload {
	status := ewv1beta1.ConditionTrue
	if !ready {
		status = ewv1beta1.ConditionFalse
	}

	var deletionTimestamp *metav1.Time
	if terminating {
		deletionTimestamp = &metav1.Time{
			Time: time.Now(),
		}
	}

	ew := &ewv1beta1.ExternalWorkload{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              fmt.Sprintf("ew-%d", n),
			Labels:            map[string]string{"foo": "bar"},
			DeletionTimestamp: deletionTimestamp,
			ResourceVersion:   fmt.Sprint(n),
		},
		Spec: ewv1beta1.ExternalWorkloadSpec{
			Ports: []ewv1beta1.PortSpec{
				{
					Name: "name",
					Port: 444,
				},
			},
			WorkloadIPs: []ewv1beta1.WorkloadIP{
				{Ip: "1.2.3.4"},
			},
		},
		Status: ewv1beta1.ExternalWorkloadStatus{
			Conditions: []ewv1beta1.WorkloadCondition{
				{
					Type:   ewv1beta1.WorkloadReady,
					Status: status,
				},
			},
		},
	}

	return ew
}

// Ensure SyncService for service with no selector results in no action
func TestSyncServiceNoSelector(t *testing.T) {
	ns := metav1.NamespaceDefault
	serviceName := "testing-1"
	_, actions, esController := newController(t)
	esController.serviceStore.Add(&v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: ns},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{{TargetPort: intstr.FromInt32(80)}},
		},
	})

	err := esController.syncService(fmt.Sprintf("%s/%s", ns, serviceName))
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}

	if len(actions()) != 0 {
		t.Errorf("expected 0 actions, got: %d", len(actions()))
	}
}

func TestServiceExternalNameTypeSync(t *testing.T) {
	serviceName := "testing-1"
	namespace := "zahari"

	testCases := []struct {
		desc    string
		service *v1.Service
	}{
		{
			desc: "External name with selector and ports should not receive endpoint slices",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: namespace},
				Spec: v1.ServiceSpec{
					Selector: map[string]string{"foo": "bar"},
					Ports:    []v1.ServicePort{{Port: 80}},
					Type:     v1.ServiceTypeExternalName,
				},
			},
		},
		{
			desc: "External name with ports should not receive endpoint slices",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: namespace},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{{Port: 80}},
					Type:  v1.ServiceTypeExternalName,
				},
			},
		},
		{
			desc: "External name with selector should not receive endpoint slices",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: namespace},
				Spec: v1.ServiceSpec{
					Selector: map[string]string{"foo": "bar"},
					Type:     v1.ServiceTypeExternalName,
				},
			},
		},
		{
			desc: "External name without selector and ports should not receive endpoint slices",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: namespace},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeExternalName,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			client, actions, esController := newController(t)
			ew := newExternalWorkload(1, namespace, true, false)
			err := esController.externalWorkloadsStore.Add(ew)
			if err != nil {
				t.Errorf("unexpected error: %s", err)
			}

			err = esController.serviceStore.Add(tc.service)
			if err != nil {
				t.Errorf("unexpected error: %s", err)
			}

			err = esController.syncService(fmt.Sprintf("%s/%s", namespace, serviceName))
			if err != nil {
				t.Errorf("unexpected error: %s", err)
			}

			if len(actions()) != 0 {
				t.Errorf("expected 0 actions, got: %d", len(actions()))
			}

			sliceList, err := client.Client.DiscoveryV1().EndpointSlices(namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				t.Errorf("unexpected error: %s", err)
			}
			if len(sliceList.Items) != 0 {
				t.Errorf("Expected 0 endpoint slices, got: %d", len(sliceList.Items))
			}
		})
	}
}

// Ensure SyncService for service with pending deletion results in no action
func TestSyncServicePendingDeletion(t *testing.T) {
	ns := metav1.NamespaceDefault
	serviceName := "testing-1"
	deletionTimestamp := metav1.Now()
	_, actions, esController := newController(t)
	esController.serviceStore.Add(&v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: ns, DeletionTimestamp: &deletionTimestamp},
		Spec: v1.ServiceSpec{
			Selector: map[string]string{"foo": "bar"},
			Ports:    []v1.ServicePort{{TargetPort: intstr.FromInt32(80)}},
		},
	})

	err := esController.syncService(fmt.Sprintf("%s/%s", ns, serviceName))
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	if len(actions()) != 0 {
		t.Errorf("Expected 0 actions, got: %d", len(actions()))
	}
}

// Ensure SyncService correctly selects ExternalWorkload.
func TestSyncServiceExternalWorkloadSelection(t *testing.T) {
	client, actions, esController := newController(t)
	ns := "test-ns"

	ew1 := newExternalWorkload(1, ns, true, false)
	esController.externalWorkloadsStore.Add(ew1)

	// ensure this ew will not match the selector
	ew2 := newExternalWorkload(2, ns, true, false)
	ew2.Labels["foo"] = "boo"
	esController.externalWorkloadsStore.Add(ew2)

	standardSyncService(t, esController, ns, "testing-1")
	expectActions(t, actions(), 1, "create", "endpointslices")

	// an endpoint slice should be created, it should only reference ew1 (not ew2)
	slices, err := client.Client.DiscoveryV1().EndpointSlices(ns).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("Expected no error fetching endpoint slices, got: %s", err)
	}
	if len(slices.Items) != 1 {
		t.Errorf("Expected 1 endpoint slices, got: %d", len(slices.Items))
	}

	slice := slices.Items[0]
	if len(slice.Endpoints) != 1 {
		t.Errorf("Expected 1 endpoint in first slice, got: %d", len(slice.Endpoints))
	}
	endpoint := slice.Endpoints[0]
	if endpoint.TargetRef.Kind != "ExternalWorkload" || endpoint.TargetRef.Namespace != ns || endpoint.TargetRef.Name != ew1.Name {
		t.Errorf("Expected endpoint to target ExternalWorkload")
	}
}

func TestSyncServiceEndpointSlicePendingDeletion(t *testing.T) {
	client, actions, esController := newController(t)
	ns := "test-ns"
	serviceName := "testing-1"
	service := createService(t, esController, ns, serviceName)
	err := esController.syncService(fmt.Sprintf("%s/%s", ns, serviceName))
	if err != nil {
		t.Fatalf("Expected no error creating EndpointSlice: %v", err)
	}

	gvk := schema.GroupVersionKind{Version: "v1", Kind: "Service"}
	ownerRef := metav1.NewControllerRef(service, gvk)

	deletedTs := metav1.Now()
	endpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "epSlice-1",
			Namespace:       ns,
			OwnerReferences: []metav1.OwnerReference{*ownerRef},
			Labels: map[string]string{
				discoveryv1.LabelServiceName: serviceName,
				discoveryv1.LabelManagedBy:   managedBy,
			},
			DeletionTimestamp: &deletedTs,
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}
	err = esController.endpointSliceStore.Add(endpointSlice)
	if err != nil {
		t.Fatalf("Expected no error adding EndpointSlice: %v", err)
	}
	_, err = client.Client.DiscoveryV1().EndpointSlices(ns).Create(context.TODO(), endpointSlice, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Expected no error creating EndpointSlice: %v", err)
	}

	numActionsBefore := len(actions())
	err = esController.syncService(fmt.Sprintf("%s/%s", ns, serviceName))
	if err != nil {
		t.Errorf("Expected no error syncing service, got: %s", err)
	}

	// The EndpointSlice marked for deletion should be ignored by the controller, and thus
	// should not result in any action.
	if len(actions()) != numActionsBefore {
		t.Errorf("Expected 0 more actions, got %d", len(actions())-numActionsBefore)
	}
}

func makeExternalWorkload(resVersion, name string, labels map[string]string, ports map[int32]string, ips []string) *ewv1beta1.ExternalWorkload {
	portSpecs := []ewv1beta1.PortSpec{}
	for port, name := range ports {
		spec := ewv1beta1.PortSpec{
			Port: port,
		}
		if name != "" {
			spec.Name = name
		}
		portSpecs = append(portSpecs, spec)
	}

	wIps := []ewv1beta1.WorkloadIP{}
	for _, ip := range ips {
		wIps = append(wIps, ewv1beta1.WorkloadIP{Ip: ip})
	}

	ew := &ewv1beta1.ExternalWorkload{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       "ns",
			Labels:          labels,
			ResourceVersion: resVersion,
		},
		Spec: ewv1beta1.ExternalWorkloadSpec{
			MeshTLS: ewv1beta1.MeshTLS{
				Identity:   "some-identity",
				ServerName: "some-sni",
			},
			Ports:       portSpecs,
			WorkloadIPs: wIps,
		},
		Status: ewv1beta1.ExternalWorkloadStatus{
			Conditions: []ewv1beta1.WorkloadCondition{
				{
					Type:   ewv1beta1.WorkloadReady,
					Status: ewv1beta1.ConditionTrue,
				},
			},
		},
	}

	ew.ObjectMeta.UID = types.UID(fmt.Sprintf("%s-%s", ew.Namespace, ew.Name))
	return ew
}

func TestSyncService(t *testing.T) {
	creationTimestamp := metav1.Now()
	namespace := "test-ns"
	testcases := []struct {
		name                  string
		service               *v1.Service
		externalWorkloads     []*ewv1beta1.ExternalWorkload
		expectedEndpointPorts []discoveryv1.EndpointPort
		expectedEndpoints     []discoveryv1.Endpoint
	}{
		{
			name: "EW with multiple IPs and Service with ipFamilies=ipv4",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foobar",
					Namespace:         namespace,
					CreationTimestamp: creationTimestamp,
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{Name: "tcp-example", TargetPort: intstr.FromInt32(80), Protocol: v1.ProtocolTCP},
						{Name: "udp-example", TargetPort: intstr.FromInt32(161), Protocol: v1.ProtocolUDP},
						{Name: "sctp-example", TargetPort: intstr.FromInt32(3456), Protocol: v1.ProtocolSCTP},
					},
					Selector:   map[string]string{"foo": "bar"},
					IPFamilies: []v1.IPFamily{v1.IPv4Protocol},
				},
			},
			externalWorkloads: []*ewv1beta1.ExternalWorkload{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         namespace,
						Name:              "ew0",
						Labels:            map[string]string{"foo": "bar"},
						DeletionTimestamp: nil,
					},
					Spec: ewv1beta1.ExternalWorkloadSpec{
						WorkloadIPs: []ewv1beta1.WorkloadIP{
							{
								Ip: "10.0.0.1",
							},
						},
						Ports: []ewv1beta1.PortSpec{
							{Name: "tcp-example", Port: 80, Protocol: v1.ProtocolTCP},
							{Name: "udp-example", Port: 161, Protocol: v1.ProtocolUDP},
							{Name: "sctp-example", Port: 3456, Protocol: v1.ProtocolSCTP},
						},
					},
					Status: ewv1beta1.ExternalWorkloadStatus{
						Conditions: []ewv1beta1.WorkloadCondition{
							{
								Type:   ewv1beta1.WorkloadReady,
								Status: ewv1beta1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         namespace,
						Name:              "ew1",
						Labels:            map[string]string{"foo": "bar"},
						DeletionTimestamp: nil,
					},

					Spec: ewv1beta1.ExternalWorkloadSpec{
						WorkloadIPs: []ewv1beta1.WorkloadIP{
							{
								Ip: "10.0.0.2",
							},
							{
								Ip: "fd08::5678:0000:0000:9abc:def0",
							},
						},
						Ports: []ewv1beta1.PortSpec{
							{Name: "tcp-example", Port: 80, Protocol: v1.ProtocolTCP},
							{Name: "udp-example", Port: 161, Protocol: v1.ProtocolUDP},
							{Name: "sctp-example", Port: 3456, Protocol: v1.ProtocolSCTP},
						},
					},
					Status: ewv1beta1.ExternalWorkloadStatus{
						Conditions: []ewv1beta1.WorkloadCondition{
							{
								Type:   ewv1beta1.WorkloadReady,
								Status: ewv1beta1.ConditionTrue,
							},
						},
					},
				},
			},
			expectedEndpointPorts: []discoveryv1.EndpointPort{
				{
					Name:     ptr.To("udp-example"),
					Protocol: protoPtr(v1.ProtocolUDP),
					Port:     ptr.To(int32(161)),
				},
				{
					Name:     ptr.To("tcp-example"),
					Protocol: protoPtr(v1.ProtocolTCP),
					Port:     ptr.To(int32(80)),
				},
				{
					Name:     ptr.To("sctp-example"),
					Protocol: protoPtr(v1.ProtocolSCTP),
					Port:     ptr.To(int32(3456)),
				},
			},
			expectedEndpoints: []discoveryv1.Endpoint{
				{
					Conditions: discoveryv1.EndpointConditions{
						Ready:       ptr.To(true),
						Serving:     ptr.To(true),
						Terminating: ptr.To(false),
					},
					Addresses: []string{"10.0.0.1"},
					TargetRef: &v1.ObjectReference{Kind: "ExternalWorkload", Namespace: namespace, Name: "ew0"},
				},
				{
					Conditions: discoveryv1.EndpointConditions{
						Ready:       ptr.To(true),
						Serving:     ptr.To(true),
						Terminating: ptr.To(false),
					},
					Addresses: []string{"10.0.0.2"},
					TargetRef: &v1.ObjectReference{Kind: "ExternalWorkload", Namespace: namespace, Name: "ew1"},
				},
			},
		},
		{
			name: "EWs with multiple IPs and Service with ipFamilies=ipv6",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foobar",
					Namespace:         namespace,
					CreationTimestamp: creationTimestamp,
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{Name: "tcp-example", TargetPort: intstr.FromInt32(80), Protocol: v1.ProtocolTCP},
						{Name: "udp-example", TargetPort: intstr.FromInt32(161), Protocol: v1.ProtocolUDP},
						{Name: "sctp-example", TargetPort: intstr.FromInt32(3456), Protocol: v1.ProtocolSCTP},
					},
					Selector:   map[string]string{"foo": "bar"},
					IPFamilies: []v1.IPFamily{v1.IPv6Protocol},
				},
			},
			externalWorkloads: []*ewv1beta1.ExternalWorkload{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         namespace,
						Name:              "ew0",
						Labels:            map[string]string{"foo": "bar"},
						DeletionTimestamp: nil,
					},
					Spec: ewv1beta1.ExternalWorkloadSpec{
						WorkloadIPs: []ewv1beta1.WorkloadIP{
							{
								Ip: "10.0.0.1",
							},
						},
						Ports: []ewv1beta1.PortSpec{
							{Name: "tcp-example", Port: 80, Protocol: v1.ProtocolTCP},
							{Name: "udp-example", Port: 161, Protocol: v1.ProtocolUDP},
							{Name: "sctp-example", Port: 3456, Protocol: v1.ProtocolSCTP},
						},
					},
					Status: ewv1beta1.ExternalWorkloadStatus{
						Conditions: []ewv1beta1.WorkloadCondition{
							{
								Type:   ewv1beta1.WorkloadReady,
								Status: ewv1beta1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         namespace,
						Name:              "ew1",
						Labels:            map[string]string{"foo": "bar"},
						DeletionTimestamp: nil,
					},
					Spec: ewv1beta1.ExternalWorkloadSpec{
						WorkloadIPs: []ewv1beta1.WorkloadIP{
							{
								Ip: "10.0.0.2",
							},
							{

								Ip: "fd08::5678:0000:0000:9abc:def0",
							},
						},
						Ports: []ewv1beta1.PortSpec{
							{Name: "tcp-example", Port: 80, Protocol: v1.ProtocolTCP},
							{Name: "udp-example", Port: 161, Protocol: v1.ProtocolUDP},
							{Name: "sctp-example", Port: 3456, Protocol: v1.ProtocolSCTP},
						},
					},
					Status: ewv1beta1.ExternalWorkloadStatus{
						Conditions: []ewv1beta1.WorkloadCondition{
							{
								Type:   ewv1beta1.WorkloadReady,
								Status: ewv1beta1.ConditionTrue,
							},
						},
					},
				},
			},
			expectedEndpointPorts: []discoveryv1.EndpointPort{
				{
					Name:     ptr.To("udp-example"),
					Protocol: protoPtr(v1.ProtocolUDP),
					Port:     ptr.To(int32(161)),
				},
				{
					Name:     ptr.To("tcp-example"),
					Protocol: protoPtr(v1.ProtocolTCP),
					Port:     ptr.To(int32(80)),
				},
				{
					Name:     ptr.To("sctp-example"),
					Protocol: protoPtr(v1.ProtocolSCTP),
					Port:     ptr.To(int32(3456)),
				},
			},
			expectedEndpoints: []discoveryv1.Endpoint{
				{
					Conditions: discoveryv1.EndpointConditions{
						Ready:       ptr.To(true),
						Serving:     ptr.To(true),
						Terminating: ptr.To(false),
					},
					Addresses: []string{"fd08::5678:0000:0000:9abc:def0"},
					TargetRef: &v1.ObjectReference{Kind: "ExternalWorkload", Namespace: namespace, Name: "ew1"},
				},
			},
		},
		{
			name: "Not ready workloads",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foobar",
					Namespace:         namespace,
					CreationTimestamp: creationTimestamp,
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{Name: "tcp-example", TargetPort: intstr.FromInt32(80), Protocol: v1.ProtocolTCP},
						{Name: "udp-example", TargetPort: intstr.FromInt32(161), Protocol: v1.ProtocolUDP},
						{Name: "sctp-example", TargetPort: intstr.FromInt32(3456), Protocol: v1.ProtocolSCTP},
					},
					Selector:   map[string]string{"foo": "bar"},
					IPFamilies: []v1.IPFamily{v1.IPv4Protocol},
				},
			},
			externalWorkloads: []*ewv1beta1.ExternalWorkload{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         namespace,
						Name:              "ew0",
						Labels:            map[string]string{"foo": "bar"},
						DeletionTimestamp: nil,
					},
					Spec: ewv1beta1.ExternalWorkloadSpec{
						WorkloadIPs: []ewv1beta1.WorkloadIP{
							{
								Ip: "10.0.0.1",
							},
						},
						Ports: []ewv1beta1.PortSpec{
							{Name: "tcp-example", Port: 80, Protocol: v1.ProtocolTCP},
							{Name: "udp-example", Port: 161, Protocol: v1.ProtocolUDP},
							{Name: "sctp-example", Port: 3456, Protocol: v1.ProtocolSCTP},
						},
					},
					Status: ewv1beta1.ExternalWorkloadStatus{
						Conditions: []ewv1beta1.WorkloadCondition{
							{
								Type:   ewv1beta1.WorkloadReady,
								Status: ewv1beta1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         namespace,
						Name:              "ew1",
						Labels:            map[string]string{"foo": "bar"},
						DeletionTimestamp: nil,
					},
					Spec: ewv1beta1.ExternalWorkloadSpec{
						WorkloadIPs: []ewv1beta1.WorkloadIP{
							{
								Ip: "10.0.0.2",
							},
							{

								Ip: "fd08::5678:0000:0000:9abc:def0",
							},
						},
						Ports: []ewv1beta1.PortSpec{
							{Name: "tcp-example", Port: 80, Protocol: v1.ProtocolTCP},
							{Name: "udp-example", Port: 161, Protocol: v1.ProtocolUDP},
							{Name: "sctp-example", Port: 3456, Protocol: v1.ProtocolSCTP},
						},
					},
					Status: ewv1beta1.ExternalWorkloadStatus{
						Conditions: []ewv1beta1.WorkloadCondition{
							{
								Type:   ewv1beta1.WorkloadReady,
								Status: ewv1beta1.ConditionFalse,
							},
						},
					},
				},
			},
			expectedEndpointPorts: []discoveryv1.EndpointPort{
				{
					Name:     ptr.To("udp-example"),
					Protocol: protoPtr(v1.ProtocolUDP),
					Port:     ptr.To(int32(161)),
				},
				{
					Name:     ptr.To("tcp-example"),
					Protocol: protoPtr(v1.ProtocolTCP),
					Port:     ptr.To(int32(80)),
				},
				{
					Name:     ptr.To("sctp-example"),
					Protocol: protoPtr(v1.ProtocolSCTP),
					Port:     ptr.To(int32(3456)),
				},
			},
			expectedEndpoints: []discoveryv1.Endpoint{
				{
					Conditions: discoveryv1.EndpointConditions{
						Ready:       ptr.To(true),
						Serving:     ptr.To(true),
						Terminating: ptr.To(false),
					},
					Addresses: []string{"10.0.0.1"},
					TargetRef: &v1.ObjectReference{Kind: "ExternalWorkload", Namespace: namespace, Name: "ew0"},
				},
				{
					Conditions: discoveryv1.EndpointConditions{
						Ready:       ptr.To(false),
						Serving:     ptr.To(false),
						Terminating: ptr.To(false),
					},
					Addresses: []string{"10.0.0.2"},
					TargetRef: &v1.ObjectReference{Kind: "ExternalWorkload", Namespace: namespace, Name: "ew1"},
				},
			},
		},
		{
			name: "Two Ready workloads with the same IPs",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foobar",
					Namespace:         namespace,
					CreationTimestamp: creationTimestamp,
				},
				Spec: v1.ServiceSpec{
					Ports: []v1.ServicePort{
						{Name: "tcp-example", TargetPort: intstr.FromInt32(80), Protocol: v1.ProtocolTCP},
						{Name: "udp-example", TargetPort: intstr.FromInt32(161), Protocol: v1.ProtocolUDP},
						{Name: "sctp-example", TargetPort: intstr.FromInt32(3456), Protocol: v1.ProtocolSCTP},
					},
					Selector:   map[string]string{"foo": "bar"},
					IPFamilies: []v1.IPFamily{v1.IPv4Protocol},
				},
			},
			externalWorkloads: []*ewv1beta1.ExternalWorkload{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         namespace,
						Name:              "ew0",
						Labels:            map[string]string{"foo": "bar"},
						DeletionTimestamp: nil,
					},
					Spec: ewv1beta1.ExternalWorkloadSpec{
						WorkloadIPs: []ewv1beta1.WorkloadIP{
							{
								Ip: "10.0.0.1",
							},
						},
						Ports: []ewv1beta1.PortSpec{
							{Name: "tcp-example", Port: 80, Protocol: v1.ProtocolTCP},
							{Name: "udp-example", Port: 161, Protocol: v1.ProtocolUDP},
							{Name: "sctp-example", Port: 3456, Protocol: v1.ProtocolSCTP},
						},
					},
					Status: ewv1beta1.ExternalWorkloadStatus{
						Conditions: []ewv1beta1.WorkloadCondition{
							{
								Type:   ewv1beta1.WorkloadReady,
								Status: ewv1beta1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         namespace,
						Name:              "ew1",
						Labels:            map[string]string{"foo": "bar"},
						DeletionTimestamp: nil,
					},
					Spec: ewv1beta1.ExternalWorkloadSpec{
						WorkloadIPs: []ewv1beta1.WorkloadIP{
							{
								Ip: "10.0.0.1",
							},
						},
						Ports: []ewv1beta1.PortSpec{
							{Name: "tcp-example", Port: 80, Protocol: v1.ProtocolTCP},
							{Name: "udp-example", Port: 161, Protocol: v1.ProtocolUDP},
							{Name: "sctp-example", Port: 3456, Protocol: v1.ProtocolSCTP},
						},
					},
					Status: ewv1beta1.ExternalWorkloadStatus{
						Conditions: []ewv1beta1.WorkloadCondition{
							{
								Type:   ewv1beta1.WorkloadReady,
								Status: ewv1beta1.ConditionTrue,
							},
						},
					},
				},
			},
			expectedEndpointPorts: []discoveryv1.EndpointPort{
				{
					Name:     ptr.To("udp-example"),
					Protocol: protoPtr(v1.ProtocolUDP),
					Port:     ptr.To(int32(161)),
				},
				{
					Name:     ptr.To("tcp-example"),
					Protocol: protoPtr(v1.ProtocolTCP),
					Port:     ptr.To(int32(80)),
				},
				{
					Name:     ptr.To("sctp-example"),
					Protocol: protoPtr(v1.ProtocolSCTP),
					Port:     ptr.To(int32(3456)),
				},
			},
			expectedEndpoints: []discoveryv1.Endpoint{
				{
					Conditions: discoveryv1.EndpointConditions{
						Ready:       ptr.To(true),
						Serving:     ptr.To(true),
						Terminating: ptr.To(false),
					},
					Addresses: []string{"10.0.0.1"},
					TargetRef: &v1.ObjectReference{Kind: "ExternalWorkload", Namespace: namespace, Name: "ew0"},
				},
				{
					Conditions: discoveryv1.EndpointConditions{
						Ready:       ptr.To(true),
						Serving:     ptr.To(true),
						Terminating: ptr.To(false),
					},
					Addresses: []string{"10.0.0.1"},
					TargetRef: &v1.ObjectReference{Kind: "ExternalWorkload", Namespace: namespace, Name: "ew1"},
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			client, actions, esController := newController(t)

			for _, ew := range testcase.externalWorkloads {
				esController.externalWorkloadsStore.Add(ew)
			}
			esController.serviceStore.Add(testcase.service)

			_, err := esController.k8sAPI.Client.CoreV1().Services(testcase.service.Namespace).Create(context.TODO(), testcase.service, metav1.CreateOptions{})
			if err != nil {
				t.Errorf("Expected no error creating service, got: %s", err)
			}
			err = esController.syncService(fmt.Sprintf("%s/%s", testcase.service.Namespace, testcase.service.Name))
			if err != nil {
				t.Errorf("Expected no error, got: %s", err)
			}

			// last action should be to create endpoint slice
			expectActions(t, actions(), 1, "create", "endpointslices")
			sliceList, err := client.Client.DiscoveryV1().EndpointSlices(testcase.service.Namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				t.Errorf("Expected no error fetching endpoint slices, got: %s", err)
			}

			if len(sliceList.Items) != 1 {
				t.Errorf("Expected 1 endpoints slices")
			}

			// ensure all attributes of endpoint slice match expected state
			slice := sliceList.Items[0]

			// check expected ports

			if !reflect.DeepEqual(testcase.expectedEndpointPorts, slice.Ports) {
				t.Error("actual ports do not match expected ones")

				for i, ep := range slice.Ports {
					t.Logf(
						"actual port[%d]: name=%s proto=%v port=%v",
						i,
						ptr.Deref(ep.Name, "<nil>"),
						ptr.Deref(ep.Protocol, "<nil>"),
						ptr.Deref(ep.Port, 0),
					)
				}

				for i, ep := range testcase.expectedEndpointPorts {
					t.Logf(
						"expected port[%d]: name=%s proto=%v port=%v",
						i,
						ptr.Deref(ep.Name, "<nil>"),
						ptr.Deref(ep.Protocol, "<nil>"),
						ptr.Deref(ep.Port, 0),
					)
				}
			}

			// sort actual endpoints in terms of targetRef name, in ascending
			// order. This will ensure reflection package doesn't give a
			// spurious error.

			sort.Slice(slice.Endpoints, func(i, j int) bool {
				return slice.Endpoints[i].TargetRef.Name < slice.Endpoints[j].TargetRef.Name
			})

			if !reflect.DeepEqual(testcase.expectedEndpoints, slice.Endpoints) {
				t.Error("actual endpoints do not match expected ones")
			}
		})
	}
}

// Test diffing logic that determines if two workloads with the same name and
// namespace have changed enough to warrant reconciliation
// TODO: Add tests for labels change
func TestEwEndpointsChanged(t *testing.T) {
	for _, tt := range []struct {
		name        string
		old         *ewv1beta1.ExternalWorkload
		updated     *ewv1beta1.ExternalWorkload
		specChanged bool
	}{
		{
			name: "no change",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			updated: makeExternalWorkload(
				"1",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			specChanged: false,
		},
		{
			name: "updated workload adds an IP address",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			updated: makeExternalWorkload(
				"2",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0", "192.0.3.0"},
			),
			specChanged: true,
		},
		{
			name: "updated workload removes an IP address",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0", "192.0.3.0"},
			),
			updated: makeExternalWorkload(
				"2",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			specChanged: true,
		},
		{
			name: "updated workload changes an IP address",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			updated: makeExternalWorkload(
				"2",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.3.0"},
			),
			specChanged: true,
		},
		{
			name: "updated workload adds new port",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			updated: makeExternalWorkload(
				"2",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1", 2: "port-2"},
				[]string{"192.0.2.0"},
			),
			specChanged: true,
		},
		{
			name: "updated workload removes port",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1", 2: "port-2"},
				[]string{"192.0.2.0"},
			),
			updated: makeExternalWorkload(
				"2",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			specChanged: true,
		},
		{
			name: "updated workload changes port number",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			updated: makeExternalWorkload(
				"2",
				"wlkd1",
				nil,
				map[int32]string{2: "port-1"},
				[]string{"192.0.2.0"},
			),
			specChanged: true,
		},
		{
			name: "updated workload changes port name",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			updated: makeExternalWorkload(
				"2",
				"wlkd1",
				nil,
				map[int32]string{1: "port-foo"},
				[]string{"192.0.2.0"},
			),
			specChanged: true,
		},
		{
			name: "updated workload removes port name",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				nil,
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			updated: makeExternalWorkload(
				"2",
				"wlkd1",
				nil,
				map[int32]string{1: ""},
				[]string{"192.0.2.0"},
			),
			specChanged: true,
		},
	} {
		tt := tt // Pin
		t.Run(tt.name, func(t *testing.T) {
			specChanged, _ := ewEndpointsChanged(tt.old, tt.updated)
			if tt.specChanged != specChanged {
				t.Errorf("expected specChanged '%v', got '%v'", tt.specChanged, specChanged)
			}
		})
	}
}

// Test diffing logic that determines if two workloads with the same name and
// namespace have changed enough to warrant reconciliation
func TestWorkloadServicesToUpdate(t *testing.T) {
	for _, tt := range []struct {
		name           string
		old            *ewv1beta1.ExternalWorkload
		updated        *ewv1beta1.ExternalWorkload
		k8sConfigs     []string
		expectServices map[string]struct{}
	}{
		{
			name: "no change",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				map[string]string{"app": "test"},
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			updated: makeExternalWorkload(
				"1",
				"wlkd1",
				map[string]string{"app": "test"},
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			k8sConfigs: []string{`
            apiVersion: v1
            kind: Service
            metadata:
              name: svc-1
              namespace: ns
            spec:
              selector:
                app: test`,
			},
			expectServices: map[string]struct{}{},
		},
		{
			name: "labels and spec have changed",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				map[string]string{"app": "test-1"},
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			updated: makeExternalWorkload(
				"2",
				"wlkd1",
				map[string]string{"app": "test-2"},
				map[int32]string{2: "port-1"},
				[]string{"192.0.2.0"},
			),
			k8sConfigs: []string{`
            apiVersion: v1
            kind: Service
            metadata:
              name: svc-1
              namespace: ns
            spec:
              selector:
                app: test-1`, `
            apiVersion: v1
            kind: Service
            metadata:
              name: svc-2
              namespace: ns
            spec:
              selector:
                app: test-2`,
			},
			expectServices: map[string]struct{}{"ns/svc-1": {}, "ns/svc-2": {}},
		},
		{
			name: "spec has changed",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				map[string]string{"app": "test-1"},
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			updated: makeExternalWorkload(
				"2",
				"wlkd1",
				map[string]string{"app": "test-1"},
				map[int32]string{2: "port-1"},
				[]string{"192.0.2.0"},
			),
			k8sConfigs: []string{`
            apiVersion: v1
            kind: Service
            metadata:
              name: svc-1
              namespace: ns
            spec:
              selector:
                app: test-1`,
			},
			expectServices: map[string]struct{}{"ns/svc-1": {}},
		},
		{
			name: "labels have changed",
			old: makeExternalWorkload(
				"1",
				"wlkd1",
				map[string]string{"app": "test-1", "env": "staging"},
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			updated: makeExternalWorkload(
				"2",
				"wlkd1",
				map[string]string{"app": "test-1", "env": "prod"},
				map[int32]string{1: "port-1"},
				[]string{"192.0.2.0"},
			),
			k8sConfigs: []string{`
            apiVersion: v1
            kind: Service
            metadata:
              name: internal
              namespace: ns
            spec:
              selector:
                app: test-1`, `
            apiVersion: v1
            kind: Service
            metadata:
              name: staging
              namespace: ns
            spec:
              selector:
                env: staging`, `
            apiVersion: v1
            kind: Service
            metadata:
              name: prod
              namespace: ns
            spec:
              selector:
                env: prod`,
			},
			expectServices: map[string]struct{}{"ns/staging": {}, "ns/prod": {}},
		}} {
		tt := tt // Pin
		t.Run(tt.name, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			ec, err := NewEndpointsController(k8sAPI, "my-hostname", "controlplane-ns", make(chan struct{}), false)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}

			ec.Start()
			k8sAPI.Sync(nil)

			services := ec.getServicesToUpdateOnExternalWorkloadChange(tt.old, tt.updated)
			if len(services) != len(tt.expectServices) {
				t.Fatalf("expected %d services to update, got %d services instead", len(tt.expectServices), len(services))
			}

			for svc := range services {
				if _, ok := tt.expectServices[svc]; !ok {
					t.Errorf("unexpected service key %s found in list of results", svc)
				}
			}
		})

	}
}

// Assert that de-registering handlers won't result in cache staleness issues
//
// The test will simulate a scenario where a lease is acquired, an endpointslice
// created, and the lease is lost. Without wiping out state, this test will
// fail, since any changes made to the resources will not be observed while the
// lease is not held; these changes will result in stale cache entries (since
// the state diverged).
func TestLeaderElectionSyncsState(t *testing.T) {
	client, actions, esController := newController(t)
	ns := "test-ns"
	service := createService(t, esController, ns, "test-svc")
	ew1 := newExternalWorkload(1, ns, false, true)
	esController.serviceStore.Add(service)
	esController.externalWorkloadsStore.Add(ew1)

	// Simulate a lease being acquired,
	err := esController.addHandlers()
	if err != nil {
		t.Fatalf("unexpected error when registering client-go callbacks: %v", err)
	}

	err = esController.syncService(fmt.Sprintf("%s/%s", ns, service.Name))
	if err != nil {
		t.Fatalf("unexpected error when processing service %s/%s: %v", ns, service.Name, err)
	}
	expectActions(t, actions(), 1, "create", "endpointslices")

	slices, err := client.Client.DiscoveryV1().EndpointSlices(ns).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("expected no error fetching endpoint slices, got: %s", err)
	}
	if len(slices.Items) != 1 {
		t.Errorf("expected 1 endpoint slices, got: %d", len(slices.Items))
	}
	sliceName := slices.Items[0].Name

	// Simulate a lease being lost; we delete the previously created
	// endpointslice out-of-band.
	err = esController.removeHandlers()
	if err != nil {
		t.Fatalf("unexpected error when de-registering client-go callbacks: %v", err)
	}
	err = client.Client.DiscoveryV1().EndpointSlices(ns).Delete(context.TODO(), sliceName, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("unexpected error when deleting endpointslice %s/%s: %v", ns, sliceName, err)
	}
	slices, err = client.Client.DiscoveryV1().EndpointSlices(ns).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("expected no error fetching endpoint slices, got: %s", err)
	}
	if len(slices.Items) != 0 {
		t.Errorf("expected 0 endpoint slices, got: %d", len(slices.Items))
	}

	// The lease is re-acquired. We should start with a clean slate to avoid
	// cache staleness errors.
	esController.addHandlers()
	err = esController.syncService(fmt.Sprintf("%s/%s", ns, service.Name))
	if err != nil {
		t.Fatalf("unexpected error when processing service %s/%s: %v", ns, service.Name, err)
	}
	expectActions(t, actions(), 1, "create", "endpointslices")
	slices, err = client.Client.DiscoveryV1().EndpointSlices(ns).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("expected no error fetching endpoint slices, got: %s", err)
	}
	if len(slices.Items) != 1 {
		t.Errorf("expected 1 endpoint slices, got: %d", len(slices.Items))
	}
	if slices.Items[0].Name == sliceName {
		t.Fatalf("expected newly created slice's name to be different than the initial slice, got: %s", sliceName)
	}

}

// protoPtr takes a Protocol and returns a pointer to it.
func protoPtr(proto v1.Protocol) *v1.Protocol {
	return &proto
}

func newStatusCondition(ready bool) ewv1beta1.WorkloadCondition {
	var status ewv1beta1.WorkloadConditionStatus
	if ready {
		status = ewv1beta1.ConditionTrue
	} else {
		status = ewv1beta1.ConditionFalse
	}
	return ewv1beta1.WorkloadCondition{
		Type:               ewv1beta1.WorkloadReady,
		Status:             status,
		LastProbeTime:      metav1.Time{},
		LastTransitionTime: metav1.NewTime(time.Now()),
		Reason:             "test",
		Message:            "test",
	}
}

//nolint:all
func expectActions(t *testing.T, actions []k8stesting.Action, num int, verb, resource string) {
	t.Helper()
	// if actions are less the below logic will panic
	if num > len(actions) {
		t.Fatalf("len of actions %v is unexpected. Expected to be at least %v", len(actions), num+1)
	}

	for i := 0; i < num; i++ {
		relativePos := len(actions) - i - 1
		if actions[relativePos].GetVerb() != verb {
			t.Errorf("Expected action -%d verb to be %s, was: %s", i, verb, actions[relativePos].GetVerb())
		}
		if resource != actions[relativePos].GetResource().Resource {
			t.Errorf("Expected action -%d resource to be %s, was: %s", i, resource, actions[relativePos].GetResource().Resource)
		}
	}
}

func createService(t *testing.T, esController *endpointSliceController, namespace, serviceName string) *v1.Service {
	t.Helper()
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:              serviceName,
			Namespace:         namespace,
			CreationTimestamp: metav1.NewTime(time.Now()),
			UID:               types.UID(namespace + "-" + serviceName),
		},
		Spec: v1.ServiceSpec{
			Ports:      []v1.ServicePort{{TargetPort: intstr.FromInt32(80)}},
			Selector:   map[string]string{"foo": "bar"},
			IPFamilies: []v1.IPFamily{v1.IPv4Protocol},
		},
	}
	esController.serviceStore.Add(service)
	_, err := esController.k8sAPI.Client.CoreV1().Services(namespace).Create(context.TODO(), service, metav1.CreateOptions{})
	if err != nil {
		t.Error("Expected no error creating service")
	}
	return service
}

func standardSyncService(t *testing.T, esController *endpointSliceController, namespace, serviceName string) {
	t.Helper()
	createService(t, esController, namespace, serviceName)

	err := esController.syncService(fmt.Sprintf("%s/%s", namespace, serviceName))
	if err != nil {
		t.Error("Expected no error syncing service")
	}
}
