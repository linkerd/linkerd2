package k8s

import (
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
