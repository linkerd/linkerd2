package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/config"
	pb "github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/charts"
)

func TestRender(t *testing.T) {
	defaultOptions, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	defaultValues, defaultConfig, err := defaultOptions.validateAndBuild("", nil)
	if err != nil {
		t.Fatalf("Unexpected error validating options: %v", err)
	}
	addFakeTLSSecrets(defaultValues)

	configValues, configConfig, err := defaultOptions.validateAndBuild(configStage, nil)
	if err != nil {
		t.Fatalf("Unexpected error validating options: %v", err)
	}
	addFakeTLSSecrets(configValues)

	controlPlaneValues, controlPlaneConfig, err := defaultOptions.validateAndBuild(controlPlaneStage, nil)
	if err != nil {
		t.Fatalf("Unexpected error validating options: %v", err)
	}

	// A configuration that shows that all config setting strings are honored
	// by `render()`.
	metaOptions, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	identityContext := toIdentityContext(&charts.Identity{
		Issuer: &charts.Issuer{
			ClockSkewAllowance: "20s",
			IssuanceLifetime:   "86400s",
		},
	})
	metaConfig := metaOptions.configs(identityContext)
	metaConfig.Global.LinkerdNamespace = "Namespace"
	metaValues := &charts.Values{
		Namespace:                   "Namespace",
		ClusterDomain:               "cluster.local",
		ControllerImage:             "ControllerImage",
		ControllerImageVersion:      "ControllerImageVersion",
		WebImage:                    "WebImage",
		PrometheusImage:             "PrometheusImage",
		GrafanaImage:                "GrafanaImage",
		ImagePullPolicy:             "ImagePullPolicy",
		UUID:                        "UUID",
		CliVersion:                  "CliVersion",
		ControllerLogLevel:          "ControllerLogLevel",
		PrometheusLogLevel:          "PrometheusLogLevel",
		ControllerComponentLabel:    "ControllerComponentLabel",
		ControllerNamespaceLabel:    "ControllerNamespaceLabel",
		CreatedByAnnotation:         "CreatedByAnnotation",
		ProxyContainerName:          "ProxyContainerName",
		ProxyInjectAnnotation:       "ProxyInjectAnnotation",
		ProxyInjectDisabled:         "ProxyInjectDisabled",
		LinkerdNamespaceLabel:       "LinkerdNamespaceLabel",
		ControllerUID:               2103,
		EnableH2Upgrade:             true,
		NoInitContainer:             false,
		WebhookFailurePolicy:        "WebhookFailurePolicy",
		OmitWebhookSideEffects:      false,
		RestrictDashboardPrivileges: false,
		InstallNamespace:            true,
		NodeSelector:                defaultValues.NodeSelector,
		Configs: charts.ConfigJSONs{
			Global:  "GlobalConfig",
			Proxy:   "ProxyConfig",
			Install: "InstallConfig",
		},
		ControllerReplicas: 1,
		Identity:           defaultValues.Identity,
		ProxyInjector:      defaultValues.ProxyInjector,
		ProfileValidator:   defaultValues.ProfileValidator,
		Tap:                defaultValues.Tap,
		Proxy: &charts.Proxy{
			Image: &charts.Image{
				Name:       "ProxyImageName",
				PullPolicy: "ImagePullPolicy",
				Version:    "ProxyVersion",
			},
			LogLevel: "warn,linkerd2_proxy=info",
			Ports: &charts.Ports{
				Admin:    4191,
				Control:  4190,
				Inbound:  4143,
				Outbound: 4140,
			},
			UID: 2102,
		},
		ProxyInit: &charts.ProxyInit{
			Image: &charts.Image{
				Name:       "ProxyInitImageName",
				PullPolicy: "ImagePullPolicy",
				Version:    "ProxyInitVersion",
			},
			Resources: &charts.Resources{
				CPU: charts.Constraints{
					Limit:   "100m",
					Request: "10m",
				},
				Memory: charts.Constraints{
					Limit:   "50Mi",
					Request: "10Mi",
				},
			},
		},
	}

	haOptions, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	haOptions.recordedFlags = []*config.Install_Flag{{Name: "ha", Value: "true"}}
	haOptions.highAvailability = true
	haValues, haConfig, _ := haOptions.validateAndBuild("", nil)
	addFakeTLSSecrets(haValues)

	haWithOverridesOptions, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

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
	addFakeTLSSecrets(haWithOverridesValues)

	noInitContainerOptions, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	noInitContainerOptions.recordedFlags = []*config.Install_Flag{{Name: "linkerd-cni-enabled", Value: "true"}}
	noInitContainerOptions.noInitContainer = true
	noInitContainerValues, noInitContainerConfig, _ := noInitContainerOptions.validateAndBuild("", nil)
	addFakeTLSSecrets(noInitContainerValues)

	testCases := []struct {
		values         *charts.Values
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
			if err := render(&buf, tc.values, tc.configs); err != nil {
				t.Fatalf("Failed to render templates: %v", err)
			}
			diffTestdata(t, tc.goldenFileName, buf.String())
		})
	}
}

