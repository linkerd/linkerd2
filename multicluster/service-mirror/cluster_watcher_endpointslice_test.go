package servicemirror

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// Helper functions for EndpointSlice testing

func remoteHeadlessEndpointSlice(name, serviceName, namespace, resourceVersion, address string, ports []discoveryv1.EndpointPort) *discoveryv1.EndpointSlice {
	hostname := "pod-0"
	ready := true
	return &discoveryv1.EndpointSlice{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EndpointSlice",
			APIVersion: "discovery.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
			Labels: map[string]string{
				discoveryv1.LabelServiceName:          serviceName,
				corev1.IsHeadlessService:              "",
				consts.DefaultExportedServiceSelector: "true",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses:  []string{address},
				Hostname:   &hostname,
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
				TargetRef: &corev1.ObjectReference{
					Kind:            "Pod",
					Name:            "pod-0",
					Namespace:       namespace,
					ResourceVersion: resourceVersion,
				},
			},
		},
		Ports: ports,
	}
}

func remoteHeadlessEndpointSliceUpdate(name, serviceName, namespace, resourceVersion, address string, ports []discoveryv1.EndpointPort) *discoveryv1.EndpointSlice {
	hostname0 := "pod-0"
	hostname1 := "pod-1"
	ready := true
	return &discoveryv1.EndpointSlice{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EndpointSlice",
			APIVersion: "discovery.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
			Labels: map[string]string{
				discoveryv1.LabelServiceName:          serviceName,
				corev1.IsHeadlessService:              "",
				consts.DefaultExportedServiceSelector: "true",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses:  []string{address},
				Hostname:   &hostname0,
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
				TargetRef: &corev1.ObjectReference{
					Kind:            "Pod",
					Name:            "pod-0",
					Namespace:       namespace,
					ResourceVersion: resourceVersion,
				},
			},
			{
				Addresses:  []string{address},
				Hostname:   &hostname1,
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
				TargetRef: &corev1.ObjectReference{
					Kind:            "Pod",
					Name:            "pod-1",
					Namespace:       namespace,
					ResourceVersion: resourceVersion,
				},
			},
		},
		Ports: ports,
	}
}

func endpointSlicePorts(ports []corev1.ServicePort) []discoveryv1.EndpointPort {
	result := make([]discoveryv1.EndpointPort, 0, len(ports))
	for _, p := range ports {
		port := p.Port
		protocol := p.Protocol
		name := p.Name
		result = append(result, discoveryv1.EndpointPort{
			Name:     &name,
			Protocol: &protocol,
			Port:     &port,
		})
	}
	return result
}

type endpointSliceTestEnvironment struct {
	events          []interface{}
	remoteResources []string
	localResources  []string
	link            v1alpha3.Link
}

func (te *endpointSliceTestEnvironment) runEnvironment(watcherQueue workqueue.TypedRateLimitingInterface[any]) (*k8s.API, error) {
	remoteAPI, err := k8s.NewFakeAPI(te.remoteResources...)
	if err != nil {
		return nil, err
	}
	localAPI, err := k8s.NewFakeAPIWithL5dClient(te.localResources...)
	if err != nil {
		return nil, err
	}
	linksAPI := k8s.NewNamespacedAPI(nil, nil, localAPI.L5dClient, "default", "local", k8s.Link)
	remoteAPI.Sync(nil)
	localAPI.Sync(nil)
	linksAPI.Sync(nil)

	watcher := RemoteClusterServiceWatcher{
		link:                    &te.link,
		remoteAPIClient:         remoteAPI,
		localAPIClient:          localAPI,
		linksAPIClient:          linksAPI,
		stopper:                 nil,
		log:                     logging.WithFields(logging.Fields{"cluster": clusterName}),
		eventsQueue:             watcherQueue,
		requeueLimit:            0,
		headlessServicesEnabled: true,
		enableEndpointSlices:    true,
	}

	watcher.gatewayAlive.Store(true)

	for _, ev := range te.events {
		watcherQueue.Add(ev)
	}

	for range te.events {
		_, event, err := watcher.processNextEvent(context.Background())
		if err != nil {
			return nil, fmt.Errorf("error processing event %v: %w", event, err)
		}
	}

	localAPI.Sync(nil)
	remoteAPI.Sync(nil)

	return localAPI, nil
}

