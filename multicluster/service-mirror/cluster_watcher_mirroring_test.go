package servicemirror

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-test/deep"
	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

type mirroringTestCase struct {
	description            string
	environment            *testEnvironment
	expectedLocalServices  []*corev1.Service
	expectedLocalEndpoints []*corev1.Endpoints
	expectedEventsInQueue  []interface{}
}

func (tc *mirroringTestCase) run(t *testing.T) {
	t.Helper()
	t.Run(tc.description, func(t *testing.T) {
		t.Helper()

		q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())
		localAPI, err := tc.environment.runEnvironment(q)
		if err != nil {
			t.Fatal(err)
		}
		if tc.expectedLocalServices == nil {
			// ensure the are no local services
			services, err := localAPI.Client.CoreV1().Services(corev1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}

			if len(services.Items) > 0 {
				t.Fatalf("Was expecting no local services but instead found %v", services.Items)

			}
		} else {
			for _, expected := range tc.expectedLocalServices {
				actual, err := localAPI.Client.CoreV1().Services(expected.Namespace).Get(context.Background(), expected.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Could not find mirrored service with name %s", expected.Name)
				}

				if err := diffServices(expected, actual); err != nil {
					t.Fatalf("service %s/%s: %v", expected.Namespace, expected.Name, err)
				}
			}
		}

		if tc.expectedLocalEndpoints == nil {
			// In a real Kubernetes cluster, deleting the service is sufficient
			// to delete the endpoints.
		} else {
			for _, expected := range tc.expectedLocalEndpoints {
				actual, err := localAPI.Client.CoreV1().Endpoints(expected.Namespace).Get(context.Background(), expected.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Could not find endpoints with name %s", expected.Name)
				}

				if err := diffEndpoints(expected, actual); err != nil {
					t.Fatalf("endpoint %s/%s: %v", expected.Namespace, expected.Name, err)
				}
			}
		}

		expectedNumEvents := len(tc.expectedEventsInQueue)
		actualNumEvents := q.Len()

		if expectedNumEvents != actualNumEvents {
			t.Fatalf("Was expecting %d events but got %d", expectedNumEvents, actualNumEvents)
		}

		for _, ev := range tc.expectedEventsInQueue {
			evInQueue, _ := q.Get()
			if diff := deep.Equal(ev, evInQueue); diff != nil {
				t.Errorf("%v", diff)
			}
		}
	})
}

func TestRemoteServiceCreatedMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "create service and endpoints when gateway can be resolved",
			environment: createExportedService,
			expectedLocalServices: []*corev1.Service{
				mirrorService(
					"service-one-remote",
					"ns1",
					"111",
					map[string]string{"lk": "lv"},
					[]corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     555,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     666,
						},
					}),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("service-one-remote", "ns1", map[string]string{"lk": "lv"}, "192.0.2.127", "gateway-identity", []corev1.EndpointPort{
					{
						Name:     "port1",
						Port:     888,
						Protocol: "TCP",
					},
					{
						Name:     "port2",
						Port:     888,
						Protocol: "TCP",
					},
				}),
			},
		},
		{
			description: "create headless service and endpoints when gateway can be resolved",
			environment: createExportedHeadlessService,
			expectedLocalServices: []*corev1.Service{
				headlessMirrorService(
					"service-one-remote",
					"ns2",
					"111",
					map[string]string{"lk": "lv"},
					[]corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     555,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     666,
						},
					}),
				endpointMirrorService(
					"pod-0",
					"service-one-remote",
					"ns2",
					"112",
					map[string]string{"lk": "lv"},
					[]corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     555,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     666,
						},
					},
				),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				headlessMirrorEndpoints("service-one-remote", "ns2", map[string]string{"lk": "lv"}, "gateway-identity", []corev1.EndpointPort{
					{
						Name:     "port1",
						Port:     555,
						Protocol: "TCP",
					},
					{
						Name:     "port2",
						Port:     666,
						Protocol: "TCP",
					},
				}),
				endpointMirrorEndpoints(
					"service-one-remote",
					"ns2",
					map[string]string{"lk": "lv"},
					"pod-0",
					"192.0.2.129",
					"gateway-identity",
					[]corev1.EndpointPort{
						{
							Name:     "port1",
							Port:     889,
							Protocol: "TCP",
						},
						{
							Name:     "port2",
							Port:     889,
							Protocol: "TCP",
						},
					}),
			},
		},
		{
			description: "remote discovery mirroring",
			environment: createRemoteDiscoveryService,
			expectedLocalServices: []*corev1.Service{
				remoteDiscoveryMirrorService(
					"service-one",
					"ns1",
					"111",
					[]corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     555,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     666,
						},
					}),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{},
		},
		{
			description: "link with no gateway mirrors only remote discovery",
			environment: noGatewayLink,
			expectedLocalServices: []*corev1.Service{
				remoteDiscoveryMirrorService(
					"service-one",
					"ns1",
					"111",
					[]corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     555,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     666,
						},
					}),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{},
		},
		{
			description: "federated service created",
			environment: createFederatedService,
			expectedLocalServices: []*corev1.Service{
				federatedService(
					"service-one",
					"ns1",
					[]corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     555,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     666,
						},
					},
					"", fmt.Sprintf("%s@%s", "service-one", clusterName)),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{},
		},
		{
			description: "federated service joined",
			environment: joinFederatedService(),
			expectedLocalServices: []*corev1.Service{
				federatedService(
					"service-one",
					"ns1",
					[]corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     555,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     666,
						},
					}, "", fmt.Sprintf("%s@%s,%s@%s", "service-one", "other", "service-one", clusterName)),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{},
		},
		{
			description: "federated service left",
			environment: leftFederatedService,
			expectedLocalServices: []*corev1.Service{
				federatedService(
					"service-one",
					"ns1",
					[]corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     555,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     666,
						},
					}, "", fmt.Sprintf("%s@%s", "service-one", "other")),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{},
		},
		{
			description: "local federated service created",
			environment: createLocalFederatedService,
			expectedLocalServices: []*corev1.Service{
				federatedService(
					"service-one",
					"ns1",
					[]corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     555,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     666,
						},
					}, "service-one", ""),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{},
		},
		{
			description: "local federated service joined",
			environment: joinLocalFederatedService(),
			expectedLocalServices: []*corev1.Service{
				federatedService(
					"service-one",
					"ns1",
					[]corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     555,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     666,
						},
					}, "service-one", "service-one@other"),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{},
		},
		{
			description: "local federated service left",
			environment: leftLocalFederatedService,
			expectedLocalServices: []*corev1.Service{
				federatedService(
					"service-one",
					"ns1",
					[]corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     555,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     666,
						},
					}, "", "service-one@other"),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{},
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}

