package k8s

import (
	"errors"
	"fmt"
	"testing"

	"k8s.io/client-go/rest"
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
			errors.New("no linkerd-metrics port found for container pod-name/linkerd-proxy"),
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
    - name: linkerd-metrics
      port: 123`,
			},
			errors.New("no linkerd-proxy container found for pod pod-name"),
		},
		{
			"bad-ns",
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
    - name: linkerd-metrics
      port: 123`,
			},
			errors.New("no running pods found for pod-name"),
		},
		{
			"pod-ns",
			"bad-name",
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
    - name: linkerd-metrics
      port: 123`,
			},
			errors.New("no running pods found for bad-name"),
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
    - name: linkerd-metrics
      port: 123`,
			},
			errors.New("no running pods found for pod-name"),
		},
	}

	for i, test := range tests {
		test := test // pin
		t.Run(fmt.Sprintf("%d: NewProxyMetricsForward returns expected result", i), func(t *testing.T) {
			k8sClient, _ := NewFakeClientSets(test.k8sConfigs...)
			_, err := NewProxyMetricsForward(&rest.Config{}, k8sClient, test.ns, test.name, false)
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
			k8sClient, _ := NewFakeClientSets(test.k8sConfigs...)
			_, err := NewPortForward(&rest.Config{}, k8sClient, test.ns, test.deployName, 0, 0, false)
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
