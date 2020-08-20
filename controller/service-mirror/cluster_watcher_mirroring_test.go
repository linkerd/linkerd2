package servicemirror

import (
	"fmt"
	"reflect"
	"testing"

	consts "github.com/linkerd/linkerd2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
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
			// In a real Kubernetes cluster, deleting the service is sufficient
			// to delete the endpoints.
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
	} {
		tc := tt // pin
		tc.run(t)
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
			description:           "skips because there is no gateway metadata present on the service",
			environment:           onDeleteNonExportedService,
			expectedEventsInQueue: []interface{}{},
		},
	} {
		tc := tt // pin
		tc.run(t)
	}
}
