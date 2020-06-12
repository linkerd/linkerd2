package servicemirror

import (
	"fmt"
	"net"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	t.Run(tc.description, func(t *testing.T) {

		q := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		localAPI, err := tc.environment.runEnvironment(q)
		if err != nil {
			t.Fatal(err)
		}

		if tc.expectedLocalServices == nil {
			// ensure the are no local services
			services, err := localAPI.Client.CoreV1().Services(corev1.NamespaceAll).List(metav1.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(services.Items) > 0 {
				t.Fatalf("Was expecting no local services but instead found %v", services.Items)

			}
		} else {
			for _, expected := range tc.expectedLocalServices {
				actual, err := localAPI.Client.CoreV1().Services(expected.Namespace).Get(expected.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Could not find mirrored service with name %s", expected.Name)
				}

				if err := diffServices(expected, actual); err != nil {
					t.Fatal(err)
				}
			}
		}

		if tc.expectedLocalEndpoints == nil {
			// ensure the are no local endpoints
			endpoints, err := localAPI.Client.CoreV1().Endpoints(corev1.NamespaceAll).List(metav1.ListOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(endpoints.Items) > 0 {
				t.Fatalf("Was expecting no local endpoints but instead found %d", len(endpoints.Items))

			}
		} else {
			for _, expected := range tc.expectedLocalEndpoints {
				actual, err := localAPI.Client.CoreV1().Endpoints(expected.Namespace).Get(expected.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Could not find endpoints with name %s", expected.Name)
				}

				if err := diffEndpoints(expected, actual); err != nil {
					t.Fatal(err)
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
			if !reflect.DeepEqual(ev, evInQueue) {
				t.Fatalf("was expecting to see event %s but got %s", ev, evInQueue)
			}
		}
	})
}

func TestRemoteServiceCreatedMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "create service and endpoints when gateway cannot be resolved",
			environment: serviceCreateWithMissingGateway,
			expectedLocalServices: []*corev1.Service{
				mirroredService("service-one-remote", "ns1", "missing-gateway", "missing-namespace", "111", "", nil),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("service-one-remote", "ns1", "missing-gateway", "missing-namespace", "", "", nil),
			},
		},
		{
			description: "create service and endpoints without subsets when gateway spec is wrong",
			environment: createServiceWrongGatewaySpec,
			expectedLocalServices: []*corev1.Service{
				mirroredService("service-one-remote", "ns1", "existing-gateway", "existing-namespace", "111", "",
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
				endpoints("service-one-remote", "ns1", "existing-gateway", "existing-namespace", "", "", nil),
			},
		},
		{
			description: "create service and endpoints when gateway can be resolved",
			environment: createServiceOkeGatewaySpec,
			expectedLocalServices: []*corev1.Service{
				mirroredService(
					"service-one-remote",
					"ns1",
					"existing-gateway",
					"existing-namespace",
					"111",
					"222",
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
				endpoints("service-one-remote", "ns1", "existing-gateway", "existing-namespace", "192.0.2.127", "gateway-identity", []corev1.EndpointPort{
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

func TestRemoteServiceDeletedMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "deletes locally mirrored service",
			environment: deleteMirroredService,
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}

func TestRemoteServiceUpdatedMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "update to new gateway",
			environment: updateServiceToNewGateway,
			expectedLocalServices: []*corev1.Service{
				mirroredService(
					"test-service-remote",
					"test-namespace",
					"gateway-new",
					"gateway-ns",
					"currentServiceResVersion",
					"currentGatewayResVersion",
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
				endpoints("test-service-remote", "test-namespace", "gateway-new", "gateway-ns", "0.0.0.0", "", []corev1.EndpointPort{
					{
						Name:     "port1",
						Port:     999,
						Protocol: "TCP",
					},
					{
						Name:     "port2",
						Port:     999,
						Protocol: "TCP",
					},
				}),
			},
		},
		{
			description: "updates service ports on both service and endpoints",
			environment: updateServiceWithChangedPorts,
			expectedLocalServices: []*corev1.Service{
				mirroredService("test-service-remote", "test-namespace", "gateway", "gateway-ns", "currentServiceResVersion", "currentGatewayResVersion",
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
				endpoints("test-service-remote", "test-namespace", "gateway", "gateway-ns", "192.0.2.127", "", []corev1.EndpointPort{
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

func TestRemoteGatewayUpdatedMirroring(t *testing.T) {

	localhostIP, err := net.ResolveIPAddr("ip", "localhost")
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []mirroringTestCase{
		{
			description: "endpoints ports are updated on gateway change",
			environment: remoteGatewayUpdated,
			expectedLocalServices: []*corev1.Service{
				mirroredService("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "currentGatewayResVersion",
					[]corev1.ServicePort{
						{
							Name:     "svc-1-port",
							Protocol: "TCP",
							Port:     8081,
						},
					}),

				mirroredService("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "currentGatewayResVersion", []corev1.ServicePort{
					{
						Name:     "svc-2-port",
						Protocol: "TCP",
						Port:     8082,
					},
				}),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
					[]corev1.EndpointPort{
						{
							Name:     "svc-1-port",
							Port:     999,
							Protocol: "TCP",
						}}),
				endpoints("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
					[]corev1.EndpointPort{
						{
							Name:     "svc-2-port",
							Port:     999,
							Protocol: "TCP",
						}}),
			},
		},

		{
			description: "endpoints addresses are updated on gateway change",
			environment: gatewayAddressChanged,
			expectedLocalServices: []*corev1.Service{
				mirroredService("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "currentGatewayResVersion",
					[]corev1.ServicePort{
						{
							Name:     "svc-1-port",
							Protocol: "TCP",
							Port:     8081,
						},
					}),
				mirroredService("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "currentGatewayResVersion", []corev1.ServicePort{
					{
						Name:     "svc-2-port",
						Protocol: "TCP",
						Port:     8082,
					},
				}),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.1", "",
					[]corev1.EndpointPort{
						{
							Name:     "svc-1-port",
							Port:     888,
							Protocol: "TCP",
						}}),
				endpoints("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.1", "",
					[]corev1.EndpointPort{
						{
							Name:     "svc-2-port",
							Port:     888,
							Protocol: "TCP",
						}}),
			},
		},

		{
			description: "identity is updated on gateway change",
			environment: gatewayIdentityChanged,
			expectedLocalServices: []*corev1.Service{
				mirroredService("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "currentGatewayResVersion",
					[]corev1.ServicePort{
						{
							Name:     "svc-1-port",
							Protocol: "TCP",
							Port:     8081,
						},
					}),
				mirroredService("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "currentGatewayResVersion", []corev1.ServicePort{
					{
						Name:     "svc-2-port",
						Protocol: "TCP",
						Port:     8082,
					},
				}),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "new-identity",
					[]corev1.EndpointPort{
						{
							Name:     "svc-1-port",
							Port:     888,
							Protocol: "TCP",
						}}),
				endpoints("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "new-identity",
					[]corev1.EndpointPort{
						{
							Name:     "svc-2-port",
							Port:     888,
							Protocol: "TCP",
						}}),
			},
		},
		{
			description: "gateway uses hostname address",
			environment: remoteGatewayUpdatedWithHostnameAddress,
			expectedEventsInQueue: []interface{}{
				&RemoteGatewayUpdated{
					gatewaySpec: GatewaySpec{
						gatewayName:      "gateway",
						gatewayNamespace: "gateway-ns",
						clusterName:      "remote",
						addresses:        []corev1.EndpointAddress{{IP: localhostIP.String()}},
						incomingPort:     999,
						resourceVersion:  "currentGatewayResVersion",
						ProbeConfig: &ProbeConfig{
							path:            defaultProbePath,
							port:            defaultProbePort,
							periodInSeconds: defaultProbePeriod,
						},
					},
					affectedServices: []*v1.Service{},
				},
			},
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}
func TestRemoteGatewayDeletedMirroring(t *testing.T) {
	for _, tt := range []mirroringTestCase{
		{
			description: "removes endpoint subsets when gateway is deleted",
			environment: gatewayDeleted,
			expectedLocalServices: []*corev1.Service{
				mirroredService("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion",
					[]corev1.ServicePort{
						{
							Name:     "svc-1-port",
							Protocol: "TCP",
							Port:     8081,
						},
					}),

				mirroredService("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion", []corev1.ServicePort{
					{
						Name:     "svc-2-port",
						Protocol: "TCP",
						Port:     8082,
					},
				}),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "", nil),
				endpoints("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "", nil),
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
				mirroredService("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "", nil),
			},

			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-1-remote", "test-namespace", "", "", "", "", nil),
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
				service: remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "resVersion", nil),
				gatewayData: gatewayMetadata{
					Name:      "gateway",
					Namespace: "gateway-ns",
				},
			}},
		},
		{
			description: fmt.Sprintf("enqueue a RemoteServiceUpdated event if this is a service that we have already mirrored and its res version is different (%s)", testType),
			environment: onAddOrUpdateRemoteServiceUpdated(isAdd),
			expectedEventsInQueue: []interface{}{&RemoteServiceUpdated{
				localService:   mirroredService("test-service-remote", "test-namespace", "gateway", "gateway-ns", "pastResourceVersion", "gatewayResVersion", nil),
				localEndpoints: endpoints("test-service-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "", nil),
				remoteUpdate:   remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "currentResVersion", nil),
				gatewayData: gatewayMetadata{
					Name:      "gateway",
					Namespace: "gateway-ns",
				},
			}},
			expectedLocalServices: []*corev1.Service{
				mirroredService("test-service-remote", "test-namespace", "gateway", "gateway-ns", "pastResourceVersion", "gatewayResVersion", nil),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "", nil),
			},
		},
		{
			description: fmt.Sprintf("not enqueue any events as this update does not really tell us anything new (res version is the same...) (%s)", testType),
			environment: onAddOrUpdateSameResVersion(isAdd),
			expectedLocalServices: []*corev1.Service{
				mirroredService("test-service-remote", "test-namespace", "gateway", "gateway-ns", "currentResVersion", "gatewayResVersion", nil),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "", nil),
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
				mirroredService("test-service-remote", "test-namespace", "gateway", "gateway-ns", "currentResVersion", "gatewayResVersion", nil),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "", nil),
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
			environment: onDeleteWithGatewayMetadata,
			expectedEventsInQueue: []interface{}{
				&RemoteServiceDeleted{
					Name:      "test-service",
					Namespace: "test-namespace",
				},
			},
		},
		{
			description:           "skips because there is no gateway metadata present on the service",
			environment:           onDeleteNoGatewayMetadata,
			expectedEventsInQueue: []interface{}{},
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}
