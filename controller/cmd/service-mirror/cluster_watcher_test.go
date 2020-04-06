package servicemirror

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
)

const (
	clusterName   = "remote"
	clusterDomain = "cluster.local"
)

type testCase struct {
	testDescription        string
	events                 []interface{}
	remoteResources        []string
	localResources         []string
	expectedLocalServices  []*corev1.Service
	expectedLocalEndpoints []*corev1.Endpoints
	expectedEventsInQueue  []interface{}
}

func runTestCase(tc *testCase, t *testing.T) {
	t.Run(tc.testDescription, func(t *testing.T) {
		remoteAPI, err := k8s.NewFakeAPI(tc.remoteResources...)
		if err != nil {
			t.Fatal(err)
		}

		localAPI, err := k8s.NewFakeAPI(tc.localResources...)
		if err != nil {
			t.Fatal(err)
		}

		remoteAPI.Sync(nil)
		localAPI.Sync(nil)

		q := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

		watcher := RemoteClusterServiceWatcher{
			clusterName:     clusterName,
			clusterDomain:   clusterDomain,
			remoteAPIClient: remoteAPI,
			localAPIClient:  localAPI,
			stopper:         nil,
			log:             logging.WithFields(logging.Fields{"cluster": clusterName}),
			eventsQueue:     q,
			requeueLimit:    0,
			probeChan:       make(chan interface{}, probeChanBufferSize),
		}

		for _, ev := range tc.events {
			q.Add(ev)
		}

		for range tc.events {
			watcher.processNextEvent()
		}

		localAPI.Sync(nil)
		remoteAPI.Sync(nil)

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
		actualNumEvents := watcher.eventsQueue.Len()

		if expectedNumEvents != actualNumEvents {
			t.Fatalf("Was expecting %d events but got %d", expectedNumEvents, actualNumEvents)
		}

		for _, ev := range tc.expectedEventsInQueue {
			evInQueue, _ := watcher.eventsQueue.Get()
			if !reflect.DeepEqual(ev, evInQueue) {
				t.Fatalf("was expecting to see event %T but got %T", ev, evInQueue)
			}
		}
	})
}

