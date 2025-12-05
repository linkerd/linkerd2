package watcher

import (
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
	log "github.com/sirupsen/logrus"
)

func TestIpWatcherGetPod(t *testing.T) {
	podIP := "10.255.0.1"
	hostIP := "172.0.0.1"
	var hostPort0 uint32 = 22344
	var hostPort1 uint32 = 22345
	var hostPort2 uint32 = 22346
	expectedPodName := "hostPortPod1"
	k8sConfigs := []string{`
apiVersion: v1
kind: Pod
metadata:
  name: hostPortPod1
  namespace: ns
spec:
  initContainers:
  - image: test
    name: hostPortInitContainer
    ports:
    - containerPort: 12344
      hostPort: 22344
  containers:
  - image: test
    name: hostPortContainer1
    restartPolicy: Always
    ports:
    - containerPort: 12345
      hostIP: 172.0.0.1
      hostPort: 22345
  - image: test
    name: hostPortContainer2
    ports:
    - containerPort: 12346
      hostIP: 172.0.0.1
      hostPort: 22346
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

		err = InitializeIndexers(k8sAPI)
		if err != nil {
			t.Fatalf("initializeIndexers returned an error: %s", err)
		}

		k8sAPI.Sync(nil)
		ww := WorkloadWatcher{
			k8sAPI: k8sAPI,
			log: log.WithFields(log.Fields{
				"component": "pod-watcher",
			}),
		}

		// Get host IP pod that is mapped to the port `hostPort0` (in an init container)
		pod, err := ww.getPodByHostIP(hostIP, hostPort0)
		if err != nil {
			t.Fatalf("failed to get pod: %s", err)
		}
		if pod == nil {
			t.Fatalf("failed to find pod mapped to %s:%d", hostIP, hostPort1)
		}
		if pod.Name != expectedPodName {
			t.Fatalf("expected pod name to be %s, but got %s", expectedPodName, pod.Name)
		}

		// Get host IP pod that is mapped to the port `hostPort1`
		pod, err = ww.getPodByHostIP(hostIP, hostPort1)
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
		pod, err = ww.getPodByHostIP(hostIP, hostPort2)
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
		pod, err = ww.getPodByHostIP(hostIP, 12347)
		if err != nil {
			t.Fatalf("expected no error when getting host IP pod with unmapped host port, but got: %s", err)
		}
		if pod != nil {
			t.Fatal("expected no pod to be found with unmapped host port")
		}
		// Get pod IP pod and expect an error
		_, err = ww.getPodByPodIP(podIP, 12346)
		if err == nil {
			t.Fatal("expected error when getting by pod IP and unmapped host port, but got none")
		}
		if !strings.Contains(err.Error(), "pods with a conflicting pod network IP") {
			t.Fatalf("expected error to be pod IP address conflict, but got: %s", err)
		}
	})
}
