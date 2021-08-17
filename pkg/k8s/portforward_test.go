package k8s

import (
	"context"
	"errors"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewContainerMetricsForward(t *testing.T) {
	// TODO: test successful cases by mocking out `clientset.CoreV1().RESTClient()`
	tests := []struct {
		ns         string
		name       string
		k8sConfigs []string
		err        error
	}{
		{
			"pod-ns",
			"pod-name",
			[]string{`apiVersion: v1
kind: Pod
metadata:
  name: pod-name
  namespace: pod-ns
status:
  phase: Running
spec:
  containers:
  - name: linkerd-proxy
    ports:
    - name: bad-port
      port: 123`,
			},
			errors.New("no linkerd-admin port found for container pod-name/linkerd-proxy"),
		},
	}

	for i, test := range tests {
		test := test // pin
		t.Run(fmt.Sprintf("%d: NewContainerMetricsForward returns expected result", i), func(t *testing.T) {
			k8sClient, err := NewFakeAPI(test.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error %s", err)
			}
			pod, err := k8sClient.CoreV1().Pods(test.ns).Get(context.Background(), test.name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Unexpected error %s", err)
			}
			var container corev1.Container
			for _, c := range pod.Spec.Containers {
				container = c
				break
			}
			_, err = NewContainerMetricsForward(&KubernetesAPI{Interface: k8sClient}, *pod, container, false, ProxyAdminPortName)
			if err != nil || test.err != nil {
				if (err == nil && test.err != nil) ||
					(err != nil && test.err == nil) ||
					(err.Error() != test.err.Error()) {
					t.Fatalf("Unexpected error (Expected: %s, Got: %s)", test.err, err)
				}
			}
		})
	}
}

func TestNewPortForward(t *testing.T) {
	tests := []struct {
		description string
		ns          string
		deployName  string
		k8sConfigs  []string
		err         error
	}{
		{
			"Pod is owned by the specified deployment",
			"ns",
			"deploy",
			[]string{`apiVersion: v1
kind: Pod
metadata:
  name: pod
  namespace: ns
  uid: pod
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: Deployment
    name: rs
    uid: rs
status:
  phase: Running`,
				`apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: rs
  namespace: ns
  uid: rs
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: Deployment
    name: deploy
    uid: deploy
  spec:
    selector:
      matchLabels:
        app: foo
`,
				`apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy
  namespace: ns
  uid: deploy
spec:
  selector:
    matchLabels:
      app: foo
`},
			nil,
		},
		// In the case of overlapping deployments, a pod may match the label
		// selector of more than one deployment but will still be owned by
		// exactly one.
		{
			"Pod's labels match, but is not owned by the deployment",
			"ns",
			"deploy",
			[]string{`apiVersion: v1
kind: Pod
metadata:
  name: pod
  namespace: ns
  uid: pod
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: ReplicaSet
    name: rs
    uid: SOME-OTHER-UID
  status:
    phase: Running`,
				`apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: rs
  namespace: ns
  uid: rs
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: Deployment
    name: deploy
    uid: deploy
  spec:
    selector:
      matchLabels:
        app: foo
`,
				`apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy
  namespace: ns
  uid: deploy
spec:
  selector:
    matchLabels:
      app: foo
`},
			errors.New("no running pods found for deploy"),
		},
		{
			"Pod is owned by the specified deployment but is not running",
			"ns",
			"deploy",
			[]string{`apiVersion: v1
kind: Pod
metadata:
  name: pod
  namespace: ns
  uid: pod
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: ReplicaSet
    name: rs
    uid: rs
  status:
    phase: Stopped`,
				`apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: rs
  namespace: ns
  uid: rs
  labels:
    app: foo
  ownerReferences:
  - apiVersion: apps/v1
    controller: true
    kind: Deployment
    name: deploy
    uid: deploy
spec:
  selector:
    matchLabels:
      app: foo
`,
				`apiVersion: apps/v1
kind: Deployment
metadata:
  name: deploy
  namespace: ns
  uid: deploy
spec:
  selector:
    matchLabels:
      app: foo
`},
			errors.New("no running pods found for deploy"),
		},
	}

	for _, test := range tests {
		test := test // pin
		t.Run(test.description, func(t *testing.T) {
			k8sClient, err := NewFakeAPI(test.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error %s", err)
			}
			_, err = NewPortForward(context.Background(), &KubernetesAPI{Interface: k8sClient}, test.ns, test.deployName, "localhost", 0, 0, false)
			if err != nil || test.err != nil {
				if (err == nil && test.err != nil) ||
					(err != nil && test.err == nil) ||
					(err.Error() != test.err.Error()) {
					t.Fatalf("Unexpected error (Expected: %s, Got: %s)", test.err, err)
				}
			}
		})
	}
}
