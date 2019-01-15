package cmd

import (
	"testing"
)

func TestNewSternConfig(t *testing.T) {

	flagTestCases := []struct {
		components     []string
		containers     []string
		testComponent  string
		testContainer  string
		expectedErrStr string
	}{
		{
			components:    []string{"grafana", "prometheus", "web", "controller"},
			containers:    []string{"tap", "linkerd-proxy", "destination"},
			testComponent: "grafana",
		},
		{
			components:     []string{"grafana", "prometheus", "web", "controller"},
			containers:     []string{"tap", "linkerd-proxy", "destination"},
			testComponent:  "not-grafana",
			expectedErrStr: "control plane component [not-grafana] does not exist. Must be one of [grafana prometheus web controller]",
		},
		{
			components:    []string{"grafana", "prometheus", "web", "controller"},
			containers:    []string{"tap", "linkerd-proxy", "destination"},
			testContainer: "tap",
		},
		{
			components:     []string{"grafana", "prometheus", "web", "controller"},
			containers:     []string{"tap", "linkerd-proxy", "destination"},
			expectedErrStr: "container [not-tap] does not exist in control plane [linkerd]",
			testContainer:  "not-tap",
		},
	}

	for _, tt := range flagTestCases {
		t.Run("container logs selection", func(t *testing.T) {
			flags := &logsOptions{controlPlaneComponent: tt.testComponent, container: tt.testContainer}
			_, err := flags.toSternConfig(tt.components, tt.containers)
			if err != nil && err.Error() != tt.expectedErrStr {
				t.Fatalf("Unexpected error: \ngot: %v\n expected: %v", err, tt.expectedErrStr)
			}
		})
	}
}