func TestEndpointSliceHelperFunctions(t *testing.T) {
	t.Run("getEndpointSliceServiceID extracts service name from labels", func(t *testing.T) {
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-abc123",
				Namespace: "test-ns",
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "my-service",
				},
			},
		}

		ns, name, err := getEndpointSliceServiceID(es)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ns != "test-ns" {
			t.Errorf("expected namespace 'test-ns', got '%s'", ns)
		}
		if name != "my-service" {
			t.Errorf("expected name 'my-service', got '%s'", name)
		}
	})

	t.Run("getEndpointSliceServiceID extracts service name from ownerReferences", func(t *testing.T) {
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-abc123",
				Namespace: "test-ns",
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "Service",
						Name: "owner-service",
					},
				},
			},
		}

		ns, name, err := getEndpointSliceServiceID(es)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ns != "test-ns" {
			t.Errorf("expected namespace 'test-ns', got '%s'", ns)
		}
		if name != "owner-service" {
			t.Errorf("expected name 'owner-service', got '%s'", name)
		}
	})

	t.Run("getEndpointSliceServiceID returns error when no service reference", func(t *testing.T) {
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "orphan-slice",
				Namespace: "test-ns",
			},
		}

		_, _, err := getEndpointSliceServiceID(es)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("isHeadlessEndpointSlice returns true for headless", func(t *testing.T) {
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					corev1.IsHeadlessService: "",
				},
			},
		}

		result := isHeadlessEndpointSlice(es, logging.NewEntry(logging.New()))
		if !result {
			t.Error("expected true for headless EndpointSlice")
		}
	})

	t.Run("isHeadlessEndpointSlice returns false for non-headless", func(t *testing.T) {
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{},
			},
		}

		result := isHeadlessEndpointSlice(es, logging.NewEntry(logging.New()))
		if result {
			t.Error("expected false for non-headless EndpointSlice")
		}
	})

	t.Run("shouldExportAsHeadlessServiceFromSlice returns true with hostname", func(t *testing.T) {
		hostname := "pod-0"
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "test-service",
				},
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Hostname: &hostname,
				},
			},
		}

		result := shouldExportAsHeadlessServiceFromSlice(es, logging.NewEntry(logging.New()))
		if !result {
			t.Error("expected true when hostname is present")
		}
	})

	t.Run("shouldExportAsHeadlessServiceFromSlice returns false without hostname", func(t *testing.T) {
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "test-service",
				},
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses: []string{"10.0.0.1"},
				},
			},
		}

		result := shouldExportAsHeadlessServiceFromSlice(es, logging.NewEntry(logging.New()))
		if result {
			t.Error("expected false when no hostname is present")
		}
	})
}

func TestEndpointSliceEmptinessCheck(t *testing.T) {
	t.Run("isEmptyEndpointSlice returns true for empty endpoints", func(t *testing.T) {
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-slice",
				Namespace: "test-ns",
			},
			Endpoints: []discoveryv1.Endpoint{},
		}

		watcher := &RemoteClusterServiceWatcher{
			log: logging.WithFields(logging.Fields{"test": "true"}),
		}

		if !watcher.isEmptyEndpointSlice(es) {
			t.Error("expected empty EndpointSlice to be detected as empty")
		}
	})

	t.Run("isEmptyEndpointSlice returns true when all endpoints not ready", func(t *testing.T) {
		ready := false
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-slice",
				Namespace: "test-ns",
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses:  []string{"10.0.0.1"},
					Conditions: discoveryv1.EndpointConditions{Ready: &ready},
				},
			},
		}

		watcher := &RemoteClusterServiceWatcher{
			log: logging.WithFields(logging.Fields{"test": "true"}),
		}

		if !watcher.isEmptyEndpointSlice(es) {
			t.Error("expected EndpointSlice with no ready endpoints to be detected as empty")
		}
	})

	t.Run("isEmptyEndpointSlice returns false when endpoint is ready", func(t *testing.T) {
		ready := true
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-slice",
				Namespace: "test-ns",
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses:  []string{"10.0.0.1"},
					Conditions: discoveryv1.EndpointConditions{Ready: &ready},
				},
			},
		}

		watcher := &RemoteClusterServiceWatcher{
			log: logging.WithFields(logging.Fields{"test": "true"}),
		}

		if watcher.isEmptyEndpointSlice(es) {
			t.Error("expected EndpointSlice with ready endpoint to not be detected as empty")
		}
	})
}

func TestEndpointSliceHeadlessServiceMirroring(t *testing.T) {
	servicePorts := []corev1.ServicePort{
		{Name: "port1", Protocol: "TCP", Port: 555},
		{Name: "port2", Protocol: "TCP", Port: 666},
	}
	esPorts := endpointSlicePorts(servicePorts)

	createHeadlessServiceWithES := &endpointSliceTestEnvironment{
		events: []interface{}{
			&RemoteServiceExported{
				service: remoteHeadlessService("service-one", "ns1", "111", map[string]string{
					consts.DefaultExportedServiceSelector: "true",
				}, servicePorts),
			},
			&OnAddEndpointSliceCalled{
				es: remoteHeadlessEndpointSlice("service-one-abc", "service-one", "ns1", "112", "192.0.0.1", esPorts),
			},
		},
		remoteResources: []string{
			asYaml(gateway("existing-gateway", "existing-namespace", "222", "192.0.2.129", "gateway", 889, "gateway-identity", 123456, "/probe1", "120s")),
			asYaml(remoteHeadlessService("service-one", "ns1", "111", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, servicePorts)),
			asYaml(remoteHeadlessEndpointSlice("service-one-abc", "service-one", "ns1", "112", "192.0.0.1", esPorts)),
		},
		localResources: []string{
			asYaml(namespace("ns1")),
		},
		link: v1alpha3.Link{
			Spec: v1alpha3.LinkSpec{
				TargetClusterName:       clusterName,
				TargetClusterDomain:     clusterDomain,
				GatewayIdentity:         "gateway-identity",
				GatewayAddress:          "192.0.2.129",
				GatewayPort:             "889",
				ProbeSpec:               defaultProbeSpec,
				Selector:                defaultSelector,
				RemoteDiscoverySelector: defaultRemoteDiscoverySelector,
			},
		},
	}

	t.Run("create headless service from EndpointSlice", func(t *testing.T) {
		queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())
		localAPI, err := createHeadlessServiceWithES.runEnvironment(queue)
		if err != nil {
			t.Fatalf("error running environment: %v", err)
		}

		// Check that mirror service was created
		mirrorSvc, err := localAPI.Client.CoreV1().Services("ns1").Get(context.Background(), "service-one-remote", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get mirror service: %v", err)
		}
		if mirrorSvc.Spec.ClusterIP != corev1.ClusterIPNone {
			t.Errorf("expected headless mirror service, got ClusterIP=%s", mirrorSvc.Spec.ClusterIP)
		}
	})
}

