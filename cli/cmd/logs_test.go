package cmd

import (
	"bytes"
	"testing"

	"github.com/fatih/color"
	"github.com/wercker/stern/stern"
)

func TestNewSternConfig(t *testing.T) {
	t.Run("Default log template", func(t *testing.T) {
		flags := &logsOptions{}
		testColor := color.New(color.FgHiRed)
		expectedLogLine := "linkerd-controller-xyz web-container Starting server on port 8084"
		buf := bytes.Buffer{}
		c, err := flags.toSternConfig(nil, nil)

		if err != nil {
			t.Fatalf("Error creating stern config: %s", err.Error())
		}

		err = c.Template.Execute(&buf, &stern.Log{
			Message:        "Starting server on port 8084",
			Namespace:      "linkerd",
			PodName:        "linkerd-controller-xyz",
			ContainerName:  "web-container",
			PodColor:       testColor,
			ContainerColor: testColor,
		})

		if err != nil {
			t.Fatalf("Unexpected error: %s", err.Error())
		}

		if buf.String() != expectedLogLine {
			t.Fatalf("Invalid log line format\n got: %s\n expected:%s\n", buf.String(), expectedLogLine)
		}
	})

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
		t.Run("Can only specify valid control plane container names", func(t *testing.T) {
			flags := &logsOptions{component: tt.testComponent, container: tt.testContainer}
			_, err := flags.toSternConfig(tt.components, tt.containers)
			if err != nil && err.Error() != tt.expectedErrStr {
				t.Fatalf("Unexpected error: \ngot: %v\n expected: %v", err, tt.expectedErrStr)
			}
		})
	}
}
