package watcher

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/client-go/tools/cache"

	"github.com/linkerd/linkerd2/controller/k8s"

	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIPWatcher(t *testing.T) {
	for _, tt := range []struct {
		serviceType                      string
		k8sConfigs                       []string
		host                             string
		port                             Port
		expectedAddresses                []string
		expectedNoEndpoints              bool
		expectedNoEndpointsServiceExists bool
		expectedError                    bool
	}{
		{
			serviceType: "local services",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 192.168.210.92
  ports:
  - port: 8989`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: name1
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.0.12
    targetRef:
      kind: Pod
      name: name1-1
      namespace: ns
  - ip: 172.17.0.19
    targetRef:
      kind: Pod
      name: name1-2
      namespace: ns
  - ip: 172.17.0.20
    targetRef:
      kind: Pod
      name: name1-3
      namespace: ns
  - ip: 172.17.0.21
  ports:
  - port: 8989`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.12`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-2
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.19`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-3
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.20`,
			},
			host: "192.168.210.92",
			port: 8989,
			expectedAddresses: []string{
				"172.17.0.12:8989",
				"172.17.0.19:8989",
				"172.17.0.20:8989",
				"172.17.0.21:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			// Test for the issue described in linkerd/linkerd2#1405.
			serviceType: "local NodePort service with unnamed port",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: NodePort
  clusterIP: 192.168.210.92
  ports:
  - port: 8989
    targetPort: port1`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: name1
  namespace: ns
subsets:
- addresses:
  - ip: 10.233.66.239
    targetRef:
      kind: Pod
      name: name1-f748fb6b4-hpwpw
      namespace: ns
  - ip: 10.233.88.244
    targetRef:
      kind: Pod
      name: name1-f748fb6b4-6vcmw
      namespace: ns
  ports:
  - port: 8990
    protocol: TCP`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-f748fb6b4-hpwpw
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  podIp: 10.233.66.239
  phase: Running`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-f748fb6b4-6vcmw
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  podIp: 10.233.88.244
  phase: Running`,
			},
			host: "192.168.210.92",
			port: 8989,
			expectedAddresses: []string{
				"10.233.66.239:8990",
				"10.233.88.244:8990",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			// Test for the issue described in linkerd/linkerd2#1853.
			serviceType: "local service with named target port and differently-named service port",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: world
  namespace: ns
spec:
  clusterIP: 192.168.210.92
  type: ClusterIP
  ports:
    - name: app
      port: 7778
      targetPort: http`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: world
  namespace: ns
subsets:
- addresses:
  - ip: 10.1.30.135
    targetRef:
      kind: Pod
      name: world-575bf846b4-tp4hw
      namespace: ns
  ports:
  - name: app
    port: 7779
    protocol: TCP`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: world-575bf846b4-tp4hw
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  podIp: 10.1.30.135
  phase: Running`,
			},
			host: "192.168.210.92",
			port: 7778,
			expectedAddresses: []string{
				"10.1.30.135:7779",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "local services with missing pods",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 192.168.210.92
  ports:
  - port: 8989`,
				`
apiVersion: v1
kind: Endpoints
metadata:
  name: name1
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.0.23
    targetRef:
      kind: Pod
      name: name1-1
      namespace: ns
  - ip: 172.17.0.24
    targetRef:
      kind: Pod
      name: name1-2
      namespace: ns
  - ip: 172.17.0.25
    targetRef:
      kind: Pod
      name: name1-3
      namespace: ns
  ports:
  - port: 8989`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-3
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.25`,
			},
			host: "192.168.210.92",
			port: 8989,
			expectedAddresses: []string{
				"172.17.0.25:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "local services with no endpoints",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name2
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 192.168.210.92
  ports:
  - port: 7979`,
			},
			host:                             "192.168.210.92",
			port:                             7979,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              true,
			expectedNoEndpointsServiceExists: true,
			expectedError:                    false,
		},
		{
			serviceType: "external name services",
			k8sConfigs: []string{`
apiVersion: v1
kind: Service
metadata:
  name: name3
  namespace: ns
spec:
  type: ExternalName
  clusterIP: 192.168.210.92
  externalName: foo`,
			},
			host:                             "192.168.210.92",
			port:                             6969,
			expectedAddresses:                []string{},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    true,
		},
		{
			serviceType:                      "services that do not yet exist",
			k8sConfigs:                       []string{},
			host:                             "192.168.210.92",
			port:                             5959,
			expectedAddresses:                []string{"192.168.210.92:5959"},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "pod ip",
			k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.12`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-2
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.19`,
				`
apiVersion: v1
kind: Pod
metadata:
  name: name1-3
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.20`,
			},
			host: "172.17.0.12",
			port: 8989,
			expectedAddresses: []string{
				"172.17.0.12:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
		{
			serviceType: "pod with hostNetwork",
			k8sConfigs: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
spec:
  hostNetwork: true
status:
  phase: Running
  podIP: 172.17.0.12`,
			},
			host: "172.17.0.12",
			port: 8989,
			expectedAddresses: []string{
				"172.17.0.12:8989",
			},
			expectedNoEndpoints:              false,
			expectedNoEndpointsServiceExists: false,
			expectedError:                    false,
		},
	} {
		tt := tt // pin
		t.Run("subscribes listener to "+tt.serviceType, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			endpoints := NewEndpointsWatcher(k8sAPI, logging.WithField("test", t.Name()), false)
			watcher := NewIPWatcher(k8sAPI, endpoints, logging.WithField("test", t.Name()))

			k8sAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.host, tt.port, listener)
			if tt.expectedError && err == nil {
				t.Fatal("Expected error but was ok")
			}
			if !tt.expectedError && err != nil {
				t.Fatalf("Expected no error, got [%s]", err)
			}

			listener.ExpectAdded(tt.expectedAddresses, t)

			if listener.endpointsAreNotCalled() != tt.expectedNoEndpoints {
				t.Fatalf("Expected noEndpointsCalled to be [%t], got [%t]",
					tt.expectedNoEndpoints, listener.endpointsAreNotCalled())
			}

			if listener.endpointsDoNotExist() != tt.expectedNoEndpointsServiceExists {
				t.Fatalf("Expected noEndpointsExist to be [%t], got [%t]",
					tt.expectedNoEndpointsServiceExists, listener.endpointsDoNotExist())
			}
		})
	}
}