func TestEndpointSliceUpdateEvent(t *testing.T) {
	servicePorts := []corev1.ServicePort{
		{Name: "port1", Protocol: "TCP", Port: 555},
		{Name: "port2", Protocol: "TCP", Port: 666},
	}
	esPorts := endpointSlicePorts(servicePorts)

	updateHeadlessServiceWithES := &endpointSliceTestEnvironment{
		events: []interface{}{
			&OnUpdateEndpointSliceCalled{
				es: remoteHeadlessEndpointSliceUpdate("service-one-abc", "service-one", "ns1", "113", "192.0.0.1", esPorts),
			},
		},
		remoteResources: []string{
			asYaml(gateway("gateway", "gateway-ns", "222", "192.0.2.127", "mc-gateway", 888, "", defaultProbePort, defaultProbePath, defaultProbePeriod)),
			asYaml(remoteHeadlessService("service-one", "ns1", "111", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, servicePorts)),
			asYaml(remoteHeadlessEndpointSliceUpdate("service-one-abc", "service-one", "ns1", "113", "192.0.0.1", esPorts)),
		},
		localResources: []string{
			asYaml(namespace("ns1")),
			asYaml(headlessMirrorService("service-one-remote", "ns1", "111", nil, servicePorts)),
			asYaml(endpointMirrorService("pod-0", "service-one-remote", "ns1", "112", nil, servicePorts)),
			asYaml(headlessMirrorEndpoints("service-one-remote", "ns1", nil, "gateway-identity", nil)),
			asYaml(endpointMirrorEndpoints("service-one-remote", "ns1", nil, "pod-0", "192.0.2.127", "gateway-identity", nil)),
		},
		link: v1alpha3.Link{
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
	}

	t.Run("update headless service from EndpointSlice with new host", func(t *testing.T) {
		queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())
		localAPI, err := updateHeadlessServiceWithES.runEnvironment(queue)
		if err != nil {
			t.Fatalf("error running environment: %v", err)
		}

		// Check that new endpoint mirror service was created for pod-1
		_, err = localAPI.Client.CoreV1().Services("ns1").Get(context.Background(), "pod-1-remote", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get endpoint mirror service for pod-1: %v", err)
		}
	})
}

// Helper to create a non-headless EndpointSlice for testing
func remoteEndpointSlice(name, serviceName, namespace, resourceVersion string, addresses []string, ports []discoveryv1.EndpointPort) *discoveryv1.EndpointSlice {
	ready := true
	endpoints := make([]discoveryv1.Endpoint, 0, len(addresses))
	for i, addr := range addresses {
		endpoints = append(endpoints, discoveryv1.Endpoint{
			Addresses:  []string{addr},
			Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			TargetRef: &corev1.ObjectReference{
				Kind:      "Pod",
				Name:      "pod-" + string(rune('0'+i)),
				Namespace: namespace,
			},
		})
	}
	return &discoveryv1.EndpointSlice{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EndpointSlice",
			APIVersion: "discovery.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
			Labels: map[string]string{
				discoveryv1.LabelServiceName:          serviceName,
				consts.DefaultExportedServiceSelector: "true",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   endpoints,
		Ports:       ports,
	}
}

// Helper to create an empty EndpointSlice
func emptyEndpointSlice(name, serviceName, namespace, resourceVersion string, ports []discoveryv1.EndpointPort) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EndpointSlice",
			APIVersion: "discovery.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
			Labels: map[string]string{
				discoveryv1.LabelServiceName:          serviceName,
				consts.DefaultExportedServiceSelector: "true",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints:   []discoveryv1.Endpoint{},
		Ports:       ports,
	}
}

