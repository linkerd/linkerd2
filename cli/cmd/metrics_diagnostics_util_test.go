package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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

func Test_obfuscateMetrics(t *testing.T) {
	tests := []struct {
		inputFileName  string
		goldenFileName string
		wantErr        bool
	}{
		{
			inputFileName:  "obfuscate-diagnostics-proxy-metrics.input",
			goldenFileName: "obfuscate-diagnostics-proxy-metrics.golden",
			wantErr:        false,
		},
	}
	for i, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("%d: %s", i, tc.inputFileName), func(t *testing.T) {
			file, err := os.Open("testdata/" + tc.inputFileName)
			if err != nil {
				t.Errorf("error opening test input file: %v\n", err)
			}

			fileBytes, err := io.ReadAll(file)
			if err != nil {
				t.Errorf("error reading test input file: %v\n", err)
			}

			got, err := obfuscateMetrics(fileBytes)
			if (err != nil) != tc.wantErr {
				t.Errorf("obfuscateMetrics() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			testDataDiffer.DiffTestdata(t, tc.goldenFileName, string(got))
		})
	}
}
