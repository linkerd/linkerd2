package servicemirror

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/multicluster"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

type mirroringTestCase struct {
	description                 string
	environment                 *testEnvironment
	expectedLocalServices       []*corev1.Service
	expectedLocalEndpoints      []*corev1.Endpoints
	expectedLocalEndpointSlices []*discoveryv1beta1.EndpointSlice
	expectedEventsInQueue       []interface{}
}

func (tc *mirroringTestCase) run(t *testing.T) {
	t.Run(tc.description, func(t *testing.T) {

		q := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		localAPI, err := tc.environment.runEnvironment(q)
		if err != nil {
			t.Fatal(err)
		}

		serviceList, err := localAPI.Client.CoreV1().Services("").List(context.Background(), metav1.ListOptions{})
		if err != nil {
			t.Fatalf("Could not list actual services")
		}
		actualServices := map[string]v1.Service{}
		for _, svc := range serviceList.Items {
			id := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)
			actualServices[id] = svc
		}

		for _, expected := range tc.expectedLocalServices {
			expectedId := fmt.Sprintf("%s/%s", expected.Namespace, expected.Name)
			actual, found := actualServices[expectedId]
			if !found {
				t.Fatalf("Could not find mirrored service with name %s, found services %v", expected.Name, actualServices)
			}

			if err := diffServices(expected, &actual); err != nil {
				t.Fatal(err)
			}

			delete(actualServices, expectedId)
		}

		var extraServices []string
		for id := range actualServices {
			extraServices = append(extraServices, id)
		}
		if len(extraServices) > 0 {
			t.Fatalf("Found extra services not in expected list: %v", extraServices)
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
					t.Fatal(err)
				}
			}
		}

		endpointSliceList, err := localAPI.Client.DiscoveryV1beta1().EndpointSlices("").List(context.Background(), metav1.ListOptions{})
		if err != nil {
			t.Fatalf("Could not list actual services")
		}
		actualEndpointSlices := map[string]discoveryv1beta1.EndpointSlice{}
		for _, es := range endpointSliceList.Items {
			id := fmt.Sprintf("%s/%s", es.Namespace, es.Name)
			if id == "default/kubernetes" {
				continue
			}
			actualEndpointSlices[id] = es
		}

		for _, expected := range tc.expectedLocalEndpointSlices {
			expectedId := fmt.Sprintf("%s/%s", expected.Namespace, expected.Name)
			actual, found := actualEndpointSlices[expectedId]
			if !found {
				t.Fatalf("Could not find endpoint slice with name %s, found endpoint slices %v", expected.Name, actualEndpointSlices)
			}

			if err := diffEndpointSlice(expected, &actual); err != nil {
				t.Fatal(err)
			}
			delete(actualEndpointSlices, expectedId)
		}

		var extraSlices []string
		for id := range actualEndpointSlices {
			extraSlices = append(extraSlices, id)
		}
		if len(extraSlices) > 0 {
			t.Fatalf("Found extra endpoint slices not in expected list: %v", extraSlices)
		}

		expectedNumEvents := len(tc.expectedEventsInQueue)
		actualNumEvents := q.Len()

		if expectedNumEvents != actualNumEvents {
			t.Fatalf("Was expecting %d events but got %d", expectedNumEvents, actualNumEvents)
		}

		for _, expectedEv := range tc.expectedEventsInQueue {
			evInQueue, _ := q.Get()
			if !reflect.DeepEqual(expectedEv, evInQueue) {
				expected, _ := json.MarshalIndent(expectedEv, "", "  ")
				actual, _ := json.MarshalIndent(evInQueue, "", "  ")
				t.Fatalf("was expecting to see event %v but got %v", string(expected), string(actual))
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
				endpoints("service-one-remote", "ns1", "192.0.2.127", "gateway-identity", []corev1.EndpointPort{
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
					"ns3",
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
				endpointMirrorService(
					"pod-0",
					"service-one-remote",
					"ns3",
					"112",
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
				headlessMirrorEndpoints("service-one-remote", "ns3", "pod-0", "", "gateway-identity", []corev1.EndpointPort{
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
					"ns3",
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
			description: "create global service and endpoints when gateway can be resolved",
			environment: createExportedGlobalService,
			expectedLocalServices: append(globalMirrorServicePair(
				"service-one-remote",
				"ns4",
				"111",
				"service-one-global",
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
					"ns4",
					"112",
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
			),
			expectedLocalEndpoints: []*corev1.Endpoints{
				headlessMirrorEndpoints("service-one-remote", "ns4", "pod-0", "", "gateway-identity", []corev1.EndpointPort{
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
					"ns4",
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
			expectedLocalEndpointSlices: []*discoveryv1beta1.EndpointSlice{
				globalMirrorEndpointSlice("service-one-global", "service-one", "ns4", "pod-0-remote", "", "gateway-identity",
					[]discoveryv1beta1.EndpointPort{
						// TODO: Validate this is needed for a headless service?
					}),
			},
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}

func TestLocalNamespaceCreatedAfterServiceExport(t *testing.T) {
	remoteAPI, err := k8s.NewFakeAPI(
		gatewayAsYaml("existing-gateway", "existing-namespace", "222", "192.0.2.127", "mc-gateway", 888, "gateway-identity", defaultProbePort, defaultProbePath, defaultProbePeriod),
		remoteServiceAsYaml("service-one", "ns1", "111", []corev1.ServicePort{}),
		endpointsAsYaml("service-one", "ns1", "192.0.2.127", "gateway-identity", []corev1.EndpointPort{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	localAPI, err := k8s.NewFakeAPI()
	if err != nil {
		t.Fatal(err)
	}
	remoteAPI.Sync(nil)
	localAPI.Sync(nil)

	q := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	eventRecorder := record.NewFakeRecorder(100)

	watcher := RemoteClusterServiceWatcher{
		link: &multicluster.Link{
			TargetClusterName:   clusterName,
			TargetClusterDomain: clusterDomain,
			GatewayIdentity:     "gateway-identity",
			GatewayAddress:      "192.0.2.127",
			GatewayPort:         888,
			ProbeSpec:           defaultProbeSpec,
			Selector:            *defaultSelector,
		},
		remoteAPIClient:            remoteAPI,
		localAPIClient:             localAPI,
		stopper:                    nil,
		recorder:                   eventRecorder,
		log:                        logging.WithFields(logging.Fields{"cluster": clusterName}),
		eventsQueue:                q,
		requeueLimit:               0,
		headlessServicesEnabled:    true,
		endpointMirrorServiceCache: NewEndpointMirrorServiceCache(),
	}

	q.Add(&RemoteServiceCreated{
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
	if skippedEvent != fmt.Sprintf("%s %s %s", v1.EventTypeNormal, eventTypeSkipped, "Skipped mirroring service: namespace does not exist") {
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

func TestRemoteServiceDeletedMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "deletes locally mirrored service",
			environment: deleteMirrorService,
		},
		{
			description: "deletes locally mirrored service and global mirror",
			environment: deleteHeadlessMirrorService,
		},
		{
			description: "deletes locally mirrored service and global mirror",
			environment: deleteGlobalMirrorService,
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
				mirrorService("test-service-remote", "test-namespace", "currentServiceResVersion",
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
				endpoints("test-service-remote", "test-namespace", "192.0.2.127", "gateway-identity", []corev1.EndpointPort{
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

func TestRemoteEndpointsUpdatedMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "updates headless mirror service with new remote Endpoints hosts",
			environment: updateEndpointsWithChangedHosts,
			expectedLocalServices: []*corev1.Service{
				headlessMirrorService("service-two-remote", "eptest", "222", []corev1.ServicePort{
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
				endpointMirrorService("pod-0", "service-two-remote", "eptest", "333", []corev1.ServicePort{
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
				endpointMirrorService("pod-1", "service-two-remote", "eptest", "112", []corev1.ServicePort{
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

/// Test behavior on the remote cluster adding endpoint for pod-1 on an exported global service
func TestRemoteEndpointSlicesUpdatedMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "updates global mirror service with new remote endpoint slices",
			environment: updateEndpointSlicesWithAddedHosts,
			expectedLocalServices: append(
				globalMirrorServicePair("service-two-remote", "estest", "222", "service-two-global", []corev1.ServicePort{
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
				endpointMirrorService("pod-0", "service-two-remote", "estest", "333", []corev1.ServicePort{
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
				endpointMirrorService("pod-1", "service-two-remote", "estest", "112", []corev1.ServicePort{
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
			),
			expectedLocalEndpoints: []*corev1.Endpoints{
				headlessMirrorEndpointsUpdated(
					"service-two-remote",
					"estest",
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
					"estest",
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
					"estest",
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
			expectedLocalEndpointSlices: []*discoveryv1beta1.EndpointSlice{
				globalMirrorEndpointSliceUpdated("service-two-global", "service-two", "estest",
					[]string{"pod-0-remote", "pod-1-remote"},
					[]string{"", ""},
					"gateway-identity",
					[]discoveryv1beta1.EndpointPort{
						// TODO: Validate this is needed for a headless service?
					}),
			},
		},
		{
			description: "updates global mirror service with removed remote endpoint slices",
			environment: updateEndpointSlicesWithRemovedHosts,
			expectedLocalServices: append(
				globalMirrorServicePair("service-two-remote", "estest", "222", "service-two-global", []corev1.ServicePort{
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
				endpointMirrorService("pod-0", "service-two-remote", "estest", "333", []corev1.ServicePort{
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
			),
			expectedLocalEndpoints: []*corev1.Endpoints{
				headlessMirrorEndpoints(
					"service-two-remote",
					"estest",
					"pod-0",
					"",
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
					"estest",
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
			},
			expectedLocalEndpointSlices: []*discoveryv1beta1.EndpointSlice{
				globalMirrorEndpointSlice("service-two-global", "service-two", "estest",
					"pod-0-remote",
					"",
					"gateway-identity",
					[]discoveryv1beta1.EndpointPort{
						// TODO: Validate this is needed for a headless service?
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
				mirrorService("test-service-1-remote", "test-namespace", "", nil),
			},

			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-1-remote", "test-namespace", "", "", nil),
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
			expectedEventsInQueue: []interface{}{&RemoteServiceCreated{
				service: remoteService("test-service", "test-namespace", "resVersion", map[string]string{
					consts.DefaultExportedServiceSelector: "true",
				}, nil),
			}},
		},
		{
			description: fmt.Sprintf("enqueue a RemoteServiceUpdated event if this is a service that we have already mirrored and its res version is different (%s)", testType),
			environment: onAddOrUpdateRemoteServiceUpdated(isAdd),
			expectedEventsInQueue: []interface{}{&RemoteServiceUpdated{
				localService:   mirrorService("test-service-remote", "test-namespace", "pastResourceVersion", nil),
				localEndpoints: endpoints("test-service-remote", "test-namespace", "0.0.0.0", "", nil),
				remoteUpdate: remoteService("test-service", "test-namespace", "currentResVersion", map[string]string{
					consts.DefaultExportedServiceSelector: "true",
				}, nil),
			}},
			expectedLocalServices: []*corev1.Service{
				mirrorService("test-service-remote", "test-namespace", "pastResourceVersion", nil),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-remote", "test-namespace", "0.0.0.0", "", nil),
			},
		},
		{
			description: fmt.Sprintf("not enqueue any events as this update does not really tell us anything new (res version is the same...) (%s)", testType),
			environment: onAddOrUpdateSameResVersion(isAdd),
			expectedLocalServices: []*corev1.Service{
				mirrorService("test-service-remote", "test-namespace", "currentResVersion", nil),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-remote", "test-namespace", "0.0.0.0", "", nil),
			},
		},
		{
			description: fmt.Sprintf("enqueue RemoteServiceDeleted event as this service is not mirrorable anymore (%s)", testType),
			environment: serviceNotExportedAnymore(isAdd),
			expectedEventsInQueue: []interface{}{&RemoteServiceDeleted{
				Name:      "test-service",
				Namespace: "test-namespace",
			}},

			expectedLocalServices: []*corev1.Service{
				mirrorService("test-service-remote", "test-namespace", "currentResVersion", nil),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-remote", "test-namespace", "0.0.0.0", "", nil),
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
				&RemoteServiceDeleted{
					Name:      "test-service",
					Namespace: "test-namespace",
				},
			},
		},
		{
			description: "enqueues a RemoteServiceDeleted because there is gateway metadata present on the service",
			environment: onDeleteExportedGlobalService,
			expectedEventsInQueue: []interface{}{
				&RemoteServiceDeleted{
					Name:       "test-service",
					Namespace:  "test-namespace",
					GlobalName: StringRef("test-service-global"),
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