func TestIsEmptyServiceES(t *testing.T) {
	servicePorts := []corev1.ServicePort{
		{Name: "port1", Protocol: "TCP", Port: 8080},
	}
	esPorts := endpointSlicePorts(servicePorts)

	t.Run("returns true when no EndpointSlices exist", func(t *testing.T) {
		remoteAPI, err := k8s.NewFakeAPI(
			asYaml(remoteService("test-svc", "test-ns", "111", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, servicePorts)),
		)
		if err != nil {
			t.Fatalf("error creating fake API: %v", err)
		}
		remoteAPI.Sync(nil)

		watcher := &RemoteClusterServiceWatcher{
			remoteAPIClient: remoteAPI,
			log:             logging.WithFields(logging.Fields{"test": "true"}),
		}

		svc, _ := remoteAPI.Svc().Lister().Services("test-ns").Get("test-svc")
		empty, err := watcher.isEmptyServiceES(svc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !empty {
			t.Error("expected service with no EndpointSlices to be empty")
		}
	})

	t.Run("returns true when EndpointSlice has no endpoints", func(t *testing.T) {
		remoteAPI, err := k8s.NewFakeAPI(
			asYaml(remoteService("test-svc", "test-ns", "111", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, servicePorts)),
			asYaml(emptyEndpointSlice("test-svc-abc", "test-svc", "test-ns", "112", esPorts)),
		)
		if err != nil {
			t.Fatalf("error creating fake API: %v", err)
		}
		remoteAPI.Sync(nil)

		watcher := &RemoteClusterServiceWatcher{
			remoteAPIClient: remoteAPI,
			log:             logging.WithFields(logging.Fields{"test": "true"}),
		}

		svc, _ := remoteAPI.Svc().Lister().Services("test-ns").Get("test-svc")
		empty, err := watcher.isEmptyServiceES(svc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !empty {
			t.Error("expected service with empty EndpointSlice to be empty")
		}
	})

	t.Run("returns false when EndpointSlice has ready endpoints", func(t *testing.T) {
		remoteAPI, err := k8s.NewFakeAPI(
			asYaml(remoteService("test-svc", "test-ns", "111", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, servicePorts)),
			asYaml(remoteEndpointSlice("test-svc-abc", "test-svc", "test-ns", "112", []string{"10.0.0.1"}, esPorts)),
		)
		if err != nil {
			t.Fatalf("error creating fake API: %v", err)
		}
		remoteAPI.Sync(nil)

		watcher := &RemoteClusterServiceWatcher{
			remoteAPIClient: remoteAPI,
			log:             logging.WithFields(logging.Fields{"test": "true"}),
		}

		svc, _ := remoteAPI.Svc().Lister().Services("test-ns").Get("test-svc")
		empty, err := watcher.isEmptyServiceES(svc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if empty {
			t.Error("expected service with ready endpoints to not be empty")
		}
	})

	t.Run("returns false when any EndpointSlice has ready endpoints (multiple slices)", func(t *testing.T) {
		remoteAPI, err := k8s.NewFakeAPI(
			asYaml(remoteService("test-svc", "test-ns", "111", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, servicePorts)),
			asYaml(emptyEndpointSlice("test-svc-abc", "test-svc", "test-ns", "112", esPorts)),
			asYaml(remoteEndpointSlice("test-svc-def", "test-svc", "test-ns", "113", []string{"10.0.0.1"}, esPorts)),
		)
		if err != nil {
			t.Fatalf("error creating fake API: %v", err)
		}
		remoteAPI.Sync(nil)

		watcher := &RemoteClusterServiceWatcher{
			remoteAPIClient: remoteAPI,
			log:             logging.WithFields(logging.Fields{"test": "true"}),
		}

		svc, _ := remoteAPI.Svc().Lister().Services("test-ns").Get("test-svc")
		empty, err := watcher.isEmptyServiceES(svc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if empty {
			t.Error("expected service to not be empty when at least one EndpointSlice has endpoints")
		}
	})
}

func TestIsEmptyServiceDispatch(t *testing.T) {
	servicePorts := []corev1.ServicePort{
		{Name: "port1", Protocol: "TCP", Port: 8080},
	}
	esPorts := endpointSlicePorts(servicePorts)

	t.Run("uses EndpointSlice when enableEndpointSlices is true", func(t *testing.T) {
		remoteAPI, err := k8s.NewFakeAPI(
			asYaml(remoteService("test-svc", "test-ns", "111", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, servicePorts)),
			asYaml(remoteEndpointSlice("test-svc-abc", "test-svc", "test-ns", "112", []string{"10.0.0.1"}, esPorts)),
		)
		if err != nil {
			t.Fatalf("error creating fake API: %v", err)
		}
		remoteAPI.Sync(nil)

		watcher := &RemoteClusterServiceWatcher{
			remoteAPIClient:      remoteAPI,
			log:                  logging.WithFields(logging.Fields{"test": "true"}),
			enableEndpointSlices: true,
		}

		svc, _ := remoteAPI.Svc().Lister().Services("test-ns").Get("test-svc")
		empty, err := watcher.isEmptyService(svc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if empty {
			t.Error("expected service to not be empty when using EndpointSlice mode")
		}
	})

	t.Run("uses Endpoints when enableEndpointSlices is false", func(t *testing.T) {
		remoteAPI, err := k8s.NewFakeAPI(
			asYaml(remoteService("test-svc", "test-ns", "111", map[string]string{
				consts.DefaultExportedServiceSelector: "true",
			}, servicePorts)),
			asYaml(endpoints("test-svc", "test-ns", nil, "10.0.0.1", "", nil)),
		)
		if err != nil {
			t.Fatalf("error creating fake API: %v", err)
		}
		remoteAPI.Sync(nil)

		watcher := &RemoteClusterServiceWatcher{
			remoteAPIClient:      remoteAPI,
			log:                  logging.WithFields(logging.Fields{"test": "true"}),
			enableEndpointSlices: false,
		}

		svc, _ := remoteAPI.Svc().Lister().Services("test-ns").Get("test-svc")
		empty, err := watcher.isEmptyService(svc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if empty {
			t.Error("expected service to not be empty when using Endpoints mode")
		}
	})
}

func TestEndpointSliceWithNotReadyEndpoints(t *testing.T) {
	t.Run("isEmptyEndpointSlice returns false when Ready is nil", func(t *testing.T) {
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-slice",
				Namespace: "test-ns",
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses:  []string{"10.0.0.1"},
					Conditions: discoveryv1.EndpointConditions{Ready: nil},
				},
			},
		}

		watcher := &RemoteClusterServiceWatcher{
			log: logging.WithFields(logging.Fields{"test": "true"}),
		}

		// As per the EndpointConditions API, nil means ready
		if watcher.isEmptyEndpointSlice(es) {
			t.Error("expected EndpointSlice with nil Ready condition to not be considered empty")
		}
	})

	t.Run("isEmptyEndpointSlice returns false with mixed ready states", func(t *testing.T) {
		ready := true
		notReady := false
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-slice",
				Namespace: "test-ns",
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses:  []string{"10.0.0.1"},
					Conditions: discoveryv1.EndpointConditions{Ready: &notReady},
				},
				{
					Addresses:  []string{"10.0.0.2"},
					Conditions: discoveryv1.EndpointConditions{Ready: &ready},
				},
			},
		}

		watcher := &RemoteClusterServiceWatcher{
			log: logging.WithFields(logging.Fields{"test": "true"}),
		}

		if watcher.isEmptyEndpointSlice(es) {
			t.Error("expected EndpointSlice with at least one ready endpoint to not be empty")
		}
	})
}

