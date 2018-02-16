package cmd

import (
	"bytes"
	"fmt"

	"io/ioutil"
	"testing"
)

func TestRender(t *testing.T) {
	// A configuration that shows that all config setting strings are honored
	// by `render()`.
	metaConfig := installConfig{
		Namespace:                "Namespace",
		ControllerImage:          "ControllerImage",
		WebImage:                 "WebImage",
		PrometheusImage:          "PrometheusImage",
		ControllerReplicas:       1,
		WebReplicas:              2,
		PrometheusReplicas:       3,
		ImagePullPolicy:          "ImagePullPolicy",
		UUID:                     "UUID",
		CliVersion:               "CliVersion",
		ControllerLogLevel:       "ControllerLogLevel",
		ControllerComponentLabel: "ControllerComponentLabel",
		CreatedByAnnotation:      "CreatedByAnnotation",
	}

	testCases := []struct {
		config                installConfig
		controlPlaneNamespace string
		goldenFileName        string
	}{
		{metaConfig, metaConfig.Namespace, "testdata/install_output.golden"},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			controlPlaneNamespace = tc.controlPlaneNamespace

			var buf bytes.Buffer
			err := render(tc.config, &buf)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			content := buf.String()

			goldenFileBytes, err := ioutil.ReadFile(tc.goldenFileName)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			expectedContent := string(goldenFileBytes)
			diffCompare(t, content, expectedContent)
		})
	}
}
