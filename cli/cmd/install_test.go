package cmd

import (
	"bytes"
	"fmt"
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
		Namespace:                   "Namespace",
		ControllerImage:             "ControllerImage",
		WebImage:                    "WebImage",
		PrometheusImage:             "PrometheusImage",
		PrometheusVolumeName:        "data",
		GrafanaImage:                "GrafanaImage",
		GrafanaVolumeName:           "data",
		ControllerReplicas:          1,
		ImagePullPolicy:             "ImagePullPolicy",
		UUID:                        "UUID",
		CliVersion:                  "CliVersion",
		ControllerLogLevel:          "ControllerLogLevel",
		PrometheusLogLevel:          "PrometheusLogLevel",
		ControllerComponentLabel:    "ControllerComponentLabel",
		CreatedByAnnotation:         "CreatedByAnnotation",
		EnableTLS:                   true,
		TLSTrustAnchorConfigMapName: "TLSTrustAnchorConfigMapName",
		ProxyContainerName:          "ProxyContainerName",
		TLSTrustAnchorFileName:      "TLSTrustAnchorFileName",
		ProxyAutoInjectEnabled:      true,
		ProxyInjectAnnotation:       "ProxyInjectAnnotation",
		ProxyInjectDisabled:         "ProxyInjectDisabled",
		ControllerUID:               2103,
		EnableH2Upgrade:             true,
		NoInitContainer:             false,
		GlobalConfig:                "GlobalConfig",
		ProxyConfig:                 "ProxyConfig",
	}

	haOptions := newInstallOptions()
	haOptions.highAvailability = true
	haConfig, _ := validateAndBuildConfig(haOptions)
	haConfig.UUID = defaultConfig.UUID

	haWithOverridesOptions := newInstallOptions()
	haWithOverridesOptions.highAvailability = true
	haWithOverridesOptions.controllerReplicas = 2
	haWithOverridesOptions.proxyCPURequest = "400m"
	haWithOverridesOptions.proxyMemoryRequest = "300Mi"
	haWithOverridesConfig, _ := validateAndBuildConfig(haWithOverridesOptions)
	haWithOverridesConfig.UUID = defaultConfig.UUID

	noInitContainerOptions := newInstallOptions()
	noInitContainerOptions.noInitContainer = true
	noInitContainerConfig, _ := validateAndBuildConfig(noInitContainerOptions)
	noInitContainerConfig.UUID = defaultConfig.UUID

	noInitContainerWithProxyAutoInjectOptions := newInstallOptions()
	noInitContainerWithProxyAutoInjectOptions.noInitContainer = true
	noInitContainerWithProxyAutoInjectOptions.proxyAutoInject = true
	noInitContainerWithProxyAutoInjectOptions.tls = "optional"
	noInitContainerWithProxyAutoInjectConfig, _ := validateAndBuildConfig(noInitContainerWithProxyAutoInjectOptions)
	noInitContainerWithProxyAutoInjectConfig.UUID = defaultConfig.UUID

	testCases := []struct {
		config                installConfig
		options               *installOptions
		controlPlaneNamespace string
		goldenFileName        string
	}{
		{*defaultConfig, defaultOptions, defaultControlPlaneNamespace, "install_default.golden"},
		{metaConfig, defaultOptions, metaConfig.Namespace, "install_output.golden"},
		{*haConfig, haOptions, haConfig.Namespace, "install_ha_output.golden"},
		{*haWithOverridesConfig, haWithOverridesOptions, haWithOverridesConfig.Namespace, "install_ha_with_overrides_output.golden"},
		{*noInitContainerConfig, noInitContainerOptions, noInitContainerConfig.Namespace, "install_no_init_container.golden"},
		{*noInitContainerWithProxyAutoInjectConfig, noInitContainerWithProxyAutoInjectOptions, noInitContainerWithProxyAutoInjectConfig.Namespace, "install_no_init_container_auto_inject.golden"},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			controlPlaneNamespace = tc.controlPlaneNamespace

			var buf bytes.Buffer
			if err := render(tc.config, &buf, tc.options); err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			diffTestdata(t, tc.goldenFileName, buf.String())
		})
	}
}

func TestValidate(t *testing.T) {
	t.Run("Accepts the default options as valid", func(t *testing.T) {
		if err := newInstallOptions().validate(); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Rejects invalid controller log level", func(t *testing.T) {
		options := newInstallOptions()
		options.controllerLogLevel = "super"
		expected := "--controller-log-level must be one of: panic, fatal, error, warn, info, debug"

		err := options.validate()
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != expected {
			t.Fatalf("Expected error string\"%s\", got \"%s\"", expected, err)
		}
	})

	t.Run("Properly validates proxy log level", func(t *testing.T) {
		testCases := []struct {
			input string
			valid bool
		}{
			{"", false},
			{"info", true},
			{"somemodule", true},
			{"bad%name", false},
			{"linkerd2_proxy=debug", true},
			{"linkerd2%proxy=debug", false},
			{"linkerd2_proxy=foobar", false},
			{"linker2d_proxy,std::option", true},
			{"warn,linkerd2_proxy=info", true},
			{"warn,linkerd2_proxy=foobar", false},
		}

		options := newInstallOptions()
		for _, tc := range testCases {
			options.proxyLogLevel = tc.input
			err := options.validate()
			if tc.valid && err != nil {
				t.Fatalf("Error not expected: %s", err)
			}
			if !tc.valid && err == nil {
				t.Fatalf("Expected error string \"%s is not a valid proxy log level\", got nothing", tc.input)
			}
			expectedErr := "\"%s\" is not a valid proxy log level - for allowed syntax check https://docs.rs/env_logger/0.6.0/env_logger/#enabling-logging"
			if !tc.valid && err.Error() != fmt.Sprintf(expectedErr, tc.input) {
				t.Fatalf("Expected error string \""+expectedErr+"\"", tc.input, err)
			}
		}
	})
}
