package servicemirror

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-test/deep"
	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/multicluster"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/yaml"
)

const (
	clusterName        = "remote"
	clusterDomain      = "cluster.local"
	defaultProbePath   = "/probe"
	defaultProbePort   = 12345
	defaultProbePeriod = 60
)

var (
	defaultProbeSpec = multicluster.ProbeSpec{
		Path:   defaultProbePath,
		Port:   defaultProbePort,
		Period: time.Duration(defaultProbePeriod) * time.Second,
	}
	defaultSelector, _                = metav1.ParseToLabelSelector(consts.DefaultExportedServiceSelector + "=true")
	defaultRemoteDiscoverySelector, _ = metav1.ParseToLabelSelector(consts.DefaultExportedServiceSelector + "=remote-discovery")
)

type testEnvironment struct {
	events          []interface{}
	remoteResources []string
	localResources  []string
	link            multicluster.Link
}

func (te *testEnvironment) runEnvironment(watcherQueue workqueue.TypedRateLimitingInterface[any]) (*k8s.API, error) {
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
		link:                    &te.link,
		remoteAPIClient:         remoteAPI,
		localAPIClient:          localAPI,
		stopper:                 nil,
		log:                     logging.WithFields(logging.Fields{"cluster": clusterName}),
		eventsQueue:             watcherQueue,
		requeueLimit:            0,
		gatewayAlive:            true,
		headlessServicesEnabled: true,
	}

	for _, ev := range te.events {
		watcherQueue.Add(ev)
	}

	for range te.events {
		watcher.processNextEvent(context.Background())
	}

	localAPI.Sync(nil)
	remoteAPI.Sync(nil)

	return localAPI, nil
}

var createExportedService = &testEnvironment{
	events: []interface{}{
		&RemoteServiceExported{
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
		},
	},
	remoteResources: []string{
		gatewayAsYaml("existing-gateway", "existing-namespace", "222", "192.0.2.127", "mc-gateway", 888, "gateway-identity", defaultProbePort, defaultProbePath, defaultProbePeriod),
		endpointsAsYaml("service-one", "ns1", "192.0.2.127", "gateway-identity", []corev1.EndpointPort{}),
	},
	localResources: []string{
		namespaceAsYaml("ns1"),
	},
	link: multicluster.Link{
		TargetClusterName:       clusterName,
		TargetClusterDomain:     clusterDomain,
		GatewayIdentity:         "gateway-identity",
		GatewayAddress:          "192.0.2.127",
		GatewayPort:             888,
		ProbeSpec:               defaultProbeSpec,
		Selector:                defaultSelector,
		RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
	},
}

