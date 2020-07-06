package servicemirror

import (
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
)

const (
	clusterName        = "remote"
	clusterDomain      = "cluster.local"
	defaultProbePath   = "/probe"
	defaultProbePort   = 12345
	defaultProbePeriod = 60
)

type testEnvironment struct {
	events          []interface{}
	remoteResources []string
	localResources  []string
}

func (te *testEnvironment) runEnvironment(watcherQueue workqueue.RateLimitingInterface) (*k8s.API, error) {
	remoteAPI, err := k8s.NewFakeAPI(te.remoteResources...)
	if err != nil {
		return nil, err
	}

	localAPI, err := k8s.NewFakeAPI(te.localResources...)
	if err != nil {
		return nil, err
	}

	remoteAPI.Sync(nil)
	localAPI.Sync(nil)

	watcher := RemoteClusterServiceWatcher{
		clusterName:     clusterName,
		clusterDomain:   clusterDomain,
		remoteAPIClient: remoteAPI,
		localAPIClient:  localAPI,
		stopper:         nil,
		log:             logging.WithFields(logging.Fields{"cluster": clusterName}),
		eventsQueue:     watcherQueue,
		requeueLimit:    0,
	}

	for _, ev := range te.events {
		watcherQueue.Add(ev)
	}

	for range te.events {
		watcher.processNextEvent()
	}

	localAPI.Sync(nil)
	remoteAPI.Sync(nil)

	return localAPI, nil
}

var serviceCreateWithMissingGateway = &testEnvironment{
	events: []interface{}{
		&RemoteServiceCreated{
			service: remoteService("service-one", "ns1", "missing-gateway", "missing-namespace", "111", nil),
			gatewayData: gatewayMetadata{
				Name:      "missing-gateway",
				Namespace: "missing-namespace",
			},
		},
	},
}

var createServiceWrongGatewaySpec = &testEnvironment{
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

			gatewayData: gatewayMetadata{
				Name:      "existing-gateway",
				Namespace: "existing-namespace",
			},
		},
	},
	remoteResources: []string{
		gatewayAsYaml("existing-gateway", "existing-namespace", "222", "192.0.2.127", "", "mc-wrong", 888, "", 111, "/path", 666),
	},
}

var createServiceOkeGatewaySpec = &testEnvironment{
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
			gatewayData: gatewayMetadata{
				Name:      "existing-gateway",
				Namespace: "existing-namespace",
			},
		},
	},
	remoteResources: []string{
		gatewayAsYaml("existing-gateway", "existing-namespace", "222", "192.0.2.127", "", "mc-gateway", 888, "gateway-identity", defaultProbePort, defaultProbePath, defaultProbePeriod),
	},
}

var deleteMirroredService = &testEnvironment{
	events: []interface{}{
		&RemoteServiceDeleted{
			Name:      "test-service-remote-to-delete",
			Namespace: "test-namespace-to-delete",
		},
	},
	localResources: []string{
		mirroredServiceAsYaml("test-service-remote-to-delete-remote", "test-namespace-to-delete", "", "", "", "", nil),
		endpointsAsYaml("test-service-remote-to-delete-remote", "test-namespace-to-delete", "", "", "", "gateway-identity", nil),
	},
}

var updateServiceToNewGateway = &testEnvironment{
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
			gatewayData: gatewayMetadata{
				Name:      "gateway-new",
				Namespace: "gateway-ns",
			},
		},
	},
	remoteResources: []string{
		gatewayAsYaml("gateway-new", "gateway-ns", "currentGatewayResVersion", "0.0.0.0", "", "mc-gateway", 999, "", defaultProbePort, defaultProbePath, defaultProbePeriod),
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
		}),
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
		}),
	},
}

