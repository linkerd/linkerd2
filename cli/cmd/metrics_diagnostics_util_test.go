package cmd

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/linkerd/linkerd2/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetAllContainersWithPort(t *testing.T) {
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

	ctx := context.Background()
	for i, test := range tests {
		test := test // pin
		t.Run(fmt.Sprintf("%d: getAllContainersWithPort returns expected result", i), func(t *testing.T) {
			k8sClient, err := k8s.NewFakeAPI(test.k8sConfigs...)
			if err != nil {
				t.Fatalf("Unexpected error %s", err)
			}
			pod, err := k8sClient.CoreV1().Pods(test.ns).Get(ctx, test.name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Unexpected error %s", err)
			}
			_, err = getAllContainersWithPort(*pod, "admin-http")
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