func TestEndpointSliceMultipleAddresses(t *testing.T) {
	t.Run("EndpointSlice with multiple addresses per endpoint", func(t *testing.T) {
		ready := true
		hostname := "pod-0"
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-slice",
				Namespace: "test-ns",
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "test-svc",
				},
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses:  []string{"10.0.0.1", "10.0.0.2"}, // Multiple addresses
					Hostname:   &hostname,
					Conditions: discoveryv1.EndpointConditions{Ready: &ready},
				},
			},
		}

		// Verify shouldExportAsHeadlessServiceFromSlice works with multiple addresses
		result := shouldExportAsHeadlessServiceFromSlice(es, logging.NewEntry(logging.New()))
		if !result {
			t.Error("expected true for EndpointSlice with hostname, regardless of number of addresses")
		}
	})
}

func TestEndpointSliceEmptyHostname(t *testing.T) {
	t.Run("shouldExportAsHeadlessServiceFromSlice returns false for empty string hostname", func(t *testing.T) {
		emptyHostname := ""
		es := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "test-service",
				},
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses: []string{"10.0.0.1"},
					Hostname:  &emptyHostname,
				},
			},
		}

		result := shouldExportAsHeadlessServiceFromSlice(es, logging.NewEntry(logging.New()))
		if result {
			t.Error("expected false when hostname is empty string")
		}
	})
}

// exportedLabels returns the labels of an exported service
func exportedLabels() map[string]string {
	return map[string]string{consts.DefaultExportedServiceSelector: "true"}
}

func endpointSliceTestLink() v1alpha3.Link {
	return v1alpha3.Link{
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
	}
}

// headlessSliceEndpoint describes an endpoint of a headless EndpointSlice
type headlessSliceEndpoint struct {
	hostname string
	ready    *bool
}

func headlessSlice(name, addressType string, port int32, endpoints ...headlessSliceEndpoint) *discoveryv1.EndpointSlice {
	portName, protocol := "http", corev1.ProtocolTCP
	es := &discoveryv1.EndpointSlice{
		TypeMeta: metav1.TypeMeta{Kind: "EndpointSlice", APIVersion: "discovery.k8s.io/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "ns1",
			Labels: map[string]string{
				discoveryv1.LabelServiceName:          "service-one",
				corev1.IsHeadlessService:              "",
				consts.DefaultExportedServiceSelector: "true",
			},
		},
		AddressType: discoveryv1.AddressType(addressType),
		Ports:       []discoveryv1.EndpointPort{{Name: &portName, Protocol: &protocol, Port: &port}},
	}
	for _, e := range endpoints {
		hostname := e.hostname
		es.Endpoints = append(es.Endpoints, discoveryv1.Endpoint{
			Addresses:  []string{"10.0.0.1"},
			Hostname:   &hostname,
			Conditions: discoveryv1.EndpointConditions{Ready: e.ready},
		})
	}
	return es
}

// mirrorEndpointsSummary returns the hostnames and ports of each of the subsets
// of the headless mirror's Endpoints object.
func mirrorEndpointsSummary(t *testing.T, localAPI *k8s.API) ([]string, []int32) {
	t.Helper()
	ep, err := localAPI.Client.CoreV1().Endpoints("ns1").Get(context.Background(), "service-one-remote", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get mirror endpoints: %v", err)
	}
	var hostnames []string
	var ports []int32
	for _, subset := range ep.Subsets {
		for _, addr := range subset.Addresses {
			hostnames = append(hostnames, addr.Hostname)
		}
		for _, port := range subset.Ports {
			ports = append(ports, port.Port)
		}
	}
	return hostnames, ports
}