var updateServiceWithChangedPorts = &testEnvironment{
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
			gatewayData: gatewayMetadata{
				Name:      "gateway",
				Namespace: "gateway-ns",
			},
		},
	},
	remoteResources: []string{
		gatewayAsYaml("gateway", "gateway-ns", "currentGatewayResVersion", "192.0.2.127", "", "mc-gateway", 888, "", defaultProbePort, defaultProbePath, defaultProbePeriod),
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
		}),
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
		}),
	},
}

var remoteGatewayUpdated = &testEnvironment{
	events: []interface{}{
		&RemoteGatewayUpdated{
			gatewaySpec: GatewaySpec{
				gatewayName:      "gateway",
				gatewayNamespace: "gateway-ns",
				clusterName:      "remote",
				addresses:        []corev1.EndpointAddress{{IP: "0.0.0.0"}},
				incomingPort:     999,
				resourceVersion:  "currentGatewayResVersion",
				ProbeConfig: &ProbeConfig{
					path:            defaultProbePath,
					port:            defaultProbePort,
					periodInSeconds: defaultProbePeriod,
				},
			},
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
	localResources: []string{
		mirroredServiceAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion",
			[]corev1.ServicePort{
				{
					Name:     "svc-1-port",
					Protocol: "TCP",
					Port:     8081,
				},
			}),
		endpointsAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
			[]corev1.EndpointPort{
				{
					Name:     "svc-1-port",
					Port:     888,
					Protocol: "TCP",
				}}),
		mirroredServiceAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion", []corev1.ServicePort{
			{
				Name:     "svc-2-port",
				Protocol: "TCP",
				Port:     8082,
			},
		}),
		endpointsAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
			[]corev1.EndpointPort{
				{
					Name:     "svc-2-port",
					Port:     888,
					Protocol: "TCP",
				}}),
	},
}

var remoteGatewayUpdatedWithHostnameAddress = &testEnvironment{
	events: []interface{}{
		&RepairEndpoints{},
	},
	remoteResources: []string{
		gatewayAsYaml("gateway", "gateway-ns", "currentGatewayResVersion", "", "localhost", "mc-gateway", 999, "", defaultProbePort, defaultProbePath, defaultProbePeriod),
	},
}

var gatewayAddressChanged = &testEnvironment{
	events: []interface{}{
		&RemoteGatewayUpdated{
			gatewaySpec: GatewaySpec{
				gatewayName:      "gateway",
				gatewayNamespace: "gateway-ns",
				clusterName:      "some-cluster",
				addresses:        []corev1.EndpointAddress{{IP: "0.0.0.1"}},
				incomingPort:     888,
				resourceVersion:  "currentGatewayResVersion",
				ProbeConfig: &ProbeConfig{
					path:            "/p",
					port:            1,
					periodInSeconds: 222,
				},
			},
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
	localResources: []string{
		mirroredServiceAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion",
			[]corev1.ServicePort{
				{
					Name:     "svc-1-port",
					Protocol: "TCP",
					Port:     8081,
				},
			}),
		endpointsAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
			[]corev1.EndpointPort{
				{
					Name:     "svc-1-port",
					Port:     888,
					Protocol: "TCP",
				}}),
		mirroredServiceAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion", []corev1.ServicePort{
			{
				Name:     "svc-2-port",
				Protocol: "TCP",
				Port:     8082,
			},
		}),
		endpointsAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
			[]corev1.EndpointPort{
				{
					Name:     "svc-2-port",
					Port:     888,
					Protocol: "TCP",
				}}),
	},
}

