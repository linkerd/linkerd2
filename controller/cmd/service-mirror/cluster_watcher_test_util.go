package servicemirror

import (
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
		link:            &te.link,
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
		&OprhanedServicesGcTriggered{},
	},
	localResources: []string{
		mirrorServiceAsYaml("test-service-1-remote", "test-namespace", "", nil),
		endpointsAsYaml("test-service-1-remote", "test-namespace", "", "", nil),
		mirrorServiceAsYaml("test-service-2-remote", "test-namespace", "", nil),
		endpointsAsYaml("test-service-2-remote", "test-namespace", "", "", nil),
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

func remoteServiceAsYaml(name, namespace, resourceVersion string, ports []corev1.ServicePort) string {
	svc := remoteService(name, namespace, resourceVersion, nil, ports)

	bytes, err := yaml.Marshal(svc)
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

func mirrorServiceAsYaml(name, namespace, resourceVersion string, ports []corev1.ServicePort) string {
	svc := mirrorService(name, namespace, resourceVersion, ports)

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

func endpointsAsYaml(name, namespace, gatewayIP, gatewayIdentity string, ports []corev1.EndpointPort) string {
	ep := endpoints(name, namespace, gatewayIP, gatewayIdentity, ports)

	bytes, err := yaml.Marshal(ep)
	if err != nil {
		log.Fatal(err)
	}
	return string(bytes)
}