// TestHeadlessMirrorEndpointsFromSlices verifies the contents of the headless
// mirror's Endpoints object, both when creating and updating it.
func TestHeadlessMirrorEndpointsFromSlices(t *testing.T) {
	servicePorts := []corev1.ServicePort{{Name: "http", Protocol: "TCP", Port: 80}}
	yes, no := true, false
	const targetPort = int32(8080)

	testCases := []struct {
		description string
		slices      []*discoveryv1.EndpointSlice
		hostnames   []string
		ports       []int32
	}{
		{
			description: "copies the ports of the slice and not those of the service",
			slices: []*discoveryv1.EndpointSlice{
				headlessSlice("a", "IPv4", targetPort, headlessSliceEndpoint{"pod-0", &yes}),
			},
			hostnames: []string{"pod-0"},
			ports:     []int32{targetPort},
		},
		{
			description: "does not duplicate hostnames across address families",
			slices: []*discoveryv1.EndpointSlice{
				headlessSlice("v4", "IPv4", targetPort, headlessSliceEndpoint{"pod-0", &yes}, headlessSliceEndpoint{"pod-1", &yes}),
				headlessSlice("v6", "IPv6", targetPort, headlessSliceEndpoint{"pod-0", &yes}, headlessSliceEndpoint{"pod-1", &yes}),
			},
			hostnames: []string{"pod-0", "pod-1"},
			ports:     []int32{targetPort},
		},
		{
			description: "aggregates the endpoints of all the slices",
			slices: []*discoveryv1.EndpointSlice{
				headlessSlice("a", "IPv4", targetPort, headlessSliceEndpoint{"pod-0", &yes}),
				headlessSlice("b", "IPv4", targetPort, headlessSliceEndpoint{"pod-1", &yes}),
			},
			hostnames: []string{"pod-0", "pod-1"},
			ports:     []int32{targetPort, targetPort},
		},
		{
			description: "skips endpoints that are not ready, with nil meaning ready",
			slices: []*discoveryv1.EndpointSlice{
				headlessSlice("a", "IPv4", targetPort, headlessSliceEndpoint{"pod-0", &no}, headlessSliceEndpoint{"pod-1", nil}),
			},
			hostnames: []string{"pod-1"},
			ports:     []int32{targetPort},
		},
	}

	for _, tc := range testCases {
		remoteResources := []string{
			asYaml(remoteHeadlessService("service-one", "ns1", "111", exportedLabels(), servicePorts)),
		}
		for _, slice := range tc.slices {
			remoteResources = append(remoteResources, asYaml(slice))
		}
		mirrorService := asYaml(headlessMirrorService("service-one-remote", "ns1", "111", nil, servicePorts))

		t.Run("create: "+tc.description, func(t *testing.T) {
			env := &endpointSliceTestEnvironment{
				events:          []interface{}{&OnAddEndpointSliceCalled{es: tc.slices[0]}},
				remoteResources: remoteResources,
				localResources:  []string{asYaml(namespace("ns1")), mirrorService},
				link:            endpointSliceTestLink(),
			}
			localAPI, err := env.runEnvironment(workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]()))
			if err != nil {
				t.Fatalf("error running environment: %v", err)
			}
			hostnames, ports := mirrorEndpointsSummary(t, localAPI)
			// Without a TargetRef, the address hostname is the endpoint's hostname.
			if !reflect.DeepEqual(hostnames, tc.hostnames) {
				t.Errorf("expected hostnames %v, got %v", tc.hostnames, hostnames)
			}
			if !reflect.DeepEqual(ports, tc.ports) {
				t.Errorf("expected ports %v, got %v", tc.ports, ports)
			}
		})

		t.Run("update: "+tc.description, func(t *testing.T) {
			env := &endpointSliceTestEnvironment{
				events:          []interface{}{&OnUpdateEndpointSliceCalled{es: tc.slices[0]}},
				remoteResources: remoteResources,
				localResources: []string{
					asYaml(namespace("ns1")),
					mirrorService,
					asYaml(headlessMirrorEndpoints("service-one-remote", "ns1", nil, "gateway-identity", nil)),
				},
				link: endpointSliceTestLink(),
			}
			localAPI, err := env.runEnvironment(workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]()))
			if err != nil {
				t.Fatalf("error running environment: %v", err)
			}
			hostnames, ports := mirrorEndpointsSummary(t, localAPI)
			if !reflect.DeepEqual(hostnames, tc.hostnames) {
				t.Errorf("expected hostnames %v, got %v", tc.hostnames, hostnames)
			}
			if !reflect.DeepEqual(ports, tc.ports) {
				t.Errorf("expected ports %v, got %v", tc.ports, ports)
			}
		})
	}
}