func TestRemoteServiceCreated(t *testing.T) {
	for _, tt := range []testCase{
		{
			testDescription: "create service and endpoints when gateway cannot be resolved",
			events: []interface{}{
				&RemoteServiceCreated{
					service: remoteService("service-one", "ns1", "missing-gateway", "missing-namespace", "111", nil),
					gatewayData: &gatewayMetadata{
						Name:      "missing-gateway",
						Namespace: "missing-namespace",
					},
				},
			},
			expectedLocalServices: []*corev1.Service{
				mirroredService("service-one-remote", "ns1", "missing-gateway", "missing-namespace", "111", "", nil),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("service-one-remote", "ns1", "missing-gateway", "missing-namespace", "", "", nil),
			},
		},
		{
			testDescription: "create service and endpoints without subsets when gateway spec is wrong",
			events: []interface{}{
				&RemoteServiceCreated{
					service: remoteService("service-one", "ns1", "existing-gateway", "existing-namespace",
						"111", []corev1.ServicePort{
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

					gatewayData: &gatewayMetadata{
						Name:      "existing-gateway",
						Namespace: "existing-namespace",
					},
				},
			},
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
			remoteResources: []string{
				gatewayAsYaml("existing-gateway", "existing-namespace", "222", "192.0.2.127", "incoming-port-wrong", 888, "", t),
			},
		},
		{
			testDescription: "create service and endpoints when gateway can be resolved",
			events: []interface{}{
				&RemoteServiceCreated{

					service: remoteService("service-one", "ns1", "existing-gateway", "existing-namespace", "111", []corev1.ServicePort{
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
					gatewayData: &gatewayMetadata{
						Name:      "existing-gateway",
						Namespace: "existing-namespace",
					},
				},
			},
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
			remoteResources: []string{
				gatewayAsYaml("existing-gateway", "existing-namespace", "222", "192.0.2.127", "incoming-port", 888, "gateway-identity", t),
			},
		},
	} {
		tc := tt // pin
		runTestCase(&tc, t)
	}
}

func TestRemoteServiceDeleted(t *testing.T) {
	for _, tt := range []testCase{
		{
			testDescription: "deletes locally mirrored service",
			events: []interface{}{
				&RemoteServiceDeleted{
					Name:      "test-service-remote-to-delete",
					Namespace: "test-namespace-to-delete",
					GatewayData: &gatewayMetadata{
						Name:      "gateway",
						Namespace: "gateway-ns",
					},
				},
			},

			localResources: []string{
				mirroredServiceAsYaml("test-service-remote-to-delete-remote", "test-namespace-to-delete", "", "", "", "", nil, t),
				endpointsAsYaml("test-service-remote-to-delete-remote", "test-namespace-to-delete", "", "", "", "gateway-identity", nil, t),
			},
		},
	} {
		tc := tt // pin
		runTestCase(&tc, t)
	}
}

func TestRemoteServiceUpdated(t *testing.T) {
	for _, tt := range []testCase{
		{
			testDescription: "update to new gateway",
			events: []interface{}{
				&RemoteServiceUpdated{
					remoteUpdate: remoteService("test-service", "test-namespace", "gateway-new", "gateway-ns", "currentServiceResVersion", []corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     111,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     222,
						},
					}),
					localService: mirroredService("test-service-remote", "test-namespace", "gateway", "gateway-ns", "pastServiceResVersion", "pastGatewayResVersion", []corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     111,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     222,
						},
					}),
					localEndpoints: endpoints("test-service-remote", "test-namespace", "gateway", "gateway-ns", "192.0.2.127", "", []corev1.EndpointPort{
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
					gatewayData: &gatewayMetadata{
						Name:      "gateway-new",
						Namespace: "gateway-ns",
					},
				},
			},
			remoteResources: []string{
				gatewayAsYaml("gateway-new", "gateway-ns", "currentGatewayResVersion", "0.0.0.0", "incoming-port", 999, "", t),
			},
			localResources: []string{
				mirroredServiceAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "past", "pastGatewayResVersion", []corev1.ServicePort{
					{
						Name:     "port1",
						Protocol: "TCP",
						Port:     111,
					},
					{
						Name:     "port2",
						Protocol: "TCP",
						Port:     222,
					},
				}, t),
				endpointsAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "192.0.2.127", "", []corev1.EndpointPort{
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
				}, t),
			},
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
			testDescription: "updates service ports on both service and endpoints",
			events: []interface{}{
				&RemoteServiceUpdated{
					remoteUpdate: remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "currentServiceResVersion", []corev1.ServicePort{
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
					localService: mirroredService("test-service-remote", "test-namespace", "gateway", "gateway-ns", "pastServiceResVersion", "pastGatewayResVersion", []corev1.ServicePort{
						{
							Name:     "port1",
							Protocol: "TCP",
							Port:     111,
						},
						{
							Name:     "port2",
							Protocol: "TCP",
							Port:     222,
						},
					}),
					localEndpoints: endpoints("test-service-remote", "test-namespace", "gateway", "gateway-ns", "192.0.2.127", "", []corev1.EndpointPort{
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
					gatewayData: &gatewayMetadata{
						Name:      "gateway",
						Namespace: "gateway-ns",
					},
				},
			},
			remoteResources: []string{
				gatewayAsYaml("gateway", "gateway-ns", "currentGatewayResVersion", "192.0.2.127", "incoming-port", 888, "", t),
			},
			localResources: []string{
				mirroredServiceAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "past", "pastGatewayResVersion", []corev1.ServicePort{
					{
						Name:     "port1",
						Protocol: "TCP",
						Port:     111,
					},
					{
						Name:     "port2",
						Protocol: "TCP",
						Port:     222,
					},
					{
						Name:     "port3",
						Protocol: "TCP",
						Port:     333,
					},
				}, t),
				endpointsAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "192.0.2.127", "", []corev1.EndpointPort{
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
					{
						Name:     "port3",
						Port:     888,
						Protocol: "TCP",
					},
				}, t),
			},
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
		runTestCase(&tc, t)
	}
}

func TestRemoteGatewayUpdated(t *testing.T) {
	for _, tt := range []testCase{
		{
			testDescription: "endpoints ports are updated on gateway change",
			events: []interface{}{
				&RemoteGatewayUpdated{
					newPort:              999,
					newEndpointAddresses: []corev1.EndpointAddress{{IP: "0.0.0.0"}},
					gatewayData: &gatewayMetadata{
						Name:      "gateway",
						Namespace: "gateway-ns",
					},
					newResourceVersion: "currentGatewayResVersion",
					affectedServices: []*corev1.Service{
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
				},
			},

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
			localResources: []string{
				mirroredServiceAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion",
					[]corev1.ServicePort{
						{
							Name:     "svc-1-port",
							Protocol: "TCP",
							Port:     8081,
						},
					}, t),
				endpointsAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
					[]corev1.EndpointPort{
						{
							Name:     "svc-1-port",
							Port:     888,
							Protocol: "TCP",
						}}, t),
				mirroredServiceAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion", []corev1.ServicePort{
					{
						Name:     "svc-2-port",
						Protocol: "TCP",
						Port:     8082,
					},
				}, t),
				endpointsAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
					[]corev1.EndpointPort{
						{
							Name:     "svc-2-port",
							Port:     888,
							Protocol: "TCP",
						}}, t),
			},
		},

		{
			testDescription: "endpoints addresses are updated on gateway change",
			events: []interface{}{
				&RemoteGatewayUpdated{
					newPort:              888,
					newEndpointAddresses: []corev1.EndpointAddress{{IP: "0.0.0.1"}},
					gatewayData: &gatewayMetadata{
						Name:      "gateway",
						Namespace: "gateway-ns",
					},
					newResourceVersion: "currentGatewayResVersion",
					affectedServices: []*corev1.Service{
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
				},
			},
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

			localResources: []string{
				mirroredServiceAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion",
					[]corev1.ServicePort{
						{
							Name:     "svc-1-port",
							Protocol: "TCP",
							Port:     8081,
						},
					}, t),
				endpointsAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
					[]corev1.EndpointPort{
						{
							Name:     "svc-1-port",
							Port:     888,
							Protocol: "TCP",
						}}, t),
				mirroredServiceAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion", []corev1.ServicePort{
					{
						Name:     "svc-2-port",
						Protocol: "TCP",
						Port:     8082,
					},
				}, t),
				endpointsAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
					[]corev1.EndpointPort{
						{
							Name:     "svc-2-port",
							Port:     888,
							Protocol: "TCP",
						}}, t),
			},
		},

		{
			testDescription: "identity is updated on gateway change",
			events: []interface{}{
				&RemoteGatewayUpdated{
					identity:             "new-identity",
					newPort:              888,
					newEndpointAddresses: []corev1.EndpointAddress{{IP: "0.0.0.0"}},
					gatewayData: &gatewayMetadata{
						Name:      "gateway",
						Namespace: "gateway-ns",
					},
					newResourceVersion: "currentGatewayResVersion",
					affectedServices: []*corev1.Service{
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
				},
			},
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

			localResources: []string{
				mirroredServiceAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion",
					[]corev1.ServicePort{
						{
							Name:     "svc-1-port",
							Protocol: "TCP",
							Port:     8081,
						},
					}, t),
				endpointsAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
					[]corev1.EndpointPort{
						{
							Name:     "svc-1-port",
							Port:     888,
							Protocol: "TCP",
						}}, t),
				mirroredServiceAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion", []corev1.ServicePort{
					{
						Name:     "svc-2-port",
						Protocol: "TCP",
						Port:     8082,
					},
				}, t),
				endpointsAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
					[]corev1.EndpointPort{
						{
							Name:     "svc-2-port",
							Port:     888,
							Protocol: "TCP",
						}}, t),
			},
		},
	} {
		tc := tt // pin
		runTestCase(&tc, t)
	}
}
func TestRemoteGatewayDeleted(t *testing.T) {
	for _, tt := range []testCase{
		{
			testDescription: "removes endpoint subsets when gateway is deleted",
			events: []interface{}{
				&RemoteGatewayDeleted{
					gatewayData: &gatewayMetadata{
						Name:      "gateway",
						Namespace: "gateway-ns",
					},
				},
			},
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
			localResources: []string{
				mirroredServiceAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion",
					[]corev1.ServicePort{
						{
							Name:     "svc-1-port",
							Protocol: "TCP",
							Port:     8081,
						},
					}, t),
				endpointsAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
					[]corev1.EndpointPort{
						{
							Name:     "svc-1-port",
							Port:     888,
							Protocol: "TCP",
						}}, t),
				mirroredServiceAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion", []corev1.ServicePort{
					{
						Name:     "svc-2-port",
						Protocol: "TCP",
						Port:     8082,
					},
				}, t),
				endpointsAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
					[]corev1.EndpointPort{
						{
							Name:     "svc-2-port",
							Port:     888,
							Protocol: "TCP",
						}}, t),
			},
		},
	} {
		tc := tt // pin
		runTestCase(&tc, t)
	}
}