func TestIPWatcherDeletion(t *testing.T) {

	podK8sConfig := []string{
		`
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.12`,
	}

	serviceK8sConfig := []string{
		`
apiVersion: v1
kind: Service
metadata:
  name: name1
  namespace: ns
spec:
  type: LoadBalancer
  clusterIP: 192.168.210.92
  ports:
  - port: 8989`,
		`
apiVersion: v1
kind: Endpoints
metadata:
  name: name1
  namespace: ns
subsets:
- addresses:
  - ip: 172.17.0.12
    targetRef:
      kind: Pod
      name: name1-1
      namespace: ns
`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.12`,
	}

	for _, tt := range []struct {
		description    string
		k8sConfigs     []string
		host           string
		port           Port
		objectToDelete interface{}
		deletingPod    bool
	}{
		{
			description:    "can delete addresses",
			k8sConfigs:     podK8sConfig,
			host:           "172.17.0.12",
			port:           8989,
			objectToDelete: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "name1-1", Namespace: "ns"}, Status: corev1.PodStatus{PodIP: "172.17.0.12"}},
			deletingPod:    true,
		},
		{
			description:    "can delete addresses wrapped in a DeletedFinalStateUnknown",
			k8sConfigs:     podK8sConfig,
			host:           "172.17.0.12",
			port:           8989,
			objectToDelete: cache.DeletedFinalStateUnknown{Obj: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "name1-1", Namespace: "ns"}, Status: corev1.PodStatus{PodIP: "172.17.0.12"}}},
			deletingPod:    true,
		},
		{
			description:    "can delete services",
			k8sConfigs:     serviceK8sConfig,
			host:           "192.168.210.92",
			port:           8989,
			objectToDelete: &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "name1", Namespace: "ns"}, Spec: corev1.ServiceSpec{ClusterIP: "192.168.210.92"}},
		},
		{
			description:    "can delete services wrapped in a DeletedFinalStateUnknown",
			k8sConfigs:     serviceK8sConfig,
			host:           "192.168.210.92",
			port:           8989,
			objectToDelete: cache.DeletedFinalStateUnknown{Obj: &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "name1", Namespace: "ns"}, Spec: corev1.ServiceSpec{ClusterIP: "192.168.210.92"}}},
		},
	} {

		tt := tt // pin
		t.Run("subscribes listener to "+tt.description, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			endpoints := NewEndpointsWatcher(k8sAPI, logging.WithField("test", t.Name()), false)
			watcher := NewIPWatcher(k8sAPI, endpoints, logging.WithField("test", t.Name()))

			k8sAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.host, tt.port, listener)
			if err != nil {
				t.Fatal(err)
			}

			if tt.deletingPod {
				watcher.deletePod(tt.objectToDelete)
			} else {
				watcher.deleteService(tt.objectToDelete)
			}

			if !listener.endpointsAreNotCalled() {
				t.Fatal("Expected NoEndpoints to be Called")
			}
		})
	}
}

func TestIPWatcherUpdate(t *testing.T) {
	podK8sConfig := `
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
status:
  phase: Running
  podIP: 172.17.0.12`

	hostNetworkPodConfig := `
apiVersion: v1
kind: Pod
metadata:
  name: name1-1
  namespace: ns
  ownerReferences:
  - kind: ReplicaSet
    name: rs-1
spec:
  hostNetwork: true
status:
  phase: Running
  podIP: 172.17.0.12`

	for _, tt := range []struct {
		description    string
		k8sConfigs     string
		host           string
		port           Port
		objectToUpdate interface{}
	}{
		{
			description: "pod update",
			k8sConfigs:  podK8sConfig,
			host:        "172.17.0.12",
			port:        12345,
			objectToUpdate: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "name1-1", Namespace: "ns"},
				Status:     corev1.PodStatus{PodIP: "172.17.0.12"},
			},
		},
		{
			description: "host network pod update",
			k8sConfigs:  hostNetworkPodConfig,
			host:        "172.17.0.12",
			port:        12345,
			objectToUpdate: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "name1-1", Namespace: "ns"},
				Spec:       corev1.PodSpec{HostNetwork: true},
				Status:     corev1.PodStatus{PodIP: "172.17.0.12"},
			},
		},
	} {
		tt := tt // pin

		t.Run("ip watch for "+tt.description, func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(tt.k8sConfigs)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			endpoints := NewEndpointsWatcher(k8sAPI, logging.WithField("test", t.Name()), false)
			watcher := NewIPWatcher(k8sAPI, endpoints, logging.WithField("test", t.Name()))

			k8sAPI.Sync(nil)

			listener := newBufferingEndpointListener()

			err = watcher.Subscribe(tt.host, tt.port, listener)
			if err != nil {
				t.Fatal(err)
			}

			watcher.addPod(tt.objectToUpdate)

			if listener.endpointsAreNotCalled() {
				t.Fatal("NoEndpoints was called but should not have been")
			}
		})
	}
}

func TestIpWatcherGetSvcID(t *testing.T) {
	name := "service"
	namespace := "test"
	clusterIP := "10.256.0.1"
	var port uint32 = 1234
	k8sConfigs := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
spec:
  type: ClusterIP
  clusterIP: %s
  ports:
  - port: %d`, name, namespace, clusterIP, port)

	t.Run("get services IDs by IP address", func(t *testing.T) {
		k8sAPI, err := k8s.NewFakeAPI(k8sConfigs)
		if err != nil {
			t.Fatalf("NewFakeAPI returned an error: %s", err)
		}

		endpoints := NewEndpointsWatcher(k8sAPI, logging.WithField("test", t.Name()), false)
		watcher := NewIPWatcher(k8sAPI, endpoints, logging.WithField("test", t.Name()))

		k8sAPI.Sync(nil)

		listener := newBufferingEndpointListener()

		err = watcher.Subscribe(clusterIP, port, listener)
		if err != nil {
			t.Fatal(err)
		}

		svc, err := watcher.GetSvcID(clusterIP)
		if err != nil {
			t.Fatalf("Error getting service: %s", err)
		}
		if svc == nil {
			t.Fatalf("Expected to find service mapped to [%s]", clusterIP)
		}
		if svc.Name != name {
			t.Fatalf("Expected service name to be [%s], but got [%s]", name, svc.Name)
		}
		if svc.Namespace != namespace {
			t.Fatalf("Expected service namespace to be [%s], but got [%s]", namespace, svc.Namespace)
		}

		badClusterIP := "10.256.0.2"
		svc, err = watcher.GetSvcID(badClusterIP)
		if err != nil {
			t.Fatalf("Error getting service: %s", err)
		}
		if svc != nil {
			t.Fatalf("Expected not to find service mapped to [%s]", badClusterIP)
		}
	})
}

