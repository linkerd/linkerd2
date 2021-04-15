package watcher

import (
	"fmt"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"

	logging "github.com/sirupsen/logrus"
)

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

		watcher := NewIPWatcher(k8sAPI, logging.WithField("test", t.Name()))

		k8sAPI.Sync(nil)

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
	var hostPort1 uint32 = 12345
	var hostPort2 uint32 = 12346
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
    name: hostPortContainer1
    ports:
    - containerPort: 12345
      hostIP: 172.0.0.1
      hostPort: 12345
  - image: test
    name: hostPortContainer2
    ports:
    - containerPort: 12346
      hostIP: 172.0.0.1
      hostPort: 12346
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
		watcher := NewIPWatcher(k8sAPI, logging.WithField("test", t.Name()))
		k8sAPI.Sync(nil)
		// Get host IP pod that is mapped to the port `hostPort1`
		pod, err := watcher.GetPod(hostIP, hostPort1)
		if err != nil {
			t.Fatalf("failed to get pod: %s", err)
		}
		if pod == nil {
			t.Fatalf("failed to find pod mapped to %s:%d", hostIP, hostPort1)
		}
		if pod.Name != expectedPodName {
			t.Fatalf("expected pod name to be %s, but got %s", expectedPodName, pod.Name)
		}
		// Get host IP pod that is mapped to the port `hostPort2`; this tests
		// that the indexer properly adds multiple containers from a single
		// pod.
		pod, err = watcher.GetPod(hostIP, hostPort2)
		if err != nil {
			t.Fatalf("failed to get pod: %s", err)
		}
		if pod == nil {
			t.Fatalf("failed to find pod mapped to %s:%d", hostIP, hostPort2)
		}
		if pod.Name != expectedPodName {
			t.Fatalf("expected pod name to be %s, but got %s", expectedPodName, pod.Name)
		}
		// Get host IP pod with unmapped host port
		pod, err = watcher.GetPod(hostIP, 12347)
		if err != nil {
			t.Fatalf("expected no error when getting host IP pod with unmapped host port, but got: %s", err)
		}
		if pod != nil {
			t.Fatal("expected no pod to be found with unmapped host port")
		}
		// Get pod IP pod and expect an error
		_, err = watcher.GetPod(podIP, 12346)
		if err == nil {
			t.Fatal("expected error when getting by pod IP and unmapped host port, but got none")
		}
		if !strings.Contains(err.Error(), "pods with a conflicting pod network IP") {
			t.Fatalf("expected error to be pod IP address conflict, but got: %s", err)
		}
	})
}
