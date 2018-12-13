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
	// by `render()`. Note that `SingleNamespace` is tested in a separate
	// configuration, since it's incompatible with `ProxyAutoInjectEnabled`.
	metaConfig := installConfig{
		Namespace:                        "Namespace",
		ControllerImage:                  "ControllerImage",
		WebImage:                         "WebImage",
		PrometheusImage:                  "PrometheusImage",
		PrometheusVolumeName:             "data",
		GrafanaImage:                     "GrafanaImage",
		GrafanaVolumeName:                "data",
		ControllerReplicas:               1,
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
		ControllerUID:                    2103,
		InboundPort:                      4143,
		OutboundPort:                     4140,
		ProxyControlPort:                 4190,
		ProxyMetricsPort:                 4191,
		ProxyInitImage:                   "ProxyInitImage",
		ProxyImage:                       "ProxyImage",
		ProxyInjectorTLSSecret:           "ProxyInjectorTLSSecret",
		ProxySpecFileName:                "ProxySpecFileName",
		ProxyInitSpecFileName:            "ProxyInitSpecFileName",
		IgnoreInboundPorts:               "4190,4191,1,2,3",
		IgnoreOutboundPorts:              "2,3,4",
		ProxyResourceRequestCPU:          "RequestCPU",
		ProxyResourceRequestMemory:       "RequestMemory",
		ProxyBindTimeout:                 "1m",
		ProfileSuffixes:                  "suffix.",
		EnableH2Upgrade:                  true,
	}

	singleNamespaceConfig := installConfig{
		Namespace:                        "Namespace",
		ControllerImage:                  "ControllerImage",
		WebImage:                         "WebImage",
		PrometheusImage:                  "PrometheusImage",
		PrometheusVolumeName:             "data",
		GrafanaImage:                     "GrafanaImage",
		GrafanaVolumeName:                "data",
		ControllerReplicas:               1,
		ImagePullPolicy:                  "ImagePullPolicy",
		UUID:                             "UUID",
		CliVersion:                       "CliVersion",
		ControllerLogLevel:               "ControllerLogLevel",
		ControllerComponentLabel:         "ControllerComponentLabel",
		CreatedByAnnotation:              "CreatedByAnnotation",
		ProxyAPIPort:                     123,
		ProxyUID:                         2102,
		ControllerUID:                    2103,
		EnableTLS:                        true,
		TLSTrustAnchorConfigMapName:      "TLSTrustAnchorConfigMapName",
		ProxyContainerName:               "ProxyContainerName",
		TLSTrustAnchorFileName:           "TLSTrustAnchorFileName",
		TLSCertFileName:                  "TLSCertFileName",
		TLSPrivateKeyFileName:            "TLSPrivateKeyFileName",
		TLSTrustAnchorVolumeSpecFileName: "TLSTrustAnchorVolumeSpecFileName",
		TLSIdentityVolumeSpecFileName:    "TLSIdentityVolumeSpecFileName",
		SingleNamespace:                  true,
		EnableH2Upgrade:                  true,
	}

	haOptions := newInstallOptions()
	haOptions.highAvailability = true
	haConfig, _ := validateAndBuildConfig(haOptions)
	haConfig.UUID = "deaab91a-f4ab-448a-b7d1-c832a2fa0a60"

	haWithOverridesOptions := newInstallOptions()
	haWithOverridesOptions.highAvailability = true
	haWithOverridesOptions.controllerReplicas = 2
	haWithOverridesOptions.proxyCPURequest = "400m"
	haWithOverridesOptions.proxyMemoryRequest = "300Mi"
	haWithOverridesConfig, _ := validateAndBuildConfig(haWithOverridesOptions)
	haWithOverridesConfig.UUID = "deaab91a-f4ab-448a-b7d1-c832a2fa0a60"

	testCases := []struct {
		config                installConfig
		options               *installOptions
		controlPlaneNamespace string
		goldenFileName        string
	}{
		{*defaultConfig, defaultOptions, defaultControlPlaneNamespace, "testdata/install_default.golden"},
		{metaConfig, defaultOptions, metaConfig.Namespace, "testdata/install_output.golden"},
		{singleNamespaceConfig, defaultOptions, singleNamespaceConfig.Namespace, "testdata/install_single_namespace_output.golden"},
		{*haConfig, haOptions, haConfig.Namespace, "testdata/install_ha_output.golden"},
		{*haWithOverridesConfig, haWithOverridesOptions, haWithOverridesConfig.Namespace, "testdata/install_ha_with_overrides_output.golden"},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			controlPlaneNamespace = tc.controlPlaneNamespace

			var buf bytes.Buffer
			err := render(tc.config, &buf, tc.options)
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

func TestValidate(t *testing.T) {
	t.Run("Accepts the default options as valid", func(t *testing.T) {
		if err := newInstallOptions().validate(); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Rejects invalid log level", func(t *testing.T) {
		options := newInstallOptions()
		options.controllerLogLevel = "super"
		expected := "--controller-log-level must be one of: panic, fatal, error, warn, info, debug"

		err := options.validate()
		if err == nil {
			t.Fatalf("Expected error, got nothing")
		}
		if err.Error() != expected {
			t.Fatalf("Expected error string\"%s\", got \"%s\"", expected, err)
		}
	})

	t.Run("Rejects single namespace install with auto inject", func(t *testing.T) {
		options := newInstallOptions()
		options.proxyAutoInject = true
		options.singleNamespace = true
		expected := "The --proxy-auto-inject and --single-namespace flags cannot both be specified together"

		err := options.validate()
		if err == nil {
			t.Fatalf("Expected error, got nothing")
		}
		if err.Error() != expected {
			t.Fatalf("Expected error string\"%s\", got \"%s\"", expected, err)
		}
	})
}