var createRemoteDiscoveryService = &testEnvironment{
	events: []interface{}{
		&RemoteServiceExported{
			service: remoteService("service-one", "ns1", "111", map[string]string{
				consts.DefaultExportedServiceSelector: "remote-discovery",
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
		},
	},
	remoteResources: []string{
		endpointsAsYaml("service-one", "ns1", "192.0.2.127", "gateway-identity", []corev1.EndpointPort{}),
	},
	localResources: []string{
		namespaceAsYaml("ns1"),
	},
	link: multicluster.Link{
		TargetClusterName:       clusterName,
		TargetClusterDomain:     clusterDomain,
		GatewayIdentity:         "gateway-identity",
		GatewayAddress:          "192.0.2.127",
		GatewayPort:             888,
		ProbeSpec:               defaultProbeSpec,
		Selector:                defaultSelector,
		RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
	},
}

var createExportedHeadlessService = &testEnvironment{
	events: []interface{}{
		&RemoteServiceExported{
			service: remoteHeadlessService("service-one", "ns2", "111", map[string]string{
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
		},
		&OnAddEndpointsCalled{
			ep: remoteHeadlessEndpoints("service-one", "ns2", "112", "192.0.0.1", []corev1.EndpointPort{
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
	},
	remoteResources: []string{
		gatewayAsYaml("existing-gateway", "existing-namespace", "222", "192.0.2.129", "gateway", 889, "gateway-identity", 123456, "/probe1", 120),
		remoteHeadlessSvcAsYaml("service-one", "ns2", "111",
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
		remoteHeadlessEndpointsAsYaml("service-one", "ns2", "112", "192.0.0.1", []corev1.EndpointPort{
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
	localResources: []string{
		namespaceAsYaml("ns2"),
	},
	link: multicluster.Link{
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "gateway-identity",
		GatewayAddress:      "192.0.2.129",
		GatewayPort:         889,
		ProbeSpec: multicluster.ProbeSpec{
			Port:   123456,
			Path:   "/probe1",
			Period: 120,
		},
		Selector:                defaultSelector,
		RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
	},
}

var deleteMirrorService = &testEnvironment{
	events: []interface{}{
		&RemoteServiceUnexported{
			Name:      "test-service-remote-to-delete",
			Namespace: "test-namespace-to-delete",
		},
	},
	localResources: []string{
		mirrorServiceAsYaml("test-service-remote-to-delete-remote", "test-namespace-to-delete", "", nil),
		endpointsAsYaml("test-service-remote-to-delete-remote", "test-namespace-to-delete", "", "gateway-identity", nil),
	},
	link: multicluster.Link{
		TargetClusterName:       clusterName,
		TargetClusterDomain:     clusterDomain,
		GatewayIdentity:         "gateway-identity",
		GatewayAddress:          "192.0.2.127",
		GatewayPort:             888,
		ProbeSpec:               defaultProbeSpec,
		Selector:                defaultSelector,
		RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
	},
}

var updateServiceWithChangedPorts = &testEnvironment{
	events: []interface{}{
		&RemoteExportedServiceUpdated{
			remoteUpdate: remoteService("test-service", "test-namespace", "currentServiceResVersion", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, []corev1.ServicePort{
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
			localService: mirrorService("test-service-remote", "test-namespace", "pastServiceResVersion", []corev1.ServicePort{
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
			localEndpoints: endpoints("test-service-remote", "test-namespace", "192.0.2.127", "", []corev1.EndpointPort{
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
	remoteResources: []string{
		gatewayAsYaml("gateway", "gateway-ns", "currentGatewayResVersion", "192.0.2.127", "mc-gateway", 888, "", defaultProbePort, defaultProbePath, defaultProbePeriod),
	},
	localResources: []string{
		mirrorServiceAsYaml("test-service-remote", "test-namespace", "past", []corev1.ServicePort{
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
		endpointsAsYaml("test-service-remote", "test-namespace", "192.0.2.127", "", []corev1.EndpointPort{
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
	link: multicluster.Link{
		TargetClusterName:       clusterName,
		TargetClusterDomain:     clusterDomain,
		GatewayIdentity:         "gateway-identity",
		GatewayAddress:          "192.0.2.127",
		GatewayPort:             888,
		ProbeSpec:               defaultProbeSpec,
		Selector:                defaultSelector,
		RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
	},
}

var updateEndpointsWithChangedHosts = &testEnvironment{
	events: []interface{}{
		&OnUpdateEndpointsCalled{
			ep: remoteHeadlessEndpointsUpdate("service-two", "eptest", "112", "192.0.0.1", []corev1.EndpointPort{
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
	},
	remoteResources: []string{
		gatewayAsYaml("gateway", "gateway-ns", "currentGatewayResVersion", "192.0.2.127", "mc-gateway", 888, "", defaultProbePort, defaultProbePath, defaultProbePeriod),
		remoteHeadlessSvcAsYaml("service-two", "eptest", "222",
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
	localResources: []string{
		headlessMirrorAsYaml("service-two-remote", "eptest", "222",
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
		endpointMirrorAsYaml("pod-0", "service-two-remote", "eptest", "333", []corev1.ServicePort{
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
		headlessMirrorEndpointsAsYaml(
			"service-two-remote",
			"eptest",
			"pod-0",
			"",
			"gateway-identity",
			[]corev1.EndpointPort{
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
		endpointMirrorEndpointsAsYaml(
			"service-two-remote",
			"eptest",
			"pod-0",
			"192.0.2.127",
			"gateway-identity",
			[]corev1.EndpointPort{
				{
					Name:     "port1",
					Protocol: "TCP",
					Port:     888,
				},
				{
					Name:     "port2",
					Protocol: "TCP",
					Port:     888,
				},
			}),
	},
	link: multicluster.Link{
		TargetClusterName:       clusterName,
		TargetClusterDomain:     clusterDomain,
		GatewayIdentity:         "gateway-identity",
		GatewayAddress:          "192.0.2.127",
		GatewayPort:             888,
		ProbeSpec:               defaultProbeSpec,
		Selector:                defaultSelector,
		RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
	},
}
var clusterUnregistered = &testEnvironment{
	events: []interface{}{
		&ClusterUnregistered{},
	},
	localResources: []string{
		mirrorServiceAsYaml("test-service-1-remote", "test-namespace", "", nil),
		endpointsAsYaml("test-service-1-remote", "test-namespace", "", "", nil),
		mirrorServiceAsYaml("test-service-2-remote", "test-namespace", "", nil),
		endpointsAsYaml("test-service-2-remote", "test-namespace", "", "", nil),
	},
	link: multicluster.Link{
		TargetClusterName: clusterName,
	},
}

var gcTriggered = &testEnvironment{
	events: []interface{}{
		&OrphanedServicesGcTriggered{},
	},
	localResources: []string{
		mirrorServiceAsYaml("test-service-1-remote", "test-namespace", "", nil),
		endpointsAsYaml("test-service-1-remote", "test-namespace", "", "", nil),
		mirrorServiceAsYaml("test-service-2-remote", "test-namespace", "", nil),
		endpointsAsYaml("test-service-2-remote", "test-namespace", "", "", nil),
		headlessMirrorAsYaml("test-headless-service-remote", "test-namespace", "", nil),
		endpointMirrorAsYaml("pod-0", "test-headless-service-remote", "test-namespace", "", nil),
		headlessMirrorEndpointsAsYaml("test-headless-service-remote", "test-namespace", "pod-0", "", "", nil),
		endpointMirrorEndpointsAsYaml("test-headless-service-remote", "test-namespace", "pod-0", "", "", nil),
	},
	remoteResources: []string{
		remoteServiceAsYaml("test-service-1", "test-namespace", "", nil),
		remoteHeadlessSvcAsYaml("test-headless-service", "test-namespace", "", nil),
	},
	link: multicluster.Link{
		TargetClusterName: clusterName,
	},
}

var noGatewayLink = &testEnvironment{
	events: []interface{}{
		&RemoteServiceExported{
			service: remoteService("service-one", "ns1", "111", map[string]string{
				consts.DefaultExportedServiceSelector: "remote-discovery",
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
		},
		&RemoteServiceExported{
			service: remoteService("service-two", "ns1", "111", map[string]string{
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
		},
	},
	localResources: []string{
		namespaceAsYaml("ns1"),
	},
	remoteResources: []string{
		endpointsAsYaml("service-one", "ns1", "192.0.2.127", "gateway-identity", []corev1.EndpointPort{}),
		endpointsAsYaml("service-two", "ns1", "192.0.2.128", "gateway-identity", []corev1.EndpointPort{}),
	},
	link: multicluster.Link{
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "",
		GatewayAddress:      "",
		GatewayPort:         0,
		ProbeSpec: multicluster.ProbeSpec{
			Path:   "",
			Port:   0,
			Period: time.Duration(0) * time.Second,
		},
		Selector:                &metav1.LabelSelector{},
		RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
	},
}

func onAddOrUpdateExportedSvc(isAdd bool) *testEnvironment {
	return &testEnvironment{
		events: []interface{}{
			onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "resVersion", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, nil)),
		},
		link: multicluster.Link{
			TargetClusterName:       clusterName,
			TargetClusterDomain:     clusterDomain,
			GatewayIdentity:         "gateway-identity",
			GatewayAddress:          "192.0.2.127",
			GatewayPort:             888,
			ProbeSpec:               defaultProbeSpec,
			Selector:                defaultSelector,
			RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
		},
	}

}

func onAddOrUpdateRemoteServiceUpdated(isAdd bool) *testEnvironment {
	return &testEnvironment{
		events: []interface{}{
			onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "currentResVersion", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, nil)),
		},
		localResources: []string{
			mirrorServiceAsYaml("test-service-remote", "test-namespace", "pastResourceVersion", nil),
			endpointsAsYaml("test-service-remote", "test-namespace", "0.0.0.0", "", nil),
		},
		link: multicluster.Link{
			TargetClusterName:       clusterName,
			TargetClusterDomain:     clusterDomain,
			GatewayIdentity:         "gateway-identity",
			GatewayAddress:          "192.0.2.127",
			GatewayPort:             888,
			ProbeSpec:               defaultProbeSpec,
			Selector:                defaultSelector,
			RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
		},
	}
}

func onAddOrUpdateSameResVersion(isAdd bool) *testEnvironment {
	return &testEnvironment{
		events: []interface{}{
			onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "currentResVersion", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, nil)),
		},
		localResources: []string{
			mirrorServiceAsYaml("test-service-remote", "test-namespace", "currentResVersion", nil),
			endpointsAsYaml("test-service-remote", "test-namespace", "0.0.0.0", "", nil),
		},
		link: multicluster.Link{
			TargetClusterName:       clusterName,
			TargetClusterDomain:     clusterDomain,
			GatewayIdentity:         "gateway-identity",
			GatewayAddress:          "192.0.2.127",
			GatewayPort:             888,
			ProbeSpec:               defaultProbeSpec,
			Selector:                defaultSelector,
			RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
		},
	}
}

func serviceNotExportedAnymore(isAdd bool) *testEnvironment {
	return &testEnvironment{
		events: []interface{}{
			onAddOrUpdateEvent(isAdd, remoteService("test-service", "test-namespace", "currentResVersion", map[string]string{}, nil)),
		},
		localResources: []string{
			mirrorServiceAsYaml("test-service-remote", "test-namespace", "currentResVersion", nil),
			endpointsAsYaml("test-service-remote", "test-namespace", "0.0.0.0", "", nil),
		},
		link: multicluster.Link{
			TargetClusterName:       clusterName,
			TargetClusterDomain:     clusterDomain,
			GatewayIdentity:         "gateway-identity",
			GatewayAddress:          "192.0.2.127",
			GatewayPort:             888,
			ProbeSpec:               defaultProbeSpec,
			Selector:                defaultSelector,
			RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
		},
	}
}

var onDeleteExportedService = &testEnvironment{
	events: []interface{}{
		&OnDeleteCalled{
			svc: remoteService("test-service", "test-namespace", "currentResVersion", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, nil),
		},
	},
	link: multicluster.Link{
		TargetClusterName:       clusterName,
		TargetClusterDomain:     clusterDomain,
		GatewayIdentity:         "gateway-identity",
		GatewayAddress:          "192.0.2.127",
		GatewayPort:             888,
		ProbeSpec:               defaultProbeSpec,
		Selector:                defaultSelector,
		RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
	},
}

var onDeleteNonExportedService = &testEnvironment{
	events: []interface{}{
		&OnDeleteCalled{
			svc: remoteService("gateway", "test-namespace", "currentResVersion", map[string]string{}, nil),
		},
	},
	link: multicluster.Link{
		TargetClusterName:       clusterName,
		TargetClusterDomain:     clusterDomain,
		GatewayIdentity:         "gateway-identity",
		GatewayAddress:          "192.0.2.127",
		GatewayPort:             888,
		ProbeSpec:               defaultProbeSpec,
		Selector:                defaultSelector,
		RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
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

	if diff := deep.Equal(expected.Annotations, actual.Annotations); diff != nil {
		return fmt.Errorf("annotation mismatch %+v", diff)
	}

	if diff := deep.Equal(expected.Labels, actual.Labels); diff != nil {
		return fmt.Errorf("label mismatch %+v", diff)
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

	if diff := deep.Equal(expected.Annotations, actual.Annotations); diff != nil {
		return fmt.Errorf("annotation mismatch %+v", diff)
	}

	if diff := deep.Equal(expected.Labels, actual.Labels); diff != nil {
		return fmt.Errorf("label mismatch %+v", diff)
	}

	if diff := deep.Equal(expected.Subsets, actual.Subsets); diff != nil {
		return fmt.Errorf("subsets mismatch %+v", diff)
	}

	return nil
}

func remoteService(name, namespace, resourceVersion string, labels map[string]string, ports []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
			Labels:          labels,
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
		},
	}
}

func remoteHeadlessService(name, namespace, resourceVersion string, labels map[string]string, ports []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
			Labels:          labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports:     ports,
		},
	}
}

func remoteHeadlessEndpoints(name, namespace, resourceVersion, address string, ports []corev1.EndpointPort) *corev1.Endpoints {
	return &corev1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
			Labels: map[string]string{
				"service.kubernetes.io/headless":      "",
				consts.DefaultExportedServiceSelector: "true",
			},
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						Hostname: "pod-0",
						IP:       address,
						TargetRef: &corev1.ObjectReference{
							Name:            "pod-0",
							ResourceVersion: resourceVersion,
						},
					},
				},
				Ports: ports,
			},
		},
	}
}

func remoteHeadlessEndpointsUpdate(name, namespace, resourceVersion, address string, ports []corev1.EndpointPort) *corev1.Endpoints {
	return &corev1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
			Labels: map[string]string{
				"service.kubernetes.io/headless":      "",
				consts.DefaultExportedServiceSelector: "true",
			},
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						Hostname: "pod-0",
						IP:       address,
						TargetRef: &corev1.ObjectReference{
							Name:            "pod-0",
							ResourceVersion: resourceVersion,
						},
					},
					{
						Hostname: "pod-1",
						IP:       address,
						TargetRef: &corev1.ObjectReference{
							Name:            "pod-1",
							ResourceVersion: resourceVersion,
						},
					},
				},
				Ports: ports,
			},
		},
	}
}

func remoteServiceAsYaml(name, namespace, resourceVersion string, ports []corev1.ServicePort) string {
	svc := remoteService(name, namespace, resourceVersion, map[string]string{
		consts.DefaultExportedServiceSelector: "true",
	}, ports)

	bytes, err := yaml.Marshal(svc)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}

func remoteHeadlessSvcAsYaml(name, namespace, resourceVersion string, ports []corev1.ServicePort) string {
	svc := remoteHeadlessService(name, namespace, resourceVersion, nil, ports)

	bytes, err := yaml.Marshal(svc)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}

func remoteHeadlessEndpointsAsYaml(name, namespace, resourceVersion, address string, ports []corev1.EndpointPort) string {
	ep := remoteHeadlessEndpoints(name, namespace, resourceVersion, address, ports)

	bytes, err := yaml.Marshal(ep)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}
func mirrorService(name, namespace, resourceVersion string, ports []corev1.ServicePort) *corev1.Service {
	annotations := make(map[string]string)
	annotations[consts.RemoteResourceVersionAnnotation] = resourceVersion
	annotations[consts.RemoteServiceFqName] = fmt.Sprintf("%s.%s.svc.cluster.local", strings.Replace(name, "-remote", "", 1), namespace)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				consts.RemoteClusterNameLabel: clusterName,
				consts.MirroredResourceLabel:  "true",
			},
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
		},
	}
}

func headlessMirrorService(name, namespace, resourceVersion string, ports []corev1.ServicePort) *corev1.Service {
	svc := mirrorService(name, namespace, resourceVersion, ports)
	svc.Spec.ClusterIP = "None"
	return svc
}

func endpointMirrorService(hostname, rootName, namespace, resourceVersion string, ports []corev1.ServicePort) *corev1.Service {
	annotations := make(map[string]string)
	annotations[consts.RemoteResourceVersionAnnotation] = resourceVersion
	annotations[consts.RemoteServiceFqName] = fmt.Sprintf("%s.%s.%s.svc.cluster.local", hostname, strings.Replace(rootName, "-remote", "", 1), namespace)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", hostname, clusterName),
			Namespace: namespace,
			Labels: map[string]string{

				consts.MirroredHeadlessSvcNameLabel: rootName,
				consts.RemoteClusterNameLabel:       clusterName,
				consts.MirroredResourceLabel:        "true",
			},
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
		},
	}
}

func remoteDiscoveryMirrorService(name, namespace, resourceVersion string, ports []corev1.ServicePort) *corev1.Service {
	annotations := make(map[string]string)
	annotations[consts.RemoteResourceVersionAnnotation] = resourceVersion
	annotations[consts.RemoteServiceFqName] = fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", name, clusterName),
			Namespace: namespace,
			Labels: map[string]string{
				consts.RemoteClusterNameLabel: clusterName,
				consts.MirroredResourceLabel:  "true",
				consts.RemoteDiscoveryLabel:   clusterName,
				consts.RemoteServiceLabel:     name,
			},
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: ports,
		},
	}
}

func mirrorServiceAsYaml(name, namespace, resourceVersion string, ports []corev1.ServicePort) string {
	svc := mirrorService(name, namespace, resourceVersion, ports)

	bytes, err := yaml.Marshal(svc)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}

func headlessMirrorAsYaml(name, namespace, resourceVersion string, ports []corev1.ServicePort) string {
	svc := headlessMirrorService(name, namespace, resourceVersion, ports)

	bytes, err := yaml.Marshal(svc)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}

func endpointMirrorAsYaml(hostname, rootName, namespace, resourceVersion string, ports []corev1.ServicePort) string {
	svc := endpointMirrorService(hostname, rootName, namespace, resourceVersion, ports)

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
				consts.GatewayIdentity:    identity,
				consts.GatewayProbePath:   probePath,
				consts.GatewayProbePeriod: fmt.Sprint(probePeriod),
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

func gatewayAsYaml(name, namespace, resourceVersion, ip, portName string, port int32, identity string, probePort int32, probePath string, probePeriod int) string {
	gtw := gateway(name, namespace, resourceVersion, ip, "", portName, port, identity, probePort, probePath, probePeriod)

	bytes, err := yaml.Marshal(gtw)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}

func endpoints(name, namespace, gatewayIP string, gatewayIdentity string, ports []corev1.EndpointPort) *corev1.Endpoints {
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
				consts.RemoteClusterNameLabel: clusterName,
				consts.MirroredResourceLabel:  "true",
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

func endpointMirrorEndpoints(rootName, namespace, hostname, gatewayIP, gatewayIdentity string, ports []corev1.EndpointPort) *corev1.Endpoints {
	localName := fmt.Sprintf("%s-%s", hostname, clusterName)
	ep := endpoints(localName, namespace, gatewayIP, gatewayIdentity, ports)

	ep.Annotations[consts.RemoteServiceFqName] = fmt.Sprintf("%s.%s.%s.svc.cluster.local", hostname, strings.Replace(rootName, "-remote", "", 1), namespace)
	ep.Labels[consts.MirroredHeadlessSvcNameLabel] = rootName

	return ep
}

func headlessMirrorEndpoints(name, namespace, hostname, hostIP, gatewayIdentity string, ports []corev1.EndpointPort) *corev1.Endpoints {
	endpoints := &corev1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				consts.RemoteClusterNameLabel: clusterName,
				consts.MirroredResourceLabel:  "true",
			},
			Annotations: map[string]string{
				consts.RemoteServiceFqName: fmt.Sprintf("%s.%s.svc.cluster.local", strings.Replace(name, "-remote", "", 1), namespace),
			},
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						Hostname: hostname,
						IP:       hostIP,
					},
				},
				Ports: ports,
			},
		},
	}

	if gatewayIdentity != "" {
		endpoints.Annotations[consts.RemoteGatewayIdentity] = gatewayIdentity
	}

	return endpoints
}

func headlessMirrorEndpointsUpdated(name, namespace string, hostnames, hostIPs []string, gatewayIdentity string, ports []corev1.EndpointPort) *corev1.Endpoints {
	endpoints := &corev1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Endpoints",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				consts.RemoteClusterNameLabel: clusterName,
				consts.MirroredResourceLabel:  "true",
			},
			Annotations: map[string]string{
				consts.RemoteServiceFqName: fmt.Sprintf("%s.%s.svc.cluster.local", strings.Replace(name, "-remote", "", 1), namespace),
			},
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						Hostname: hostnames[0],
						IP:       hostIPs[0],
					},
					{
						Hostname: hostnames[1],
						IP:       hostIPs[1],
					},
				},
				Ports: ports,
			},
		},
	}

	if gatewayIdentity != "" {
		endpoints.Annotations[consts.RemoteGatewayIdentity] = gatewayIdentity
	}

	return endpoints
}

func namespaceAsYaml(name string) string {
	ns := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	bytes, err := yaml.Marshal(ns)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}

func endpointsAsYaml(name, namespace, gatewayIP, gatewayIdentity string, ports []corev1.EndpointPort) string {
	ep := endpoints(name, namespace, gatewayIP, gatewayIdentity, ports)

	bytes, err := yaml.Marshal(ep)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}

func headlessMirrorEndpointsAsYaml(name, namespace, hostname, hostIP, gatewayIdentity string, ports []corev1.EndpointPort) string {
	ep := headlessMirrorEndpoints(name, namespace, hostname, hostIP, gatewayIdentity, ports)

	bytes, err := yaml.Marshal(ep)
	if err != nil {
		log.Fatal(err)
	}

	return string(bytes)
}

func endpointMirrorEndpointsAsYaml(name, namespace, hostname, gatewayIP, gatewayIdentity string, ports []corev1.EndpointPort) string {
	ep := endpointMirrorEndpoints(name, namespace, hostname, gatewayIP, gatewayIdentity, ports)

	bytes, err := yaml.Marshal(ep)
	if err != nil {
		log.Fatal(err)
	}

	return string(bytes)
}

// createEnvWithSelector will create a test environment with two services. It
// accepts a default and a remote discovery selector which it will use for the
// link creation. This function is used to create environments that differ only
// in the selector used in the link.
func createEnvWithSelector(defaultSelector, remoteSelector *metav1.LabelSelector) *testEnvironment {
	return &testEnvironment{
		events: []interface{}{
			&OnAddCalled{
				svc: remoteService("service-one", "ns1", "111", map[string]string{
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
			&OnAddCalled{
				svc: remoteService("service-two", "ns1", "111", map[string]string{
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
			},
		},
		localResources: []string{
			namespaceAsYaml("ns1"),
		},
		remoteResources: []string{
			endpointsAsYaml("service-one", "ns1", "192.0.2.127", "gateway-identity", []corev1.EndpointPort{}),
			endpointsAsYaml("service-two", "ns1", "192.0.3.127", "gateway-identity", []corev1.EndpointPort{}),
		},
		link: multicluster.Link{
			TargetClusterName:   clusterName,
			TargetClusterDomain: clusterDomain,
			GatewayIdentity:     "",
			GatewayAddress:      "",
			GatewayPort:         0,
			ProbeSpec: multicluster.ProbeSpec{
				Path:   "",
				Port:   0,
				Period: time.Duration(0) * time.Second,
			},
			Selector:                defaultSelector,
			RemoteDiscoverySelector: remoteSelector,
		},
	}
}
