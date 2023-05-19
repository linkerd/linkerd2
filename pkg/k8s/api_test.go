package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestGetPodStatus(t *testing.T) {
	scenarios := []struct {
		desc     string
		pod      string
		expected string
	}{
		{
			desc:     "Pod is running",
			expected: "Running",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
`,
		},
		{
			desc:     "Pod's reason is filled",
			expected: "podReason",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Pending
  reason: podReason
`,
		},
		{
			desc:     "Pod waiting is filled",
			expected: "CrashLoopBackOff",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Pending
  reason: podReason
  containerStatuses:
  - state:
      waiting:
        reason: CrashLoopBackOff
`,
		},
		{
			desc:     "Pod is terminated",
			expected: "podTerminated",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Pending
  reason: podReason
  containerStatuses:
  - state:
      terminated:
        reason: podTerminated
        exitCode: 2
`,
		},
		{
			desc:     "Pod terminated with signal",
			expected: "Signal:9",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Pending
  reason: podReason
  containerStatuses:
  - state:
      terminated:
        signal: 9
`,
		},
		{
			desc:     "Pod terminated with exti code",
			expected: "ExitCode:2",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Pending
  reason: podReason
  containerStatuses:
  - state:
      terminated:
        exitCode: 2
`,
		},
		{
			desc:     "Pod has a running container",
			expected: "Running",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
  reason: Completed
  containerStatuses:
  - ready: true
    state:
      running:
        startedAt: 1995-02-10T00:42:42Z
`,
		},
		{
			desc:     "Pod init container terminated with exit code 0",
			expected: "Running",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
  containerStatuses:
  - state:
      running:
        startedAt: 1995-02-10T00:42:42Z
  initContainerStatuses:
  - state:
      terminated:
        exitCode: 0
        reason: Completed
`,
		},
		{
			desc:     "Pod init container terminated with exit code 2",
			expected: "Init:ExitCode:2",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
  containerStatuses:
  - state:
      running:
        startedAt: 1995-02-10T00:42:42Z
  initContainerStatuses:
  - state:
      terminated:
        exitCode: 2
`,
		},
		{
			desc:     "Pod init container terminated with signal",
			expected: "Init:Signal:9",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Running
  containerStatuses:
  - state:
      running:
        startedAt: 1995-02-10T00:42:42Z
  initContainerStatuses:
  - state:
      terminated:
        signal: 9
`,
		},
		{
			desc:     "Pod init container in CrashLooBackOff",
			expected: "Init:CrashLoopBackOff",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Pending
  initContainerStatuses:
  - state:
      terminated:
        exitCode: 2
        reason: CrashLoopBackOff
`,
		},
		{
			desc:     "Pod init container waiting for someReason",
			expected: "Init:someReason",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Pending
  initContainerStatuses:
  - state:
      waiting:
        reason: someReason
`,
		},
		{
			desc:     "Pod init container is waiting on PodInitializing",
			expected: "Init:0/0",
			pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji
  namespace: emojivoto
  labels:
    app: emoji-svc
    linkerd.io/control-plane-ns: linkerd
status:
  phase: Pending
  initContainerStatuses:
  - state:
      waiting:
        reason: PodInitializing
`,
		},
	}
	for _, s := range scenarios {
		obj, err := ToRuntimeObject(s.pod)
		if err != nil {
			t.Fatalf("could not decode yml: %s", err)
		}
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			t.Fatalf("could not convert returned object to pod")
		}

		got := GetPodStatus(*pod)
		if s.expected != got {
			t.Fatalf("Wrong pod status on '%s'. Expected '%s', got '%s'", s.desc, s.expected, got)
		}
	}
}

func TestGetPodsFor(t *testing.T) {

	configs := []string{
		// pod-1
		`apiVersion: v1
kind: Pod
metadata:
  name: pod-1
  namespace: ns
  uid: pod-1
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: ReplicaSet
    name: rs-1
    uid: rs-1
`,
		// rs-1
		`apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: rs-1
  namespace: ns
  uid: rs-1
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: Deployment
    name: deploy-1
    uid: deploy-1
spec:
  selector:
    matchLabels:
      app: foo
`,
		// deploy-1
		`apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-1
  namespace: ns
  uid: deploy-1
spec:
  selector:
    matchLabels:
      app: foo
`,
		// pod-2
		`apiVersion: v1
kind: Pod
metadata:
  name: pod-2
  namespace: ns
  uid: pod-2
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: ReplicaSet
    name: rs-2
    uid: rs-2
`,
		// rs-2
		`apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: rs-2
  namespace: ns
  uid: rs-2
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: Deployment
    name: deploy-2
    uid: deploy-2
spec:
  selector:
    matchLabels:
     app: foo
`,
		// deploy-2
		`apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy-2
  namespace: ns
  uid: deploy-2
spec:
  selector:
    matchLabels:
      app: foo
`}

	k8sClient, err := NewFakeAPI(configs...)
	if err != nil {
		t.Fatalf("Unexpected error %s", err)
	}

	// Both pod-1 and pod-2 have labels which match deploy-1's selector.
	// However, only pod-1 is owned by deploy-1 according to the owner references.
	// Owner references should be considered authoritative to resolve ambiguity
	// when deployments have overlapping seletors.
	pods, err := GetPodsFor(context.Background(), k8sClient, "ns", "deploy/deploy-1")
	if err != nil {
		t.Fatalf("Unexpected error %s", err)
	}

	if len(pods) != 1 {
		for _, p := range pods {
			t.Logf("%s/%s", p.Namespace, p.Name)
		}
		t.Fatalf("Expected 1 pod, got %d", len(pods))
	}
}