// TestEndpointMirrorCleanupRespectsNamespaces verifies that reconciling a
// headless service doesn't delete the endpoint mirrors of a service with the
// same name in another namespace.
func TestEndpointMirrorCleanupRespectsNamespaces(t *testing.T) {
	servicePorts := []corev1.ServicePort{{Name: "http", Protocol: "TCP", Port: 80}}
	ready := true
	slice := headlessSlice("a", "IPv4", 80, headlessSliceEndpoint{"pod-0", &ready})

	env := &endpointSliceTestEnvironment{
		events: []interface{}{&OnUpdateEndpointSliceCalled{es: slice}},
		remoteResources: []string{
			asYaml(remoteHeadlessService("service-one", "ns1", "111", exportedLabels(), servicePorts)),
			asYaml(slice),
		},
		localResources: []string{
			asYaml(namespace("ns1")),
			asYaml(namespace("ns2")),
			asYaml(headlessMirrorService("service-one-remote", "ns1", "111", nil, servicePorts)),
			asYaml(headlessMirrorEndpoints("service-one-remote", "ns1", nil, "gateway-identity", nil)),
			asYaml(endpointMirrorService("pod-9", "service-one-remote", "ns1", "112", nil, servicePorts)),
			asYaml(headlessMirrorService("service-one-remote", "ns2", "111", nil, servicePorts)),
			asYaml(endpointMirrorService("pod-5", "service-one-remote", "ns2", "112", nil, servicePorts)),
		},
		link: endpointSliceTestLink(),
	}
	localAPI, err := env.runEnvironment(workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]()))
	if err != nil {
		t.Fatalf("error running environment: %v", err)
	}

	if _, err := localAPI.Client.CoreV1().Services("ns2").Get(context.Background(), "pod-5-remote", metav1.GetOptions{}); err != nil {
		t.Errorf("expected endpoint mirror in the other namespace to be kept: %v", err)
	}
	if _, err := localAPI.Client.CoreV1().Services("ns1").Get(context.Background(), "pod-9-remote", metav1.GetOptions{}); err == nil {
		t.Error("expected stale endpoint mirror in the same namespace to be deleted")
	}
}

func TestEndpointSliceDeleted(t *testing.T) {
	servicePorts := []corev1.ServicePort{{Name: "http", Protocol: "TCP", Port: 80}}
	ready := true
	remaining := headlessSlice("a", "IPv4", 80, headlessSliceEndpoint{"pod-0", &ready})
	deleted := headlessSlice("b", "IPv4", 80, headlessSliceEndpoint{"pod-1", &ready})
	service := asYaml(remoteHeadlessService("service-one", "ns1", "111", exportedLabels(), servicePorts))

	t.Run("reconciles the headless mirror against the remaining slices", func(t *testing.T) {
		env := &endpointSliceTestEnvironment{
			events:          []interface{}{&OnDeleteEndpointSliceCalled{es: deleted}},
			remoteResources: []string{service, asYaml(remaining)},
			localResources: []string{
				asYaml(namespace("ns1")),
				asYaml(headlessMirrorService("service-one-remote", "ns1", "111", nil, servicePorts)),
				asYaml(headlessMirrorEndpoints("service-one-remote", "ns1", nil, "gateway-identity", nil)),
				asYaml(endpointMirrorService("pod-0", "service-one-remote", "ns1", "112", nil, servicePorts)),
				asYaml(endpointMirrorService("pod-1", "service-one-remote", "ns1", "112", nil, servicePorts)),
			},
			link: endpointSliceTestLink(),
		}
		localAPI, err := env.runEnvironment(workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]()))
		if err != nil {
			t.Fatalf("error running environment: %v", err)
		}
		if _, err := localAPI.Client.CoreV1().Services("ns1").Get(context.Background(), "pod-1-remote", metav1.GetOptions{}); err == nil {
			t.Error("expected endpoint mirror of the deleted slice to be deleted")
		}
		hostnames, _ := mirrorEndpointsSummary(t, localAPI)
		if !reflect.DeepEqual(hostnames, []string{"pod-0"}) {
			t.Errorf("expected only pod-0 in the mirror endpoints, got %v", hostnames)
		}
	})

	t.Run("empties the mirror of a non-headless service when no ready endpoints remain", func(t *testing.T) {
		deletedSlice := remoteEndpointSlice("svc-b", "svc", "ns1", "3", []string{"10.0.0.1"}, endpointSlicePorts(servicePorts))
		env := &endpointSliceTestEnvironment{
			events: []interface{}{&OnDeleteEndpointSliceCalled{es: deletedSlice}},
			remoteResources: []string{
				asYaml(remoteService("svc", "ns1", "1", exportedLabels(), servicePorts)),
				asYaml(emptyEndpointSlice("svc-a", "svc", "ns1", "2", endpointSlicePorts(servicePorts))),
			},
			localResources: []string{
				asYaml(namespace("ns1")),
				asYaml(mirrorService("svc-remote", "ns1", "1", nil, servicePorts)),
				asYaml(endpoints("svc-remote", "ns1", nil, "192.0.2.127", "gateway-identity", []corev1.EndpointPort{{Name: "http", Protocol: "TCP", Port: 80}})),
			},
			link: endpointSliceTestLink(),
		}
		localAPI, err := env.runEnvironment(workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]()))
		if err != nil {
			t.Fatalf("error running environment: %v", err)
		}
		ep, err := localAPI.Client.CoreV1().Endpoints("ns1").Get(context.Background(), "svc-remote", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get mirror endpoints: %v", err)
		}
		if len(ep.Subsets) != 0 {
			t.Errorf("expected mirror endpoints to be emptied, got %v", ep.Subsets)
		}
	})

	t.Run("does nothing when the exported service is gone", func(t *testing.T) {
		env := &endpointSliceTestEnvironment{
			events:         []interface{}{&OnDeleteEndpointSliceCalled{es: deleted}},
			localResources: []string{asYaml(namespace("ns1"))},
			link:           endpointSliceTestLink(),
		}
		if _, err := env.runEnvironment(workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())); err != nil {
			t.Fatalf("error running environment: %v", err)
		}
	})

	t.Run("does not create a mirror when there is none", func(t *testing.T) {
		env := &endpointSliceTestEnvironment{
			events:          []interface{}{&OnDeleteEndpointSliceCalled{es: deleted}},
			remoteResources: []string{service},
			localResources:  []string{asYaml(namespace("ns1"))},
			link:            endpointSliceTestLink(),
		}
		localAPI, err := env.runEnvironment(workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]()))
		if err != nil {
			t.Fatalf("error running environment: %v", err)
		}
		if _, err := localAPI.Client.CoreV1().Services("ns1").Get(context.Background(), "service-one-remote", metav1.GetOptions{}); err == nil {
			t.Error("expected no mirror service to be created")
		}
	})
}

