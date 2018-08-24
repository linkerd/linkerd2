package cmd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"
)

func TestRender(t *testing.T) {
	// The default configuration, with the random UUID overridden with a fixed
	// value to facilitate testing.
	defaultControlPlaneNamespace := controlPlaneNamespace
	defaultOptions := newInstallOptions()
	defaultConfig, err := validateAndBuildConfig(defaultOptions)
	if err != nil {
		t.Fatalf("Unexpected error from validateAndBuildConfig(): %v", err)
	}
	defaultConfig.UUID = "3e5903bf-803b-4555-abc4-9de68d35c4e3"

	// A configuration that shows that all config setting strings are honored
	// by `render()`.
	metaConfig := installConfig{
		Namespace:                   "Namespace",
		ControllerImage:             "ControllerImage",
		WebImage:                    "WebImage",
		PrometheusImage:             "PrometheusImage",
		GrafanaImage:                "GrafanaImage",
		ControllerReplicas:          1,
		WebReplicas:                 2,
		PrometheusReplicas:          3,
		ImagePullPolicy:             "ImagePullPolicy",
		UUID:                        "UUID",
		CliVersion:                  "CliVersion",
		ControllerLogLevel:          "ControllerLogLevel",
		ControllerComponentLabel:    "ControllerComponentLabel",
		CreatedByAnnotation:         "CreatedByAnnotation",
		ProxyAPIPort:                123,
		EnableTLS:                   true,
		TLSTrustAnchorConfigMapName: "TLSTrustAnchorConfigMapName",
	}

	testCases := []struct {
		config                installConfig
		controlPlaneNamespace string
		goldenFileName        string
	}{
		{*defaultConfig, defaultControlPlaneNamespace, "testdata/install_default.golden"},
		{metaConfig, metaConfig.Namespace, "testdata/install_output.golden"},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			controlPlaneNamespace = tc.controlPlaneNamespace

			var buf bytes.Buffer
			err := render(tc.config, &buf, defaultOptions)
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