func TestIpWatcherGetPod(t *testing.T) {
	podIP := "10.255.0.1"
	hostIP := "172.0.0.1"
	var hostPort uint32 = 12345
	expectedPodName := "hostPortPod1"
	k8sConfigs := []string{`
apiVersion: v1
kind: Pod
metadata:
  name: hostPortPod1
  namespace: ns
spec:
  containers:
  - image: test
    name: hostPortContainer
    ports:
    - containerPort: 12345
      hostIP: 172.0.0.1
      hostPort: 12345
status:
  phase: Running
  podIP: 10.255.0.1
  hostIP: 172.0.0.1`,
		`
apiVersion: v1
kind: Pod
metadata:
  name: pod
  namespace: ns
status:
  phase: Running
  podIP: 10.255.0.1`,
	}
	t.Run("get pod by host IP and host port", func(t *testing.T) {
		k8sAPI, err := k8s.NewFakeAPI(k8sConfigs...)
		if err != nil {
			t.Fatalf("failed to create new fake API: %s", err)
		}
		endpoints := NewEndpointsWatcher(k8sAPI, logging.WithField("test", t.Name()), false)
		watcher := NewIPWatcher(k8sAPI, endpoints, logging.WithField("test", t.Name()))
		k8sAPI.Sync(nil)
		// Get host IP pod that is mapped to the port `hostPort`
		pod, err := watcher.GetPod(hostIP, hostPort)
		if err != nil {
			t.Fatalf("failed to get pod: %s", err)
		}
		if pod == nil {
			t.Fatalf("failed to find pod mapped to %s:%d", hostIP, hostPort)
		}
		if pod.Name != expectedPodName {
			t.Fatalf("expected pod name to be %s, but got %s", expectedPodName, pod.Name)
		}
		// Get host IP pod with unmapped host port
		pod, err = watcher.GetPod(hostIP, 12346)
		if err != nil {
			t.Fatalf("expected no error when getting host IP pod with unmapped host port, but got: %s", err)
		}
		if pod != nil {
			t.Fatal("expected no pod to be found with unmapped host port")
		}
		// Get pod IP pod and expect an error
		pod, err = watcher.GetPod(podIP, 12346)
		if err == nil {
			t.Fatal("expected error when getting by pod IP and unmapped host port, but got none")
		}
		if !strings.Contains(err.Error(), "pods with conflicting pod IP") {
			t.Fatalf("expected error to be pod IP address conflict, but got: %s", err)
		}
	})
}
