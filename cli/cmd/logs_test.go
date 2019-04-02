package cmd

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/wercker/stern/stern"
	"k8s.io/apimachinery/pkg/labels"
)

func generateLabel(component string, t *testing.T) labels.Selector {
	if component == "" {
		return labels.Everything()
	}
	lbl, err := labels.Parse(fmt.Sprintf("linkerd.io/control-plane-component=%s", component))
	if err != nil {
		t.Fatalf("Unable to generate label selector from [%s]", component)
	}
	return lbl
}

func generateRegexForContainer(containerName string, t *testing.T) *regexp.Regexp {
	containerRegex, err := regexp.Compile(containerName)
	if err != nil {
		t.Fatalf("Unable to generate regex from [%s]", containerName)
	}
	return containerRegex
}

func TestNewSternConfig(t *testing.T) {

	flagTestCases := []struct {
		testComponent  string
		testContainer  string
		expectedErr    error
		expectedConfig *stern.Config
	}{
		{
			testComponent: "grafana",
			expectedConfig: &stern.Config{
				LabelSelector:  generateLabel("grafana", t),
				ContainerQuery: generateRegexForContainer("", t),
			},
		},
		{
			testComponent: "not-grafana",
			expectedErr:   fmt.Errorf("control plane component [not-grafana] does not exist. Must be one of [grafana prometheus web controller]"),
			expectedConfig: &stern.Config{
				LabelSelector:  generateLabel("", t),
				ContainerQuery: generateRegexForContainer("", t),
			},
		},
		{
			testContainer: "tap",
			expectedConfig: &stern.Config{
				LabelSelector:  generateLabel("", t),
				ContainerQuery: generateRegexForContainer("tap", t),
			},
		},
		{
			testContainer: "not-tap",
			expectedErr:   fmt.Errorf("container [not-tap] does not exist in control plane [linkerd]"),
			expectedConfig: &stern.Config{
				LabelSelector:  generateLabel("", t),
				ContainerQuery: generateRegexForContainer("tap", t),
			},
		},
	}

	for _, tt := range flagTestCases {
		tt := tt // pin
		components := []string{"grafana", "prometheus", "web", "controller"}
		containers := []string{"tap", k8s.ProxyContainerName, "destination"}

		flags := &logsOptions{controlPlaneComponent: tt.testComponent, container: tt.testContainer}
		config, err := flags.toSternConfig(components, containers)

		t.Run(fmt.Sprintf("component %s, container %s", tt.testComponent, tt.testContainer), func(t *testing.T) {

			if config != nil {
				if config.LabelSelector.String() != tt.expectedConfig.LabelSelector.String() {
					t.Fatalf("Unexpected label selector:\ngot: %s\nExpected: %s", config.LabelSelector.String(), tt.expectedConfig.LabelSelector.String())
				}

				if config.ContainerQuery.String() != tt.expectedConfig.ContainerQuery.String() {
					t.Fatalf("Unexpected regex for container query:\ngot %s\nExpected: %s", config.ContainerQuery.String(), tt.expectedConfig.ContainerQuery.String())
				}
			}

			if err != nil {
				if tt.expectedErr != nil && err.Error() != tt.expectedErr.Error() {
					t.Fatalf("Unexpected error:\ngot: %s\nExpected: %s", err.Error(), tt.expectedErr.Error())
				}

				if tt.expectedErr == nil {
					t.Fatalf("Expected error to be nil but got %s", err.Error())
				}
			} else {
				if tt.expectedErr != nil {
					t.Fatalf("Expected error to be not nil")
				}
			}
		})
	}
}