func TestOnEndpointSliceInformerDelete(t *testing.T) {
	ready := true
	exported := headlessSlice("a", "IPv4", 80, headlessSliceEndpoint{"pod-0", &ready})
	notExported := exported.DeepCopy()
	delete(notExported.Labels, consts.DefaultExportedServiceSelector)

	testCases := []struct {
		description string
		obj         interface{}
		queued      int
	}{
		{"queues exported slices", exported, 1},
		{"queues exported slices from tombstones", cache.DeletedFinalStateUnknown{Obj: exported}, 1},
		{"ignores slices that are not exported", notExported, 0},
		{"ignores unknown objects", "not-a-slice", 0},
		{"ignores tombstones of unknown objects", cache.DeletedFinalStateUnknown{Obj: "not-a-slice"}, 0},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			link := endpointSliceTestLink()
			watcher := &RemoteClusterServiceWatcher{
				link:        &link,
				log:         logging.WithFields(logging.Fields{"cluster": clusterName}),
				eventsQueue: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]()),
			}
			watcher.onEndpointSliceInformerDelete(tc.obj)
			if queued := watcher.eventsQueue.Len(); queued != tc.queued {
				t.Errorf("expected %d queued events, got %d", tc.queued, queued)
			}
		})
	}
}

// TestAPIResourcesProvideInformers runs the watcher against APIs configured with
// the same resources as the service-mirror's entrypoint, since accessing an
// informer that wasn't configured panics, and the fake APIs used in the other
// tests have all of them configured.
func TestAPIResourcesProvideInformers(t *testing.T) {
	servicePorts := []corev1.ServicePort{{Name: "http", Protocol: "TCP", Port: 80}}
	slice := remoteEndpointSlice("svc-a", "svc", "ns1", "2", []string{"10.0.0.1"}, endpointSlicePorts(servicePorts))
	exportedEndpoints := endpoints("svc", "ns1", exportedLabels(), "10.0.0.1", "", []corev1.EndpointPort{{Name: "http", Protocol: "TCP", Port: 80}})

	for _, tc := range []struct {
		enableEndpointSlices bool
		localMirror          bool
	}{
		{enableEndpointSlices: true, localMirror: false},
		{enableEndpointSlices: true, localMirror: true},
		{enableEndpointSlices: false, localMirror: false},
		{enableEndpointSlices: false, localMirror: true},
	} {
		t.Run(fmt.Sprintf("enableEndpointSlices=%t,localMirror=%t", tc.enableEndpointSlices, tc.localMirror), func(t *testing.T) {
			remoteFake, err := k8s.NewFakeAPI(
				asYaml(remoteService("svc", "ns1", "1", exportedLabels(), servicePorts)),
				asYaml(slice),
				asYaml(exportedEndpoints),
			)
			if err != nil {
				t.Fatal(err)
			}
			localFake, err := k8s.NewFakeAPIWithL5dClient(
				asYaml(namespace("ns1")),
				asYaml(mirrorService("svc-remote", "ns1", "1", nil, servicePorts)),
				asYaml(endpoints("svc-remote", "ns1", nil, "192.0.2.127", "gateway-identity", []corev1.EndpointPort{{Name: "http", Protocol: "TCP", Port: 80}})),
			)
			if err != nil {
				t.Fatal(err)
			}

			localAPI := k8s.NewClusterScopedAPI(localFake.Client, nil, nil, "local", LocalAPIResources(tc.enableEndpointSlices, tc.localMirror)...)
			remoteAPI := localAPI
			if !tc.localMirror {
				remoteAPI = k8s.NewClusterScopedAPI(remoteFake.Client, nil, nil, "remote", RemoteAPIResources(tc.enableEndpointSlices)...)
			}
			localAPI.Sync(nil)
			remoteAPI.Sync(nil)

			link := endpointSliceTestLink()
			watcher := &RemoteClusterServiceWatcher{
				link:                 &link,
				remoteAPIClient:      remoteAPI,
				localAPIClient:       localAPI,
				log:                  logging.WithFields(logging.Fields{"cluster": clusterName}),
				eventsQueue:          workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]()),
				enableEndpointSlices: tc.enableEndpointSlices,
			}
			watcher.gatewayAlive.Store(true)

			// Only reaching the end matters here: a missing informer panics.
			ctx := context.Background()
			if tc.enableEndpointSlices {
				_ = watcher.handleCreateOrUpdateEndpointSlice(ctx, slice)
				_ = watcher.handleDeleteEndpointSlice(ctx, slice)
			} else {
				_ = watcher.handleCreateOrUpdateEndpoints(ctx, exportedEndpoints)
			}
			_ = watcher.repairEndpoints(ctx)
			_ = watcher.cleanupMirroredResources(ctx)
		})
	}
}