func TestLocalNamespaceCreatedAfterServiceExport(t *testing.T) {
	remoteAPI, err := k8s.NewFakeAPI(
		asYaml(gateway("existing-gateway", "existing-namespace", "222", "192.0.2.127", "mc-gateway", 888, "gateway-identity", defaultProbePort, defaultProbePath, defaultProbePeriod)),
		asYaml(remoteService("service-one", "ns1", "111", map[string]string{consts.DefaultExportedServiceSelector: "true"}, []corev1.ServicePort{})),
		asYaml(endpoints("service-one", "ns1", nil, "192.0.2.127", "gateway-identity", []corev1.EndpointPort{})),
	)
	if err != nil {
		t.Fatal(err)
	}
	localAPI, err := k8s.NewFakeAPIWithL5dClient()
	if err != nil {
		t.Fatal(err)
	}
	linksAPI := k8s.NewL5dNamespacedAPI(localAPI.L5dClient, "linkerd-multicluster", "local", k8s.Link)
	remoteAPI.Sync(nil)
	localAPI.Sync(nil)
	linksAPI.Sync(nil)

	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())
	eventRecorder := record.NewFakeRecorder(100)

	watcher := RemoteClusterServiceWatcher{
		link: &v1alpha3.Link{
			Spec: v1alpha3.LinkSpec{
				TargetClusterName:       clusterName,
				TargetClusterDomain:     clusterDomain,
				GatewayIdentity:         "gateway-identity",
				GatewayAddress:          "192.0.2.127",
				GatewayPort:             "888",
				ProbeSpec:               defaultProbeSpec,
				Selector:                defaultSelector,
				RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
			},
		},
		remoteAPIClient:         remoteAPI,
		localAPIClient:          localAPI,
		linksAPIClient:          linksAPI,
		stopper:                 nil,
		recorder:                eventRecorder,
		log:                     logging.WithFields(logging.Fields{"cluster": clusterName}),
		eventsQueue:             q,
		requeueLimit:            0,
		gatewayAlive:            true,
		headlessServicesEnabled: true,
	}

	q.Add(&RemoteServiceExported{
		service: remoteService("service-one", "ns1", "111", map[string]string{
			consts.DefaultExportedServiceSelector: "true",
		}, []corev1.ServicePort{
			{
				Name:     "port1",
				Protocol: "TCP",
				Port:     555,
			},
			{
				Name:     "port2",
				Protocol: "TCP",
				Port:     666,
			},
		}),
	})
	for q.Len() > 0 {
		watcher.processNextEvent(context.Background())
	}

	_, err = localAPI.Svc().Lister().Services("ns1").Get("service-one-remote")
	if err == nil {
		t.Fatalf("service-one should not exist in local cluster before namespace is created")
	} else if !errors.IsNotFound(err) {
		t.Fatalf("unexpected error: %v", err)
	}

	skippedEvent := <-eventRecorder.Events
	if skippedEvent != fmt.Sprintf("%s %s %s", corev1.EventTypeNormal, eventTypeSkipped, "Skipped mirroring service: namespace does not exist") {
		t.Error("Expected skipped event, got:", skippedEvent)
	}

	ns, err := localAPI.Client.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	q.Add(&OnLocalNamespaceAdded{ns})
	for q.Len() > 0 {
		watcher.processNextEvent(context.Background())
	}

	_, err = localAPI.Client.CoreV1().Services("ns1").Get(context.Background(), "service-one-remote", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting service-one locally: %v", err)
	}
}

