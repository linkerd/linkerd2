package servicemirror

import (
	"context"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/apis/link/v1alpha3"
	"github.com/linkerd/linkerd2/controller/k8s"
	consts "github.com/linkerd/linkerd2/pkg/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
				discoveryv1.LabelServiceName:           serviceName,
				corev1.IsHeadlessService:               "",
				consts.DefaultExportedServiceSelector:  "true",
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
				discoveryv1.LabelServiceName:           serviceName,
				corev1.IsHeadlessService:               "",
				consts.DefaultExportedServiceSelector:  "true",
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
		gatewayAlive:            true,
		headlessServicesEnabled: true,
		enableEndpointSlices:    true,
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
	t.Run("isEmptyEndpointSlice returns true when Ready is nil", func(t *testing.T) {
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

		// When Ready is nil, the endpoint is considered not ready
		if !watcher.isEmptyEndpointSlice(es) {
			t.Error("expected EndpointSlice with nil Ready condition to be considered empty")
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

func TestEndpointSliceNonHeadlessService(t *testing.T) {
	servicePorts := []corev1.ServicePort{
		{Name: "http", Protocol: "TCP", Port: 80},
	}
	esPorts := endpointSlicePorts(servicePorts)

	t.Run("non-headless EndpointSlice triggers emptiness check", func(t *testing.T) {
		// Create an EndpointSlice without headless label
		es := &discoveryv1.EndpointSlice{
			TypeMeta: metav1.TypeMeta{
				Kind:       "EndpointSlice",
				APIVersion: "discovery.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "svc-abc",
				Namespace: "test-ns",
				Labels: map[string]string{
					discoveryv1.LabelServiceName:          "svc",
					consts.DefaultExportedServiceSelector: "true",
					// No corev1.IsHeadlessService label
				},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses:  []string{"10.0.0.1"},
					Conditions: discoveryv1.EndpointConditions{Ready: func() *bool { b := true; return &b }()},
				},
			},
			Ports: esPorts,
		}

		// Verify it's not considered headless
		if isHeadlessEndpointSlice(es, logging.NewEntry(logging.New())) {
			t.Error("expected EndpointSlice without headless label to not be considered headless")
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

func TestWatcherEnableEndpointSlicesField(t *testing.T) {
	t.Run("watcher correctly stores enableEndpointSlices flag", func(t *testing.T) {
		watcherWithES := &RemoteClusterServiceWatcher{
			enableEndpointSlices: true,
			log:                  logging.WithFields(logging.Fields{"test": "true"}),
		}

		watcherWithoutES := &RemoteClusterServiceWatcher{
			enableEndpointSlices: false,
			log:                  logging.WithFields(logging.Fields{"test": "true"}),
		}

		if !watcherWithES.enableEndpointSlices {
			t.Error("expected enableEndpointSlices to be true")
		}

		if watcherWithoutES.enableEndpointSlices {
			t.Error("expected enableEndpointSlices to be false")
		}
	})
}