var gatewayIdentityChanged = &testEnvironment{
	events: []interface{}{
		&RemoteGatewayUpdated{
			gatewaySpec: GatewaySpec{
				gatewayName:      "gateway",
				gatewayNamespace: "gateway-ns",
				clusterName:      clusterName,
				addresses:        []corev1.EndpointAddress{{IP: "0.0.0.0"}},
				incomingPort:     888,
				resourceVersion:  "currentGatewayResVersion",
				identity:         "new-identity",
				ProbeConfig: &ProbeConfig{
					path:            defaultProbePath,
					port:            defaultProbePort,
					periodInSeconds: defaultProbePeriod,
				},
			},
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
	localResources: []string{
		mirroredServiceAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion",
			[]corev1.ServicePort{
				{
					Name:     "svc-1-port",
					Protocol: "TCP",
					Port:     8081,
				},
			}),
		endpointsAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
			[]corev1.EndpointPort{
				{
					Name:     "svc-1-port",
					Port:     888,
					Protocol: "TCP",
				}}),
		mirroredServiceAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion", []corev1.ServicePort{
			{
				Name:     "svc-2-port",
				Protocol: "TCP",
				Port:     8082,
			},
		}),
		endpointsAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
			[]corev1.EndpointPort{
				{
					Name:     "svc-2-port",
					Port:     888,
					Protocol: "TCP",
				}}),
	},
}

var gatewayDeleted = &testEnvironment{
	events: []interface{}{
		&RemoteGatewayDeleted{
			gatewayData: gatewayMetadata{
				Name:      "gateway",
				Namespace: "gateway-ns",
			},
		},
	},
	localResources: []string{
		mirroredServiceAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion",
			[]corev1.ServicePort{
				{
					Name:     "svc-1-port",
					Protocol: "TCP",
					Port:     8081,
				},
			}),
		endpointsAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
			[]corev1.EndpointPort{
				{
					Name:     "svc-1-port",
					Port:     888,
					Protocol: "TCP",
				}}),
		mirroredServiceAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "", "pastGatewayResVersion", []corev1.ServicePort{
			{
				Name:     "svc-2-port",
				Protocol: "TCP",
				Port:     8082,
			},
		}),
		endpointsAsYaml("test-service-2-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "",
			[]corev1.EndpointPort{
				{
					Name:     "svc-2-port",
					Port:     888,
					Protocol: "TCP",
				}}),
	},
}

var clusterUnregistered = &testEnvironment{
	events: []interface{}{
		&ClusterUnregistered{},
	},
	localResources: []string{
		mirroredServiceAsYaml("test-service-1-remote", "test-namespace", "", "", "", "", nil),
		endpointsAsYaml("test-service-1-remote", "test-namespace", "", "", "", "", nil),
		mirroredServiceAsYaml("test-service-2-remote", "test-namespace", "", "", "", "", nil),
		endpointsAsYaml("test-service-2-remote", "test-namespace", "", "", "", "", nil),
	},
}

var gcTriggered = &testEnvironment{
	events: []interface{}{
		&OprhanedServicesGcTriggered{},
	},
	localResources: []string{
		mirroredServiceAsYaml("test-service-1-remote", "test-namespace", "gateway", "gateway-ns", "", "", nil),
		endpointsAsYaml("test-service-1-remote", "test-namespace", "", "", "", "", nil),
		mirroredServiceAsYaml("test-service-2-remote", "test-namespace", "", "", "", "", nil),
		endpointsAsYaml("test-service-2-remote", "test-namespace", "", "", "", "", nil),
	},
	remoteResources: []string{
		remoteServiceAsYaml("test-service-1", "test-namespace", "gateway", "gateway-ns", "", nil),
	},
}

func onAddOrUpdateExportedSvc(isAdd bool) *testEnvironment {
	return &testEnvironment{
		events: []interface{}{
			onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "resVersion", nil)),
		},
	}

}

func onAddOrUpdateRemoteServiceUpdated(isAdd bool) *testEnvironment {
	return &testEnvironment{
		events: []interface{}{
			onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "currentResVersion", nil)),
		},
		localResources: []string{
			mirroredServiceAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "pastResourceVersion", "gatewayResVersion", nil),
			endpointsAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "", nil),
		},
	}
}

func onAddOrUpdateSameResVersion(isAdd bool) *testEnvironment {
	return &testEnvironment{
		events: []interface{}{
			onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "currentResVersion", nil)),
		},
		localResources: []string{
			mirroredServiceAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "currentResVersion", "gatewayResVersion", nil),
			endpointsAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "", nil),
		},
	}
}