func TestServiceCreatedGatewayAlive(t *testing.T) {
	remoteAPI, err := k8s.NewFakeAPI(
		asYaml(gateway("gateway", "gateway-ns", "1", "192.0.0.1", "gateway", 888, "gateway-identity", defaultProbePort, defaultProbePath, defaultProbePeriod)),
		asYaml(remoteService("svc", "ns", "1", map[string]string{consts.DefaultExportedServiceSelector: "true"}, []corev1.ServicePort{})),
		asYaml(endpoints("svc", "ns", nil, "192.0.0.1", "gateway-identity", []corev1.EndpointPort{})),
	)
	if err != nil {
		t.Fatal(err)
	}
	localAPI, err := k8s.NewFakeAPIWithL5dClient(
		asYaml(namespace("ns")),
	)
	if err != nil {
		t.Fatal(err)
	}
	linksAPI := k8s.NewL5dNamespacedAPI(localAPI.L5dClient, "linkerd-multicluster", "local", k8s.Link)
	remoteAPI.Sync(nil)
	localAPI.Sync(nil)
	linksAPI.Sync(nil)

	events := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())
	watcher := RemoteClusterServiceWatcher{
		link: &v1alpha3.Link{
			Spec: v1alpha3.LinkSpec{
				TargetClusterName:       clusterName,
				TargetClusterDomain:     clusterDomain,
				GatewayIdentity:         "gateway-identity",
				GatewayAddress:          "192.0.0.1",
				GatewayPort:             "888",
				ProbeSpec:               defaultProbeSpec,
				Selector:                defaultSelector,
				RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
			},
		},
		remoteAPIClient: remoteAPI,
		localAPIClient:  localAPI,
		linksAPIClient:  linksAPI,
		log:             logging.WithFields(logging.Fields{"cluster": clusterName}),
		eventsQueue:     events,
		requeueLimit:    0,
		gatewayAlive:    true,
	}

	events.Add(&RemoteServiceExported{
		service: remoteService("svc", "ns", "1", map[string]string{
			consts.DefaultExportedServiceSelector: "true",
		}, []corev1.ServicePort{
			{
				Name:     "port",
				Protocol: "TCP",
				Port:     111,
			},
		}),
	})
	for events.Len() > 0 {
		watcher.processNextEvent(context.Background())
	}

	// Expect Service svc-remote to be created with ready endpoints because
	// the Namespace ns exists and the gateway is alive.
	_, err = localAPI.Client.CoreV1().Services("ns").Get(context.Background(), "svc-remote", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting svc-remote Service: %v", err)
	}
	endpoints, err := localAPI.Client.CoreV1().Endpoints("ns").Get(context.Background(), "svc-remote", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting svc-remote Endpoints: %v", err)
	}
	if len(endpoints.Subsets) == 0 {
		t.Fatal("expected svc-remote Endpoints subsets")
	}
	for _, ss := range endpoints.Subsets {
		if len(ss.Addresses) == 0 {
			t.Fatal("svc-remote Endpoints should have addresses")
		}
		if len(ss.NotReadyAddresses) != 0 {
			t.Fatalf("svc-remote Endpoints should not have not ready addresses: %v", ss.NotReadyAddresses)
		}
	}

	// The gateway is now down which triggers repairing Endpoints on the local
	// cluster.
	watcher.gatewayAlive = false
	events.Add(&RepairEndpoints{})
	for events.Len() > 0 {
		watcher.processNextEvent(context.Background())
	}

	// When repairing Endpoints on the local cluster, the gateway address
	// should have been moved to NotReadyAddresses meaning that Endpoints
	// for the mirrored Service svc-remote should have no ready addresses.
	endpoints, err = localAPI.Client.CoreV1().Endpoints("ns").Get(context.Background(), "svc-remote", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting svc-remote Endpoints locally: %v", err)
	}
	if len(endpoints.Subsets) == 0 {
		t.Fatal("expected svc-remote Endpoints subsets")
	}
	for _, ss := range endpoints.Subsets {
		if len(ss.NotReadyAddresses) == 0 {
			t.Fatal("svc-remote Endpoints should have not ready addresses")
		}
		if len(ss.Addresses) != 0 {
			t.Fatalf("svc-remote Endpoints should not have addresses: %v", ss.Addresses)
		}
	}

	// Issue an update for the remote Service which adds a new label
	// 'new-label'. This should exercise RemoteServiceUpdated which should
	// update svc-remote; the gateway is still not alive though so we expect
	// the Endpoints of svc-remote to still have no ready addresses.
	events.Add(&RemoteExportedServiceUpdated{
		remoteUpdate: remoteService("svc", "ns", "2", map[string]string{
			consts.DefaultExportedServiceSelector: "true",
			"new-label":                           "hi",
		}, []corev1.ServicePort{
			{
				Name:     "port",
				Protocol: "TCP",
				Port:     111,
			},
		}),
	})

	// Processing the RemoteExportedServiceUpdated involves reading from the
	// localAPI informer cache. Since this cache is updated asyncronously, we
	// pause briefly here to give a chance for updates to the localAPI to be
	// reflected in the cache.
	time.Sleep(100 * time.Millisecond)

	for events.Len() > 0 {
		watcher.processNextEvent(context.Background())
	}
	service, err := localAPI.Client.CoreV1().Services("ns").Get(context.Background(), "svc-remote", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting svc-remote Service: %v", err)
	}
	_, ok := service.Labels["new-label"]
	if !ok {
		t.Fatalf("error updating svc-remote Service: %v", err)
	}
	endpoints, err = localAPI.Client.CoreV1().Endpoints("ns").Get(context.Background(), "svc-remote", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting svc-remote Endpoints: %v", err)
	}
	if len(endpoints.Subsets) == 0 {
		t.Fatal("expected svc-remote Endpoints subsets")
	}
	for _, ss := range endpoints.Subsets {
		if len(ss.NotReadyAddresses) == 0 {
			t.Fatal("svc-remote Endpoints should have not ready addresses")
		}
		if len(ss.Addresses) != 0 {
			t.Fatalf("svc-remote Endpoints should not have addresses: %v", ss.Addresses)
		}
	}
}

