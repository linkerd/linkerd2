package k8s

import (
	"errors"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewProxyMetricsForward(t *testing.T) {
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
  - name: bad-container
    ports:
    - name: linkerd-admin
      port: 123`,
			},
			errors.New("no linkerd-proxy container found for pod pod-name"),
		},
		{
			"pod-ns",
			"pod-name",
			[]string{`apiVersion: v1
kind: Pod
metadata:
  name: pod-name
  namespace: pod-ns
status:
  phase: Stopped
spec:
  containers:
  - name: linkerd-proxy
    ports:
    - name: linkerd-admin
      port: 123`,
			},
			errors.New("pod not running: pod-name"),
		},
	}

	for i, test := range tests {
		test := test // pin
		t.Run(fmt.Sprintf("%d: NewProxyMetricsForward returns expected result", i), func(t *testing.T) {
			k8sClient, err := NewFakeAPI(test.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error %s", err)
			}
			pod, err := k8sClient.CoreV1().Pods(test.ns).Get(test.name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Unexpected error %s", err)
			}
			_, err = NewProxyMetricsForward(&KubernetesAPI{Interface: k8sClient}, *pod, false)
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
	// TODO: test successful cases by mocking out `clientset.CoreV1().RESTClient()`
	tests := []struct {
		ns         string
		deployName string
		k8sConfigs []string
		err        error
	}{
		{
			"pod-ns",
			"deploy-name",
			[]string{`apiVersion: v1
kind: Pod
metadata:
  name: bad-name
  namespace: pod-ns
status:
  phase: Running`,
			},
			errors.New("no running pods found for deploy-name"),
		},
		{
			"pod-ns",
			"deploy-name",
			[]string{`apiVersion: v1
kind: Pod
metadata:
  name: deploy-name-foo-bar
  namespace: bad-ns
status:
  phase: Running`,
			},
			errors.New("no running pods found for deploy-name"),
		},
		{
			"pod-ns",
			"deploy-name",
			[]string{`apiVersion: v1
kind: Pod
metadata:
  name: deploy-name-foo-bar
  namespace: pod-ns
status:
  phase: Stopped`,
			},
			errors.New("no running pods found for deploy-name"),
		},
	}

	for i, test := range tests {
		test := test // pin
		t.Run(fmt.Sprintf("%d: NewPortForward returns expected result", i), func(t *testing.T) {
			k8sClient, err := NewFakeAPI(test.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error %s", err)
			}
			_, err = NewPortForward(&KubernetesAPI{Interface: k8sClient}, test.ns, test.deployName, "localhost", 0, 0, false)
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