func serviceNotExportedAnymore(isAdd bool) *testEnvironment {
	return &testEnvironment{
		events: []interface{}{
			onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "", "gateway-ns", "currentResVersion", nil)),
		},
		localResources: []string{
			mirroredServiceAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "currentResVersion", "gatewayResVersion", nil),
			endpointsAsYaml("test-service-remote", "test-namespace", "gateway", "gateway-ns", "0.0.0.0", "", nil),
		},
	}
}

var onDeleteWithGatewayMetadata = &testEnvironment{
	events: []interface{}{
		&OnDeleteCalled{
			svc: remoteService("test-service", "test-namespace", "gateway", "gateway-ns", "currentResVersion", nil),
		},
	},
}

var onDeleteNoGatewayMetadata = &testEnvironment{
	events: []interface{}{
		&OnDeleteCalled{
			svc: remoteService("gateway", "test-namespace", "", "", "currentResVersion", nil),
		},
	},
}

// the following tests ensure that onAdd, onUpdate and onDelete result in
// queueing more specific events to be processed

func onAddOrUpdateEvent(isAdd bool, svc *corev1.Service) interface{} {
	if isAdd {
		return &OnAddCalled{svc: svc}
	}
	return &OnUpdateCalled{svc: svc}
}

func diffServices(expected, actual *corev1.Service) error {
	if expected.Name != actual.Name {
		return fmt.Errorf("was expecting service with name %s but was %s", expected.Name, actual.Name)
	}

	if expected.Namespace != actual.Namespace {
		return fmt.Errorf("was expecting service with namespace %s but was %s", expected.Namespace, actual.Namespace)
	}

	if !reflect.DeepEqual(expected.Annotations, actual.Annotations) {
		return fmt.Errorf("was expecting service with annotations %v but got %v", expected.Annotations, actual.Annotations)
	}

	if !reflect.DeepEqual(expected.Labels, actual.Labels) {
		return fmt.Errorf("was expecting service with labels %v but got %v", expected.Labels, actual.Labels)
	}

	return nil
}

func diffEndpoints(expected, actual *corev1.Endpoints) error {
	if expected.Name != actual.Name {
		return fmt.Errorf("was expecting endpoints with name %s but was %s", expected.Name, actual.Name)
	}

	if expected.Namespace != actual.Namespace {
		return fmt.Errorf("was expecting endpoints with namespace %s but was %s", expected.Namespace, actual.Namespace)
	}

	if !reflect.DeepEqual(expected.Annotations, actual.Annotations) {
		return fmt.Errorf("was expecting endpoints with annotations %v but got %v", expected.Annotations, actual.Annotations)
	}

	if !reflect.DeepEqual(expected.Labels, actual.Labels) {
		return fmt.Errorf("was expecting endpoints with labels %v but got %v", expected.Labels, actual.Labels)
	}

	if !reflect.DeepEqual(expected.Subsets, actual.Subsets) {
		return fmt.Errorf("was expecting endpoints with subsets %v but got %v", expected.Subsets, actual.Subsets)
	}

	return nil
}

func remoteService(name, namespace, gtwName, gtwNs, resourceVersion string, ports []corev1.ServicePort) *corev1.Service {
	annotations := make(map[string]string)
	if gtwName != "" && gtwNs != "" {
		annotations[consts.GatewayNameAnnotation] = gtwName
		annotations[consts.GatewayNsAnnotation] = gtwNs
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
			Annotations:     annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
		},
	}
}

func remoteServiceAsYaml(name, namespace, gtwName, gtwNs, resourceVersion string, ports []corev1.ServicePort) string {
	svc := remoteService(name, namespace, gtwName, gtwNs, resourceVersion, ports)

	bytes, err := yaml.Marshal(svc)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}