func TestServiceCreatedGatewayDown(t *testing.T) {
	remoteAPI, err := k8s.NewFakeAPI(
		asYaml(gateway("gateway", "gateway-ns", "1", "192.0.0.1", "gateway", 888, "gateway-identity", defaultProbePort, defaultProbePath, defaultProbePeriod)),
		asYaml(remoteService("svc", "ns", "1", map[string]string{consts.DefaultExportedServiceSelector: "true"}, []corev1.ServicePort{})),
		asYaml(endpoints("svc", "ns", nil, "192.0.0.1", "gateway-identity", []corev1.EndpointPort{})),
	)
	if err != nil {
		t.Fatal(err)
	}
	localAPI, err := k8s.NewFakeAPIWithL5dClient(
		asYaml(namespace("ns")),
	)
	if err != nil {
		t.Fatal(err)
	}
	linksAPI := k8s.NewL5dNamespacedAPI(localAPI.L5dClient, "linkerd-multicluster", "local", k8s.Link)
	remoteAPI.Sync(nil)
	localAPI.Sync(nil)
	linksAPI.Sync(nil)

	events := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())
	watcher := RemoteClusterServiceWatcher{
		link: &v1alpha3.Link{
			Spec: v1alpha3.LinkSpec{
				TargetClusterName:       clusterName,
				TargetClusterDomain:     clusterDomain,
				GatewayIdentity:         "gateway-identity",
				GatewayAddress:          "192.0.0.1",
				GatewayPort:             "888",
				ProbeSpec:               defaultProbeSpec,
				Selector:                defaultSelector,
				RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
			},
		},
		remoteAPIClient: remoteAPI,
		localAPIClient:  localAPI,
		linksAPIClient:  linksAPI,
		log:             logging.WithFields(logging.Fields{"cluster": clusterName}),
		eventsQueue:     events,
		requeueLimit:    0,
		gatewayAlive:    false,
	}

	events.Add(&RemoteServiceExported{
		service: remoteService("svc", "ns", "1", map[string]string{
			consts.DefaultExportedServiceSelector: "true",
		}, []corev1.ServicePort{
			{
				Name:     "port",
				Protocol: "TCP",
				Port:     111,
			},
		}),
	})
	for events.Len() > 0 {
		watcher.processNextEvent(context.Background())
	}

	// Expect Service svc-remote to be created with Endpoints subsets
	// that are not ready because the gateway is down.
	_, err = localAPI.Client.CoreV1().Services("ns").Get(context.Background(), "svc-remote", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting svc-remote Service: %v", err)
	}
	endpoints, err := localAPI.Client.CoreV1().Endpoints("ns").Get(context.Background(), "svc-remote", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting svc-remote Endpoints: %v", err)
	}
	if len(endpoints.Subsets) == 0 {
		t.Fatal("expected svc-remote Endpoints subsets")
	}
	for _, ss := range endpoints.Subsets {
		if len(ss.NotReadyAddresses) == 0 {
			t.Fatal("svc-remote Endpoints should have not ready addresses")
		}
		if len(ss.Addresses) != 0 {
			t.Fatalf("svc-remote Endpoints should not have addresses: %v", ss.Addresses)
		}
	}

	// The gateway is now alive which triggers repairing Endpoints on the
	// local cluster.
	watcher.gatewayAlive = true
	events.Add(&RepairEndpoints{})
	for events.Len() > 0 {
		watcher.processNextEvent(context.Background())
	}
	endpoints, err = localAPI.Client.CoreV1().Endpoints("ns").Get(context.Background(), "svc-remote", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("error getting svc-remote Endpoints locally: %v", err)
	}
	if len(endpoints.Subsets) == 0 {
		t.Fatal("expected svc-remote Endpoints subsets")
	}
	for _, ss := range endpoints.Subsets {
		if len(ss.Addresses) == 0 {
			t.Fatal("svc-remote Endpoints should have addresses")
		}
		if len(ss.NotReadyAddresses) != 0 {
			t.Fatalf("svc-remote Service endpoints should not have not ready addresses: %v", ss.NotReadyAddresses)
		}
	}
}

func TestRemoteServiceDeletedMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "deletes locally mirrored service",
			environment: deleteMirrorService,
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}

func TestRemoteServiceUpdatedMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "updates service ports on both service and endpoints",
			environment: updateServiceWithChangedPorts,
			expectedLocalServices: []*corev1.Service{
				mirrorService("test-service-remote", "test-namespace", "currentServiceResVersion", nil,
					[]corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     111,
						},
						{
							Name:     "port3",
							Protocol: "TCP",
							Port:     333,
						},
					}),
			},

			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-remote", "test-namespace", nil, "192.0.2.127", "gateway-identity", []corev1.EndpointPort{
					{
						Name:     "port1",
						Port:     888,
						Protocol: "TCP",
					},
					{
						Name:     "port3",
						Port:     888,
						Protocol: "TCP",
					},
				}),
			},
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}

