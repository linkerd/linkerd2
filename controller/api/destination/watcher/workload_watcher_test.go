package watcher

import (
	"strings"
	"sync"
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// recordingWorkloadListener records the pod backing each update it receives.
// A "" entry means the update carried no pod (identity cleared).
type recordingWorkloadListener struct {
	mu       sync.Mutex
	podNames []string
}

func (l *recordingWorkloadListener) Update(addr *Address) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	name := ""
	if addr.Pod != nil {
		name = addr.Pod.Name
	}
	l.podNames = append(l.podNames, name)
	return nil
}

func (l *recordingWorkloadListener) lastPodName() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.podNames) == 0 {
		return "<no updates>"
	}
	return l.podNames[len(l.podNames)-1]
}

// TestWorkloadPublisherKeepsLivePodOnFailedTwinUpdate covers
// https://github.com/linkerd/linkerd2/issues/15773: a Failed pod object that
// shares a live pod's IP:port must not clear the live pod's endpoint
// metadata (identity) from the workload publisher.
func TestWorkloadPublisherKeepsLivePodOnFailedTwinUpdate(t *testing.T) {
	const podIP = "10.255.0.1"
	const port = 8080
	k8sConfigs := []string{`
apiVersion: v1
kind: Pod
metadata:
  name: live-pod
  namespace: ns
  uid: live-uid
spec:
  containers:
  - image: test
    name: web
    ports:
    - containerPort: 8080
status:
  phase: Running
  podIP: 10.255.0.1
  podIPs:
  - ip: 10.255.0.1`,
	}
	k8sAPI, err := k8s.NewFakeAPI(k8sConfigs...)
	if err != nil {
		t.Fatalf("failed to create new fake API: %s", err)
	}
	metadataAPI, err := k8s.NewFakeMetadataAPI(nil)
	if err != nil {
		t.Fatalf("failed to create new fake metadata API: %s", err)
	}
	if err := InitializeIndexers(k8sAPI); err != nil {
		t.Fatalf("initializeIndexers returned an error: %s", err)
	}
	k8sAPI.Sync(nil)
	ww, err := NewWorkloadWatcher(k8sAPI, metadataAPI, log.WithFields(log.Fields{
		"component": "pod-watcher",
	}), false, nil)
	if err != nil {
		t.Fatalf("failed to create workload watcher: %s", err)
	}

	wp, err := ww.getOrNewWorkloadPublisher(nil, "", podIP, port)
	if err != nil {
		t.Fatalf("failed to get workload publisher: %s", err)
	}
	if wp.addr.Pod == nil || wp.addr.Pod.Name != "live-pod" {
		t.Fatalf("expected publisher to be backed by live-pod, got %+v", wp.addr.Pod)
	}

	listener := &recordingWorkloadListener{}
	if err := wp.subscribe(listener); err != nil {
		t.Fatalf("failed to subscribe listener: %s", err)
	}
	if got := listener.lastPodName(); got != "live-pod" {
		t.Fatalf("expected initial update to carry live-pod, got %q", got)
	}

	// A Failed pod object sharing the live pod's IP:port must not clear it.
	failedTwin := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "failed-twin",
			Namespace: "ns",
			UID:       "failed-uid",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "web",
					Ports: []corev1.ContainerPort{{ContainerPort: port}},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			PodIP: podIP,
			PodIPs: []corev1.PodIP{
				{IP: podIP},
			},
		},
	}
	ww.submitPodUpdate(failedTwin, false)
	if wp.addr.Pod == nil || wp.addr.Pod.Name != "live-pod" {
		t.Fatalf("Failed twin cleared the live pod: publisher now backed by %+v", wp.addr.Pod)
	}
	if got := listener.lastPodName(); got != "live-pod" {
		t.Fatalf("expected last listener update to still carry live-pod, got %q", got)
	}

	// The stale twin's delete event must not clear the live pod either:
	// delete events used to arrive as a nil pod, which cleared whichever
	// pod backed the address. This is the case reported against #15774's
	// test on this branch.
	ww.submitPodUpdate(failedTwin, true)
	if wp.addr.Pod == nil || wp.addr.Pod.Name != "live-pod" {
		t.Fatalf("Stale twin delete cleared the live pod: publisher now backed by %+v", wp.addr.Pod)
	}
	if got := listener.lastPodName(); got != "live-pod" {
		t.Fatalf("expected last listener update to still carry live-pod, got %q", got)
	}

	// Control: when the live pod itself stops running, the publisher must
	// still clear it.
	failedLive := wp.addr.Pod.DeepCopy()
	failedLive.Status.Phase = corev1.PodFailed
	ww.submitPodUpdate(failedLive, false)
	if wp.addr.Pod != nil {
		t.Fatalf("expected publisher to clear the failed live pod, got %+v", wp.addr.Pod)
	}
	if got := listener.lastPodName(); got != "" {
		t.Fatalf("expected last listener update to carry no pod, got %q", got)
	}

	// Control: a Running live pod rebinds, and its delete event clears it.
	runningLive := failedLive.DeepCopy()
	runningLive.Status.Phase = corev1.PodRunning
	ww.submitPodUpdate(runningLive, false)
	if wp.addr.Pod == nil || wp.addr.Pod.Name != "live-pod" {
		t.Fatalf("expected publisher to rebind live-pod, got %+v", wp.addr.Pod)
	}
	ww.submitPodUpdate(runningLive, true)
	if wp.addr.Pod != nil {
		t.Fatalf("expected publisher to clear the deleted live pod, got %+v", wp.addr.Pod)
	}
}