func mirroredService(name, namespace, gtwName, gtwNs, resourceVersion, gatewayResourceVersion string, ports []corev1.ServicePort) *corev1.Service {
	annotations := make(map[string]string)
	annotations[consts.RemoteResourceVersionAnnotation] = resourceVersion
	annotations[consts.RemoteServiceFqName] = fmt.Sprintf("%s.%s.svc.cluster.local", strings.Replace(name, "-remote", "", 1), namespace)

	if gatewayResourceVersion != "" {
		annotations[consts.RemoteGatewayResourceVersionAnnotation] = gatewayResourceVersion

	}
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				consts.RemoteClusterNameLabel: "remote",
				consts.MirroredResourceLabel:  "true",
				consts.RemoteGatewayNameLabel: gtwName,
				consts.RemoteGatewayNsLabel:   gtwNs,
			},
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
		},
	}
}

func mirroredServiceAsYaml(name, namespace, gtwName, gtwNs, resourceVersion, gatewayResourceVersion string, ports []corev1.ServicePort) string {
	svc := mirroredService(name, namespace, gtwName, gtwNs, resourceVersion, gatewayResourceVersion, ports)

	bytes, err := yaml.Marshal(svc)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}

func gateway(name, namespace, resourceVersion, ip, hostname, portName string, port int32, identity string, probePort int32, probePath string, probePeriod int) *corev1.Service {
	svc := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
			Annotations: map[string]string{
				consts.GatewayIdentity:               identity,
				consts.GatewayProbePath:              probePath,
				consts.GatewayProbePeriod:            fmt.Sprint(probePeriod),
				consts.MulticlusterGatewayAnnotation: "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     portName,
					Protocol: "TCP",
					Port:     port,
				},
				{
					Name:     consts.ProbePortName,
					Protocol: "TCP",
					Port:     probePort,
				},
			},
		},
	}

	if ip != "" {
		svc.Status.LoadBalancer.Ingress = append(svc.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{IP: ip})
	}
	if hostname != "" {
		svc.Status.LoadBalancer.Ingress = append(svc.Status.LoadBalancer.Ingress, corev1.LoadBalancerIngress{Hostname: hostname})
	}
	return &svc
}

func gatewayAsYaml(name, namespace, resourceVersion, ip, hostname, portName string, port int32, identity string, probePort int32, probePath string, probePeriod int) string {
	gtw := gateway(name, namespace, resourceVersion, ip, hostname, portName, port, identity, probePort, probePath, probePeriod)

	bytes, err := yaml.Marshal(gtw)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}

func endpoints(name, namespace, gtwName, gtwNs, gatewayIP string, gatewayIdentity string, ports []corev1.EndpointPort) *corev1.Endpoints {
	var subsets []corev1.EndpointSubset
	if gatewayIP != "" {
		subsets = []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP: gatewayIP,
					},
				},
				Ports: ports,
			},
		}
	}

	endpoints := &corev1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				consts.RemoteClusterNameLabel: "remote",
				consts.MirroredResourceLabel:  "true",
				consts.RemoteGatewayNameLabel: gtwName,
				consts.RemoteGatewayNsLabel:   gtwNs,
			},
			Annotations: map[string]string{
				consts.RemoteServiceFqName: fmt.Sprintf("%s.%s.svc.cluster.local", strings.Replace(name, "-remote", "", 1), namespace),
			},
		},
		Subsets: subsets,
	}

	if gatewayIdentity != "" {
		endpoints.Annotations[consts.RemoteGatewayIdentity] = gatewayIdentity
	}

	return endpoints
}

func endpointsAsYaml(name, namespace, gtwName, gtwNs, gatewayIP, gatewayIdentity string, ports []corev1.EndpointPort) string {
	ep := endpoints(name, namespace, gtwName, gtwNs, gatewayIP, gatewayIdentity, ports)

	bytes, err := yaml.Marshal(ep)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}
