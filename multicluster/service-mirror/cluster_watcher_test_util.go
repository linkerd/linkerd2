package servicemirror

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/multicluster"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
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

var (
	defaultProbeSpec = multicluster.ProbeSpec{
		Path:   defaultProbePath,
		Port:   defaultProbePort,
		Period: time.Duration(defaultProbePeriod) * time.Second,
	}
	defaultSelector, _ = metav1.ParseToLabelSelector(consts.DefaultExportedServiceSelector)
)

type testEnvironment struct {
	events          []interface{}
	remoteResources []string
	localResources  []string
	link            multicluster.Link
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
		link:                       &te.link,
		remoteAPIClient:            remoteAPI,
		localAPIClient:             localAPI,
		stopper:                    nil,
		log:                        logging.WithFields(logging.Fields{"cluster": clusterName}),
		eventsQueue:                watcherQueue,
		requeueLimit:               0,
		headlessServicesEnabled:    true,
		endpointMirrorServiceCache: NewEndpointMirrorServiceCache(),
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
		&RemoteServiceCreated{
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
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "gateway-identity",
		GatewayAddress:      "192.0.2.127",
		GatewayPort:         888,
		ProbeSpec:           defaultProbeSpec,
		Selector:            *defaultSelector,
	},
}

var createExportedHeadlessService = &testEnvironment{
	events: []interface{}{
		&RemoteServiceCreated{
			service: remoteHeadlessService("service-one", "ns3", "111", map[string]string{
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
			ep: remoteHeadlessEndpoints("service-one", "ns3", "112", "192.0.0.1", []corev1.EndpointPort{
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
		remoteHeadlessSvcAsYaml("service-one", "ns3", "111",
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
		remoteHeadlessEndpointsAsYaml("service-one", "ns3", "112", "192.0.0.1", []corev1.EndpointPort{
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
		namespaceAsYaml("ns3"),
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
		Selector: *defaultSelector,
	},
}

var createExportedGlobalService = &testEnvironment{
	events: []interface{}{
		&RemoteServiceCreated{
			service: remoteHeadlessService("service-one", "ns4", "111", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
				consts.GlobalServiceNameLabel:         "service-one-global",
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
			ep: remoteHeadlessEndpoints("service-one", "ns4", "112", "192.0.0.1", []corev1.EndpointPort{
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
		remoteGlobalSvcAsYaml("service-one", "ns4", "111", "service-one-global",
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
		remoteHeadlessEndpointsAsYaml("service-one", "ns4", "112", "192.0.0.1", []corev1.EndpointPort{
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
		namespaceAsYaml("ns4"),
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
		Selector: *defaultSelector,
	},
}

var deleteMirrorService = &testEnvironment{
	events: []interface{}{
		&RemoteServiceDeleted{
			Name:      "test-service-remote-to-delete",
			Namespace: "test-namespace-to-delete",
		},
	},
	localResources: []string{
		mirrorServiceAsYaml("test-service-remote-to-delete-remote", "test-namespace-to-delete", "", nil),
		endpointsAsYaml("test-service-remote-to-delete-remote", "test-namespace-to-delete", "", "gateway-identity", nil),
	},
	link: multicluster.Link{
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "gateway-identity",
		GatewayAddress:      "192.0.2.127",
		GatewayPort:         888,
		ProbeSpec:           defaultProbeSpec,
		Selector:            *defaultSelector,
	},
}

var deleteHeadlessMirrorService = &testEnvironment{
	events: []interface{}{
		&RemoteServiceDeleted{
			Name:      "test-service-remote-to-delete",
			Namespace: "test-namespace-to-delete",
		},
	},
	localResources: []string{
		headlessMirrorAsYaml("test-service-remote-to-delete-remote", "test-namespace-to-delete", "", nil),
		endpointsAsYaml("test-service-remote-to-delete-remote", "test-namespace-to-delete", "", "gateway-identity", nil),
	},
	link: multicluster.Link{
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "gateway-identity",
		GatewayAddress:      "192.0.2.127",
		GatewayPort:         888,
		ProbeSpec:           defaultProbeSpec,
		Selector:            *defaultSelector,
	},
}

func StringRef(s string) *string { return &s }

func ProtocolRef(s v1.Protocol) *v1.Protocol { return &s }

func Int32Ref(s int32) *int32 { return &s }

var deleteGlobalMirrorService = &testEnvironment{
	events: []interface{}{
		&RemoteServiceDeleted{
			Name:       "test-service-remote-to-delete",
			Namespace:  "test-namespace-to-delete",
			GlobalName: StringRef("test-service-remote-to-delete-global"),
		},
	},
	localResources: append(
		globalMirrorPairAsYaml("test-service-remote-to-delete-remote", "test-namespace-to-delete", "", "test-service-remote-to-delete-global", nil),
		endpointsAsYaml("test-service-remote-to-delete-remote", "test-namespace-to-delete", "", "gateway-identity", nil),
	),
	link: multicluster.Link{
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "gateway-identity",
		GatewayAddress:      "192.0.2.127",
		GatewayPort:         888,
		ProbeSpec:           defaultProbeSpec,
		Selector:            *defaultSelector,
	},
}

var updateServiceWithChangedPorts = &testEnvironment{
	events: []interface{}{
		&RemoteServiceUpdated{
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
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "gateway-identity",
		GatewayAddress:      "192.0.2.127",
		GatewayPort:         888,
		ProbeSpec:           defaultProbeSpec,
		Selector:            *defaultSelector,
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
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "gateway-identity",
		GatewayAddress:      "192.0.2.127",
		GatewayPort:         888,
		ProbeSpec:           defaultProbeSpec,
		Selector:            *defaultSelector,
	},
}

var updateEndpointSlicesWithAddedHosts = &testEnvironment{
	events: []interface{}{
		&OnUpdateEndpointsCalled{
			ep: remoteHeadlessEndpointsUpdate("service-two", "estest", "112", "192.0.0.1", []corev1.EndpointPort{
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
		remoteGlobalSvcAsYaml("service-two", "estest", "222", "service-two-global",
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
	localResources: append(
		globalMirrorPairAsYaml("service-two-remote", "estest", "222", "service-two-global",
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
		globalMirrorEndpointSliceAsYaml("service-two-global", "service-two", "estest", "pod-0", "", "gateway-identity", nil),
		endpointMirrorAsYaml("pod-0", "service-two-remote", "estest", "333", []corev1.ServicePort{
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
			"estest",
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
			"estest",
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
	),
	link: multicluster.Link{
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "gateway-identity",
		GatewayAddress:      "192.0.2.127",
		GatewayPort:         888,
		ProbeSpec:           defaultProbeSpec,
		Selector:            *defaultSelector,
	},
}

var updateEndpointSlicesWithRemovedHosts = &testEnvironment{
	events: []interface{}{
		&OnUpdateEndpointsCalled{
			ep: remoteHeadlessEndpoints("service-two", "estest", "112", "192.0.0.1", []corev1.EndpointPort{
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
		remoteGlobalSvcAsYaml("service-two", "estest", "222", "service-two-global",
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
	localResources: append(
		globalMirrorPairAsYaml("service-two-remote", "estest", "222", "service-two-global",
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
		globalMirrorEndpointSliceAsYaml("service-two-global", "service-two", "estest", "pod-0", "", "gateway-identity", nil),
		headlessMirrorEndpointsUpdatedAsYaml(
			"service-two-remote",
			"estest",
			[]string{"pod-0", "pod-1"},
			[]string{"", ""},
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
		endpointMirrorAsYaml("pod-0", "service-two-remote", "estest", "333", []corev1.ServicePort{
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
			"estest",
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
		endpointMirrorAsYaml("pod-1", "service-two-remote", "estest", "333", []corev1.ServicePort{
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
			"estest",
			"pod-1",
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
	),
	link: multicluster.Link{
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "gateway-identity",
		GatewayAddress:      "192.0.2.127",
		GatewayPort:         888,
		ProbeSpec:           defaultProbeSpec,
		Selector:            *defaultSelector,
	},
}

var clusterUnregistered = &testEnvironment{
	events: []interface{}{
		&ClusterUnregistered{},
	},
	localResources: append(
		globalMirrorPairAsYaml("test-service-3-remote", "test-namespace", "", "test-service-3-global", nil),
		mirrorServiceAsYaml("test-service-1-remote", "test-namespace", "", nil),
		endpointsAsYaml("test-service-1-remote", "test-namespace", "", "", nil),
		mirrorServiceAsYaml("test-service-2-remote", "test-namespace", "", nil),
		endpointsAsYaml("test-service-2-remote", "test-namespace", "", "", nil),
		globalMirrorEndpointSliceAsYaml("test-service-3-global", "test-service-3", "test-namespace", "", "", "", nil),
	),
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
		globalMirrorEndpointSliceAsYaml("test-service-3-global", "test-service-3", "test-namespace", "foo", "bar", "baz", nil),
	},
	remoteResources: []string{
		remoteServiceAsYaml("test-service-1", "test-namespace", "", nil),
	},
	link: multicluster.Link{
		TargetClusterName: clusterName,
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
			TargetClusterName:   clusterName,
			TargetClusterDomain: clusterDomain,
			GatewayIdentity:     "gateway-identity",
			GatewayAddress:      "192.0.2.127",
			GatewayPort:         888,
			ProbeSpec:           defaultProbeSpec,
			Selector:            *defaultSelector,
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
			TargetClusterName:   clusterName,
			TargetClusterDomain: clusterDomain,
			GatewayIdentity:     "gateway-identity",
			GatewayAddress:      "192.0.2.127",
			GatewayPort:         888,
			ProbeSpec:           defaultProbeSpec,
			Selector:            *defaultSelector,
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
			TargetClusterName:   clusterName,
			TargetClusterDomain: clusterDomain,
			GatewayIdentity:     "gateway-identity",
			GatewayAddress:      "192.0.2.127",
			GatewayPort:         888,
			ProbeSpec:           defaultProbeSpec,
			Selector:            *defaultSelector,
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
			TargetClusterName:   clusterName,
			TargetClusterDomain: clusterDomain,
			GatewayIdentity:     "gateway-identity",
			GatewayAddress:      "192.0.2.127",
			GatewayPort:         888,
			ProbeSpec:           defaultProbeSpec,
			Selector:            *defaultSelector,
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
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "gateway-identity",
		GatewayAddress:      "192.0.2.127",
		GatewayPort:         888,
		ProbeSpec:           defaultProbeSpec,
		Selector:            *defaultSelector,
	},
}

var onDeleteExportedGlobalService = &testEnvironment{
	events: []interface{}{
		&OnDeleteCalled{
			svc: remoteService("test-service", "test-namespace", "currentResVersion", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
				consts.GlobalServiceNameLabel:         "test-service-global",
			}, nil),
		},
	},
	link: multicluster.Link{
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "gateway-identity",
		GatewayAddress:      "192.0.2.127",
		GatewayPort:         888,
		ProbeSpec:           defaultProbeSpec,
		Selector:            *defaultSelector,
	},
}

var onDeleteNonExportedService = &testEnvironment{
	events: []interface{}{
		&OnDeleteCalled{
			svc: remoteService("gateway", "test-namespace", "currentResVersion", map[string]string{}, nil),
		},
	},
	link: multicluster.Link{
		TargetClusterName:   clusterName,
		TargetClusterDomain: clusterDomain,
		GatewayIdentity:     "gateway-identity",
		GatewayAddress:      "192.0.2.127",
		GatewayPort:         888,
		ProbeSpec:           defaultProbeSpec,
		Selector:            *defaultSelector,
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
	errorPrefix := func() string {
		return fmt.Sprintf("was expecting service %s/%s", expected.Namespace, expected.Name)
	}

	if expected.Name != actual.Name {
		return fmt.Errorf("%s with name %s but was %s", errorPrefix(), expected.Name, actual.Name)
	}

	if expected.Namespace != actual.Namespace {
		return fmt.Errorf("%s with namespace %s but was %s", errorPrefix(), expected.Namespace, actual.Namespace)
	}

	if !reflect.DeepEqual(expected.Annotations, actual.Annotations) {
		return fmt.Errorf("%s with annotations %v but got %v", errorPrefix(), expected.Annotations, actual.Annotations)
	}

	if !reflect.DeepEqual(expected.Labels, actual.Labels) {
		return fmt.Errorf("%s with labels %v but got %v", errorPrefix(), expected.Labels, actual.Labels)
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

func diffEndpointSlice(expected, actual *discoveryv1beta1.EndpointSlice) error {
	errorPrefix := func() string {
		return fmt.Sprintf("was expecting endpoint slice %s/%s", expected.Namespace, expected.Name)
	}

	if expected.Name != actual.Name {
		return fmt.Errorf("%s with name %s but was %s", errorPrefix(), expected.Name, actual.Name)
	}

	if expected.Namespace != actual.Namespace {
		return fmt.Errorf("%s with namespace %s but was %s", errorPrefix(), expected.Namespace, actual.Namespace)
	}

	if !reflect.DeepEqual(expected.Annotations, actual.Annotations) {
		return fmt.Errorf("%s with annotations %v but got %v", errorPrefix(), expected.Annotations, actual.Annotations)
	}

	if !reflect.DeepEqual(expected.Labels, actual.Labels) {
		return fmt.Errorf("%s with labels %v but got %v", errorPrefix(), expected.Labels, actual.Labels)
	}

	if !reflect.DeepEqual(expected.Endpoints, actual.Endpoints) {
		expected, _ := json.MarshalIndent(expected.Endpoints, "", "  ")
		actual, _ := json.MarshalIndent(actual.Endpoints, "", "  ")
		return fmt.Errorf("%s with endpoints %s but got %s", errorPrefix(), string(expected), string(actual))
	}

	// TODO
	if !reflect.DeepEqual(expected.Ports, actual.Ports) {
		expected, _ := json.MarshalIndent(expected.Ports, "", "  ")
		actual, _ := json.MarshalIndent(actual.Ports, "", "  ")
		return fmt.Errorf("%s with ports %s but got %s", errorPrefix(), string(expected), string(actual))
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

func remoteGlobalSvcAsYaml(name, namespace, resourceVersion string, globalName string, ports []corev1.ServicePort) string {
	svc := remoteHeadlessService(name, namespace, resourceVersion, nil, ports)
	if svc.ObjectMeta.Labels == nil {
		svc.ObjectMeta.Labels = map[string]string{}
	}
	svc.ObjectMeta.Labels[consts.GlobalServiceNameLabel] = globalName

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

func globalMirrorServicePair(name, namespace, resourceVersion string, globalName string, ports []corev1.ServicePort) []*corev1.Service {
	headlessService := headlessMirrorService(name, namespace, resourceVersion, ports)
	headlessService.Labels[consts.GlobalServiceNameLabel] = globalName
	globalService := headlessService.DeepCopy()
	globalService.Name = globalName

	return []*corev1.Service{
		headlessService,
		globalService,
	}
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

func globalMirrorPairAsYaml(name, namespace, resourceVersion string, globalName string, ports []corev1.ServicePort) []string {
	ret := []string{}

	servicePair := globalMirrorServicePair(name, namespace, resourceVersion, globalName, ports)

	for _, svc := range servicePair {
		bytes, err := yaml.Marshal(svc)
		if err != nil {
			log.Fatal(err)
		}
		ret = append(ret, string(bytes))
	}

	return ret
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

func globalMirrorEndpointSlice(globalName, remoteServiceName, namespace, hostname, hostIP, gatewayIdentity string, ports []discoveryv1beta1.EndpointPort) *discoveryv1beta1.EndpointSlice {
	if ports == nil {
		ports = []discoveryv1beta1.EndpointPort{}
	}

	endpoints := &discoveryv1beta1.EndpointSlice{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EndpointSlice",
			APIVersion: "discovery.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%s", globalName, "remote", remoteServiceName),
			Namespace: namespace,
			Labels: map[string]string{
				"kubernetes.io/service-name":             globalName,
				"endpointslice.kubernetes.io/managed-by": "linkerd",
				consts.RemoteClusterNameLabel:            clusterName,
				consts.MirroredResourceLabel:             "true",
			},
			Annotations: map[string]string{
				consts.RemoteServiceFqName: fmt.Sprintf("%s.%s.svc.cluster.local", remoteServiceName, namespace),
			},
		},
		AddressType: discoveryv1beta1.AddressType(v1.IPv4Protocol),
		Endpoints: []discoveryv1beta1.Endpoint{
			{
				Hostname:  &hostname,
				Addresses: []string{hostIP},
			},
		},
		Ports: ports,
	}

	if gatewayIdentity != "" {
		endpoints.Annotations[consts.RemoteGatewayIdentity] = gatewayIdentity
	}

	return endpoints
}

func globalMirrorEndpointSliceUpdated(globalName, remoteServiceName, namespace string, hostnames, hostIPs []string, gatewayIdentity string, ports []discoveryv1beta1.EndpointPort) *discoveryv1beta1.EndpointSlice {
	if ports == nil {
		ports = []discoveryv1beta1.EndpointPort{}
	}
	endpoints := &discoveryv1beta1.EndpointSlice{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EndpointSlice",
			APIVersion: "discovery.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%s", globalName, "remote", remoteServiceName),
			Namespace: namespace,
			Labels: map[string]string{
				"kubernetes.io/service-name":             globalName,
				"endpointslice.kubernetes.io/managed-by": "linkerd",
				consts.RemoteClusterNameLabel:            clusterName,
				consts.MirroredResourceLabel:             "true",
			},
			Annotations: map[string]string{
				consts.RemoteServiceFqName: fmt.Sprintf("%s.%s.svc.cluster.local", remoteServiceName, namespace),
			},
		},
		Endpoints: []discoveryv1beta1.Endpoint{
			{
				Hostname:  &hostnames[0],
				Addresses: []string{hostIPs[0]},
			},
			{
				Hostname:  &hostnames[1],
				Addresses: []string{hostIPs[1]},
			},
		},
		Ports: ports,
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

func headlessMirrorEndpointsUpdatedAsYaml(name, namespace string, hostname, hostIP []string, gatewayIdentity string, ports []corev1.EndpointPort) string {
	ep := headlessMirrorEndpointsUpdated(name, namespace, hostname, hostIP, gatewayIdentity, ports)

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

func globalMirrorEndpointSliceAsYaml(globalName, remoteServiceName, namespace, hostname, hostIP, gatewayIdentity string, ports []discoveryv1beta1.EndpointPort) string {
	ep := globalMirrorEndpointSlice(globalName, remoteServiceName, namespace, hostname, hostIP, gatewayIdentity, ports)

	bytes, err := yaml.Marshal(ep)
	if err != nil {
		log.Fatal(err)
	}

	return string(bytes)
}

func globalMirrorEndpointSliceUpdatedAsYaml(globalName, remoteServiceName, namespace string, hostname, hostIP []string, gatewayIdentity string, ports []discoveryv1beta1.EndpointPort) string {
	ep := globalMirrorEndpointSliceUpdated(globalName, remoteServiceName, namespace, hostname, hostIP, gatewayIdentity, ports)

	bytes, err := yaml.Marshal(ep)
	if err != nil {
		log.Fatal(err)
	}

	return string(bytes)
}