// TestEmptyRemoteSelectors asserts that empty selectors do not introduce side
// effects, such as mirroring unexported services. An empty label selector
// functions as a catch-all (i.e. matches everything), the cluster watcher must
// uphold an invariant whereby empty selectors do not export everything by
// default.
func TestEmptyRemoteSelectors(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "empty remote discovery selector does not result in exports",
			environment: createEnvWithSelector(defaultSelector, &metav1.LabelSelector{}),
			expectedEventsInQueue: []interface{}{&RemoteServiceExported{
				service: remoteService("service-one", "ns1", "111", map[string]string{
					consts.DefaultExportedServiceSelector: "true",
				}, []corev1.ServicePort{
					{
						Name:     "default1",
						Protocol: "TCP",
						Port:     555,
					},
					{
						Name:     "default2",
						Protocol: "TCP",
						Port:     666,
					},
				}),
			},
			},
		},
		{
			description: "empty default selector does not result in exports",
			environment: createEnvWithSelector(&metav1.LabelSelector{}, defaultRemoteDiscoverySelector),
			expectedEventsInQueue: []interface{}{&RemoteServiceExported{
				service: remoteService("service-two", "ns1", "111", map[string]string{
					consts.DefaultExportedServiceSelector: "remote-discovery",
				}, []corev1.ServicePort{
					{
						Name:     "remote1",
						Protocol: "TCP",
						Port:     777,
					},
					{
						Name:     "remote2",
						Protocol: "TCP",
						Port:     888,
					},
				}),
			}},
		},
		{
			description: "no selector in link does not result in exports",
			environment: createEnvWithSelector(&metav1.LabelSelector{}, &metav1.LabelSelector{}),
		},
	} {
		tc := tt
		tc.run(t)
	}
}

func TestRemoteEndpointsUpdatedMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "updates headless mirror service with new remote Endpoints hosts",
			environment: updateEndpointsWithChangedHosts,
			expectedLocalServices: []*corev1.Service{
				headlessMirrorService("service-two-remote", "eptest", "222", nil, []corev1.ServicePort{
					{
						Name:     "port1",
						Protocol: "TCP",
						Port:     555,
					},
					{
						Name:     "port2",
						Protocol: "TCP",
						Port:     666,
					},
				}),
				endpointMirrorService("pod-0", "service-two-remote", "eptest", "333", nil, []corev1.ServicePort{
					{
						Name:     "port1",
						Protocol: "TCP",
						Port:     555,
					},
					{
						Name:     "port2",
						Protocol: "TCP",
						Port:     666,
					},
				}),
				endpointMirrorService("pod-1", "service-two-remote", "eptest", "112", nil, []corev1.ServicePort{
					{
						Name:     "port1",
						Protocol: "TCP",
						Port:     555,
					},
					{
						Name:     "port2",
						Protocol: "TCP",
						Port:     666,
					},
				}),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				headlessMirrorEndpointsUpdated(
					"service-two-remote",
					"eptest",
					[]string{"pod-0", "pod-1"},
					[]string{"", ""},
					"gateway-identity",
					[]corev1.EndpointPort{
						{
							Name:     "port1",
							Port:     555,
							Protocol: "TCP",
						},
						{
							Name:     "port2",
							Port:     666,
							Protocol: "TCP",
						},
					}),
				endpointMirrorEndpoints(
					"service-two-remote",
					"eptest",
					nil,
					"pod-0",
					"192.0.2.127",
					"gateway-identity",
					[]corev1.EndpointPort{
						{
							Name:     "port1",
							Port:     888,
							Protocol: "TCP",
						},
						{
							Name:     "port2",
							Port:     888,
							Protocol: "TCP",
						},
					}),
				endpointMirrorEndpoints(
					"service-two-remote",
					"eptest",
					nil,
					"pod-1",
					"192.0.2.127",
					"gateway-identity",
					[]corev1.EndpointPort{
						{
							Name:     "port1",
							Port:     888,
							Protocol: "TCP",
						},
						{
							Name:     "port2",
							Port:     888,
							Protocol: "TCP",
						},
					}),
			},
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}

func TestClusterUnregisteredMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "unregisters cluster and cleans up all mirrored resources",
			environment: clusterUnregistered,
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}

func TestGcOrphanedServicesMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "deletes mirrored resources that are no longer present on the remote cluster",
			environment: gcTriggered,
			expectedLocalServices: []*corev1.Service{
				mirrorService("test-service-1-remote", "test-namespace", "", nil, nil),
				headlessMirrorService("test-headless-service-remote", "test-namespace", "", nil, nil),
				endpointMirrorService("pod-0", "test-headless-service-remote", "test-namespace", "", nil, nil),
			},

			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-1-remote", "test-namespace", nil, "", "", nil),
				headlessMirrorEndpoints("test-headless-service-remote", "test-namespace", nil, "", nil),
				endpointMirrorEndpoints("test-headless-service-remote", "test-namespace", nil, "pod-0", "", "", nil),
			},
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}

func onAddOrUpdateTestCases(isAdd bool) []mirroringTestCase {

	testType := "ADD"
	if !isAdd {
		testType = "UPDATE"
	}

	return []mirroringTestCase{
		{
			description: fmt.Sprintf("enqueue a RemoteServiceCreated event when this is not a gateway and we have the needed annotations (%s)", testType),
			environment: onAddOrUpdateExportedSvc(isAdd),
			expectedEventsInQueue: []interface{}{&RemoteServiceExported{
				service: remoteService("test-service", "test-namespace", "resVersion", map[string]string{
					consts.DefaultExportedServiceSelector: "true",
				}, nil),
			}},
		},
		{
			description: fmt.Sprintf("enqueue a RemoteServiceUpdated event if this is a service that we have already mirrored and its res version is different (%s)", testType),
			environment: onAddOrUpdateRemoteServiceUpdated(isAdd),
			expectedEventsInQueue: []interface{}{&RemoteExportedServiceUpdated{
				remoteUpdate: remoteService("test-service", "test-namespace", "currentResVersion", map[string]string{
					consts.DefaultExportedServiceSelector: "true",
				}, nil),
			}},
			expectedLocalServices: []*corev1.Service{
				mirrorService("test-service-remote", "test-namespace", "pastResourceVersion", nil, nil),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-remote", "test-namespace", nil, "0.0.0.0", "", nil),
			},
		},
		{
			description: fmt.Sprintf("not enqueue any events as this update does not really tell us anything new (res version is the same...) (%s)", testType),
			environment: onAddOrUpdateSameResVersion(isAdd),
			expectedLocalServices: []*corev1.Service{
				mirrorService("test-service-remote", "test-namespace", "currentResVersion", nil, nil),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-remote", "test-namespace", nil, "0.0.0.0", "", nil),
			},
		},
		{
			description: fmt.Sprintf("enqueue RemoteServiceDeleted event as this service is not mirrorable anymore (%s)", testType),
			environment: serviceNotExportedAnymore(isAdd),
			expectedEventsInQueue: []interface{}{&RemoteServiceUnexported{
				Name:      "test-service",
				Namespace: "test-namespace",
			}},

			expectedLocalServices: []*corev1.Service{
				mirrorService("test-service-remote", "test-namespace", "currentResVersion", nil, nil),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-remote", "test-namespace", nil, "0.0.0.0", "", nil),
			},
		},
	}
}

func TestOnAdd(t *testing.T) {
	for _, tt := range onAddOrUpdateTestCases(true) {
		tc := tt // pin
		tc.run(t)
	}
}

func TestOnUpdate(t *testing.T) {
	for _, tt := range onAddOrUpdateTestCases(false) {
		tc := tt // pin
		tc.run(t)
	}
}

func TestOnDelete(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "enqueues a RemoteServiceDeleted because there is gateway metadata present on the service",
			environment: onDeleteExportedService,
			expectedEventsInQueue: []interface{}{
				&RemoteServiceUnexported{
					Name:      "test-service",
					Namespace: "test-namespace",
				},
			},
		},
		{
			description:           "skips because there is no gateway metadata present on the service",
			environment:           onDeleteNonExportedService,
			expectedEventsInQueue: []interface{}{},
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}
