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
	defaultConfig.UUID = "deaab91a-f4ab-448a-b7d1-c832a2fa0a60"

	// A configuration that shows that all config setting strings are honored
	// by `render()`.
	metaConfig := installConfig{
		Namespace:                        "Namespace",
		ControllerImage:                  "ControllerImage",
		WebImage:                         "WebImage",
		PrometheusImage:                  "PrometheusImage",
		GrafanaImage:                     "GrafanaImage",
		ControllerReplicas:               1,
		WebReplicas:                      2,
		PrometheusReplicas:               3,
		ImagePullPolicy:                  "ImagePullPolicy",
		UUID:                             "UUID",
		CliVersion:                       "CliVersion",
		ControllerLogLevel:               "ControllerLogLevel",
		ControllerComponentLabel:         "ControllerComponentLabel",
		CreatedByAnnotation:              "CreatedByAnnotation",
		ProxyAPIPort:                     123,
		EnableTLS:                        true,
		TLSTrustAnchorConfigMapName:      "TLSTrustAnchorConfigMapName",
		ProxyContainerName:               "ProxyContainerName",
		TLSTrustAnchorFileName:           "TLSTrustAnchorFileName",
		TLSCertFileName:                  "TLSCertFileName",
		TLSPrivateKeyFileName:            "TLSPrivateKeyFileName",
		TLSTrustAnchorVolumeSpecFileName: "TLSTrustAnchorVolumeSpecFileName",
		TLSIdentityVolumeSpecFileName:    "TLSIdentityVolumeSpecFileName",
		ProxyAutoInjectEnabled:           true,
		ProxyAutoInjectLabel:             "ProxyAutoInjectLabel",
		ProxyUID:                         2102,
		InboundPort:                      4143,
		OutboundPort:                     4140,
		ProxyControlPort:                 4190,
		ProxyMetricsPort:                 4191,
		ProxyInitImage:                   "ProxyInitImage",
		ProxyImage:                       "ProxyImage",
		ProxyInjectorTLSSecret:           "ProxyInjectorTLSSecret",
		ProxyInjectorSidecarConfig:       "ProxyInjectorSidecarConfig",
		ProxySpecFileName:                "ProxySpecFileName",
		ProxyInitSpecFileName:            "ProxyInitSpecFileName",
		IgnoreInboundPorts:               []uint{1, 2, 3},
		IgnoreOutboundPorts:              []uint{2, 3, 4},
		ProxyResourceRequestCPU:          "RequestCPU",
		ProxyResourceRequestMemory:       "RequestMemory",
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