func TestClusterUnregistered(t *testing.T) {
	for _, tt := range []testCase{
		{
			testDescription: "unregisters cluster and cleans up all mirrored resources",
			events: []interface{}{
				&ClusterUnregistered{},
			},
			localResources: []string{
				mirroredServiceAsYaml("test-service-1-remote", "test-namespace", "", "", "", "", nil, t),
				endpointsAsYaml("test-service-1-remote", "test-namespace", "", "", "", "", nil, t),
				mirroredServiceAsYaml("test-service-2-remote", "test-namespace", "", "", "", "", nil, t),
				endpointsAsYaml("test-service-2-remote", "test-namespace", "", "", "", "", nil, t),
			},
		},
	} {
		tc := tt // pin
		runTestCase(&tc, t)
	}
}

func TestGcOrphanedServices(t *testing.T) {
	for _, tt := range []testCase{
		{
			testDescription: "deletes mirrored resources that are no longer present on the remote cluster",
			events: []interface{}{
				&OprhanedServicesGcTriggered{},
			},
			localResources: []string{
				mirroredServiceAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "", nil, t),
				endpointsAsYaml("test-service-1-remote", "test-namespace", "", "", "", "", nil, t),
				mirroredServiceAsYaml("test-service-2-remote", "test-namespace", "", "", "", "", nil, t),
				endpointsAsYaml("test-service-2-remote", "test-namespace", "", "", "", "", nil, t),
			},
			remoteResources: []string{
				remoteServiceAsYaml("test-service-1", "test-namespace", "gateway", "gateway-ns", "", nil, t),
			},

			expectedLocalServices: []*corev1.Service{
				mirroredService("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "", nil),
			},

			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-1-remote", "test-namespace", "", "", "", "", nil),
			},
		},
	} {
		tc := tt // pin
		runTestCase(&tc, t)
	}
}

