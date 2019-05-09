package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

func TestRender(t *testing.T) {
	defaultOptions := testInstallOptions()
	defaultValues, defaultConfig, err := defaultOptions.validateAndBuild("", nil)
	if err != nil {
		t.Fatalf("Unexpected error validating options: %v", err)
	}

	configValues, configConfig, err := defaultOptions.validateAndBuild(configStage, nil)
	if err != nil {
		t.Fatalf("Unexpected error validating options: %v", err)
	}

	controlPlaneValues, controlPlaneConfig, err := defaultOptions.validateAndBuild(controlPlaneStage, nil)
	if err != nil {
		t.Fatalf("Unexpected error validating options: %v", err)
	}

	// A configuration that shows that all config setting strings are honored
	// by `render()`.
	metaOptions := testInstallOptions()
	metaConfig := metaOptions.configs(nil)
	metaConfig.Global.LinkerdNamespace = "Namespace"
	metaValues := &installValues{
		Namespace:                "Namespace",
		ControllerImage:          "ControllerImage",
		WebImage:                 "WebImage",
		PrometheusImage:          "PrometheusImage",
		GrafanaImage:             "GrafanaImage",
		ImagePullPolicy:          "ImagePullPolicy",
		UUID:                     "UUID",
		CliVersion:               "CliVersion",
		ControllerLogLevel:       "ControllerLogLevel",
		PrometheusLogLevel:       "PrometheusLogLevel",
		ControllerComponentLabel: "ControllerComponentLabel",
		CreatedByAnnotation:      "CreatedByAnnotation",
		ProxyContainerName:       "ProxyContainerName",
		ProxyInjectAnnotation:    "ProxyInjectAnnotation",
		ProxyInjectDisabled:      "ProxyInjectDisabled",
		ControllerUID:            2103,
		EnableH2Upgrade:          true,
		NoInitContainer:          false,
		Configs: configJSONs{
			Global:  "GlobalConfig",
			Proxy:   "ProxyConfig",
			Install: "InstallConfig",
		},
		ControllerReplicas: 1,
		Identity:           defaultValues.Identity,
		ProxyInjector: &proxyInjectorValues{
			&tlsValues{
				KeyPEM: "proxy injector key",
				CrtPEM: "proxy injector crt",
			},
		},
		ProfileValidator: &profileValidatorValues{
			&tlsValues{
				KeyPEM: "profile validator key",
				CrtPEM: "profile validator crt",
			},
		},
	}

	haOptions := testInstallOptions()
	haOptions.recordedFlags = []*config.Install_Flag{{Name: "ha", Value: "true"}}
	haOptions.highAvailability = true
	haValues, haConfig, _ := haOptions.validateAndBuild("", nil)

	haWithOverridesOptions := testInstallOptions()
	haWithOverridesOptions.recordedFlags = []*config.Install_Flag{
		{Name: "ha", Value: "true"},
		{Name: "controller-replicas", Value: "2"},
		{Name: "proxy-cpu-request", Value: "400m"},
		{Name: "proxy-memory-request", Value: "300Mi"},
	}
	haWithOverridesOptions.highAvailability = true
	haWithOverridesOptions.controllerReplicas = 2
	haWithOverridesOptions.proxyCPURequest = "400m"
	haWithOverridesOptions.proxyMemoryRequest = "300Mi"
	haWithOverridesValues, haWithOverridesConfig, _ := haWithOverridesOptions.validateAndBuild("", nil)

	noInitContainerOptions := testInstallOptions()
	noInitContainerOptions.recordedFlags = []*config.Install_Flag{{Name: "linkerd-cni-enabled", Value: "true"}}
	noInitContainerOptions.noInitContainer = true
	noInitContainerValues, noInitContainerConfig, _ := noInitContainerOptions.validateAndBuild("", nil)

	testCases := []struct {
		values         *installValues
		configs        *config.All
		goldenFileName string
	}{
		{defaultValues, defaultConfig, "install_default.golden"},
		{configValues, configConfig, "install_config.golden"},
		{controlPlaneValues, controlPlaneConfig, "install_control-plane.golden"},
		{metaValues, metaConfig, "install_output.golden"},
		{haValues, haConfig, "install_ha_output.golden"},
		{haWithOverridesValues, haWithOverridesConfig, "install_ha_with_overrides_output.golden"},
		{noInitContainerValues, noInitContainerConfig, "install_no_init_container.golden"},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			controlPlaneNamespace = tc.configs.GetGlobal().GetLinkerdNamespace()

			var buf bytes.Buffer
			if err := tc.values.render(&buf, tc.configs); err != nil {
				t.Fatalf("Failed to render templates: %v", err)
			}
			diffTestdata(t, tc.goldenFileName, buf.String())
		})
	}
}

func testInstallOptions() *installOptions {
	o := newInstallOptionsWithDefaults()
	o.ignoreCluster = true
	o.proxyVersion = "install-proxy-version"
	o.controlPlaneVersion = "install-control-plane-version"
	o.generateUUID = func() string {
		return "deaab91a-f4ab-448a-b7d1-c832a2fa0a60"
	}
	o.generateTLS = func(commonName string) (*tlsValues, error) {
		switch commonName {
		case webhookCommonName(k8s.ProxyInjectorWebhookServiceName):
			return &tlsValues{
				KeyPEM: "proxy injector key",
				CrtPEM: "proxy injector crt",
			}, nil
		case webhookCommonName(k8s.SPValidatorWebhookServiceName):
			return &tlsValues{
				KeyPEM: "profile validator key",
				CrtPEM: "profile validator crt",
			}, nil
		}
		return nil, nil
	}
	o.identityOptions.crtPEMFile = filepath.Join("testdata", "crt.pem")
	o.identityOptions.keyPEMFile = filepath.Join("testdata", "key.pem")
	o.identityOptions.trustPEMFile = filepath.Join("testdata", "trust-anchors.pem")
	return o
}

func TestValidate(t *testing.T) {
	t.Run("Accepts the default options as valid", func(t *testing.T) {
		if err := testInstallOptions().validate(); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Rejects invalid controller log level", func(t *testing.T) {
		options := testInstallOptions()
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

		options := testInstallOptions()
		for _, tc := range testCases {
			options.proxyLogLevel = tc.input
			err := options.validate()
			if tc.valid && err != nil {
				t.Fatalf("Error not expected: %s", err)
			}
			if !tc.valid && err == nil {
				t.Fatalf("Expected error string \"%s is not a valid proxy log level\", got nothing", tc.input)
			}
			expectedErr := fmt.Sprintf("\"%s\" is not a valid proxy log level - for allowed syntax check https://docs.rs/env_logger/0.6.0/env_logger/#enabling-logging", tc.input)
			if tc.input == "" {
				expectedErr = "--proxy-log-level must not be empty"
			}
			if !tc.valid && err.Error() != expectedErr {
				t.Fatalf("Expected error string \"%s\", got \"%s\"; input=\"%s\"", expectedErr, err, tc.input)
			}
		}
	})
}