func testInstallOptions() (*installOptions, error) {
	o, err := newInstallOptionsWithDefaults()
	if err != nil {
		return nil, err
	}

	o.ignoreCluster = true
	o.proxyVersion = "install-proxy-version"
	o.controlPlaneVersion = "install-control-plane-version"
	o.generateUUID = func() string {
		return "deaab91a-f4ab-448a-b7d1-c832a2fa0a60"
	}
	o.heartbeatSchedule = fakeHeartbeatSchedule
	o.identityOptions.crtPEMFile = filepath.Join("testdata", "crt.pem")
	o.identityOptions.keyPEMFile = filepath.Join("testdata", "key.pem")
	o.identityOptions.trustPEMFile = filepath.Join("testdata", "trust-anchors.pem")
	return o, nil
}

func TestValidate(t *testing.T) {
	t.Run("Accepts the default options as valid", func(t *testing.T) {
		opts, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		if err := opts.validate(); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Rejects invalid controller log level", func(t *testing.T) {
		options, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		options.controllerLogLevel = "super"
		expected := "--controller-log-level must be one of: panic, fatal, error, warn, info, debug"

		err = options.validate()
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != expected {
			t.Fatalf("Expected error string\"%s\", got \"%s\"", expected, err)
		}
	})

	t.Run("Ensure log level input is converted to lower case before passing to prometheus", func(t *testing.T) {
		underTest, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		underTest.controllerLogLevel = "DEBUG"
		expected := "debug"

		testValues := new(pb.All)
		testValues.Global = new(pb.Global)
		testValues.Proxy = new(pb.Proxy)
		testValues.Install = new(pb.Install)

		actual, err := underTest.buildValuesWithoutIdentity(testValues)

		if err != nil {
			t.Fatalf("Unexpected error ocured %s", err)
		}

		if actual.PrometheusLogLevel != expected {
			t.Fatalf("Expected error string\"%s\", got \"%s\"", expected, actual.PrometheusLogLevel)
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

		options, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

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

	t.Run("Rejects identity cert files data when external issuer is set", func(t *testing.T) {

		options, err := testInstallOptions()
		options.identityOptions.crtPEMFile = ""
		options.identityOptions.keyPEMFile = ""
		options.identityOptions.trustPEMFile = ""

		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		withoutCertDataOptions := options.identityOptions
		withoutCertDataOptions.identityExternalIssuer = true
		withCrtFile := *withoutCertDataOptions
		withCrtFile.crtPEMFile = "crt-file"
		withTrustAnchorsFile := *withoutCertDataOptions
		withTrustAnchorsFile.trustPEMFile = "ta-file"
		withKeyFile := *withoutCertDataOptions
		withKeyFile.keyPEMFile = "key-file"

		testCases := []struct {
			input         *installIdentityOptions
			expectedError string
		}{
			{withoutCertDataOptions, ""},
			{&withCrtFile, "--identity-issuer-certificate-file must not be specified if --identity-external-issuer=true"},
			{&withTrustAnchorsFile, "--identity-trust-anchors-file must not be specified if --identity-external-issuer=true"},
			{&withKeyFile, "--identity-issuer-key-file must not be specified if --identity-external-issuer=true"},
		}

		for _, tc := range testCases {
			err = tc.input.validate()

			if tc.expectedError != "" {
				if err == nil {
					t.Fatal("Expected error, got nothing")
				}
				if err.Error() != tc.expectedError {
					t.Fatalf("Expected error string\"%s\", got \"%s\"", tc.expectedError, err)
				}
			} else {
				if err != nil {
					t.Fatalf("Expected no error bu got \"%s\"", err)

				}
			}
		}
	})
}

func fakeHeartbeatSchedule() string {
	return "1 2 3 4 5"
}

func addFakeTLSSecrets(values *charts.Values) {
	values.ProxyInjector.CrtPEM = "proxy injector crt"
	values.ProxyInjector.KeyPEM = "proxy injector key"
	values.ProfileValidator.CrtPEM = "proxy injector crt"
	values.ProfileValidator.KeyPEM = "proxy injector key"
	values.Tap.CrtPEM = "tap crt"
	values.Tap.KeyPEM = "tap key"
}