// the following tests ensure that onAdd, onUpdate and onDelete result in
// queueing more specific events to be processed

func onAddOrUpdateEvent(isAdd bool, svc *corev1.Service) interface{} {
	if isAdd {
		return &OnAddCalled{svc: svc}
	}
	return &OnUpdateCalled{svc: svc}
}

func onAddOrUpdateTestCases(t *testing.T, isAdd bool) []testCase {

	testType := "ADD"
	if !isAdd {
		testType = "UPDATE"
	}

	return []testCase{
		{
			testDescription: fmt.Sprintf("enqueue a RemoteServiceCreated event when this is not a gateway and we have the needed annotations (%s)", testType),
			events: []interface{}{
				onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "resVersion", nil)),
			},
			expectedEventsInQueue: []interface{}{&RemoteServiceCreated{
				service: remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "resVersion", nil),
				gatewayData: &gatewayMetadata{
					Name:      "gateway",
					Namespace: "gateway-ns",
				},
			}},
		},
		{
			testDescription: fmt.Sprintf("enqueue a ConsiderGatewayUpdateDispatch event if this is clearly not a mirrorable service (%s)", testType),
			events: []interface{}{
				onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "", "", "resVersion", nil)),
			},
			expectedEventsInQueue: []interface{}{&ConsiderGatewayUpdateDispatch{
				maybeGateway: remoteService("test-service", "test-namespace", "", "", "resVersion", nil),
			}},
		},
		{
			testDescription: fmt.Sprintf("enqueue a RemoteServiceUpdated event if this is a service that we have already mirrored and its res version is different (%s)", testType),
			events: []interface{}{
				onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "currentResVersion", nil)),
			},
			localResources: []string{
				mirroredServiceAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "pastResourceVersion", "gatewayResVersion", nil, t),
				endpointsAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "", nil, t),
			},
			expectedEventsInQueue: []interface{}{&RemoteServiceUpdated{
				localService:   mirroredService("test-service-remote", "test-namespace", "gateway", "gateway-ns", "pastResourceVersion", "gatewayResVersion", nil),
				localEndpoints: endpoints("test-service-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "", nil),
				remoteUpdate:   remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "currentResVersion", nil),
				gatewayData: &gatewayMetadata{
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
			testDescription: fmt.Sprintf("not enqueue any events as this update does not really tell us anything new (res version is the same...) (%s)", testType),
			events: []interface{}{
				onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "currentResVersion", nil)),
			},
			localResources: []string{
				mirroredServiceAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "currentResVersion", "gatewayResVersion", nil, t),
				endpointsAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "", nil, t),
			},
			expectedLocalServices: []*corev1.Service{
				mirroredService("test-service-remote", "test-namespace", "gateway", "gateway-ns", "currentResVersion", "gatewayResVersion", nil),
			},
			expectedLocalEndpoints: []*corev1.Endpoints{
				endpoints("test-service-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "", nil),
			},
		},
		{
			testDescription: fmt.Sprintf("enqueue RemoteServiceDeleted event as this service is not mirrorable anymore (%s)", testType),
			events: []interface{}{
				onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "", "gateway-ns", "currentResVersion", nil)),
			},
			localResources: []string{
				mirroredServiceAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "currentResVersion", "gatewayResVersion", nil, t),
				endpointsAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "", nil, t),
			},
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
	for _, tt := range onAddOrUpdateTestCases(t, true) {
		tc := tt // pin
		runTestCase(&tc, t)
	}
}

func TestOnUpdate(t *testing.T) {
	for _, tt := range onAddOrUpdateTestCases(t, false) {
		tc := tt // pin
		runTestCase(&tc, t)
	}
}

func TestOnDelete(t *testing.T) {
	for _, tt := range []testCase{
		{
			testDescription: "enqueues a RemoteServiceDeleted because there is gateway metadata present on the service",
			events: []interface{}{
				&OnDeleteCalled{
					svc: remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "currentResVersion", nil),
				},
			},

			expectedEventsInQueue: []interface{}{
				&RemoteServiceDeleted{
					Name:      "test-service",
					Namespace: "test-namespace",
					GatewayData: &gatewayMetadata{
						Name:      "gateway",
						Namespace: "gateway-ns",
					},
				},
			},
		},
		{
			testDescription: "enqueues a RemoteGatewayDeleted because there is no gateway metadata present on the service",
			events: []interface{}{
				&OnDeleteCalled{
					svc: remoteService("gateway", "test-namespace", "", "", "currentResVersion", nil),
				},
			},
			expectedEventsInQueue: []interface{}{
				&RemoteGatewayDeleted{
					gatewayData: &gatewayMetadata{
						Name:      "gateway",
						Namespace: "test-namespace",
					},
				},
			},
		},
	} {
		tc := tt // pin
		runTestCase(&tc, t)
	}
}
