package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
)

const (
	installProxyVersion        = "install-proxy-version"
	installControlPlaneVersion = "install-control-plane-version"
	installDebugVersion        = "install-debug-version"
)

func TestRender(t *testing.T) {
	ctx := context.Background()
	defaultOptions, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	defaultValues, err := defaultOptions.validateAndBuild(ctx, "", nil)
	if err != nil {
		t.Fatalf("Unexpected error validating options: %v", err)
	}
	addFakeTLSSecrets(defaultValues)

	configValues, err := defaultOptions.validateAndBuild(ctx, configStage, nil)
	if err != nil {
		t.Fatalf("Unexpected error validating options: %v", err)
	}
	addFakeTLSSecrets(configValues)

	controlPlaneValues, err := defaultOptions.validateAndBuild(ctx, controlPlaneStage, nil)
	if err != nil {
		t.Fatalf("Unexpected error validating options: %v", err)
	}

	// A configuration that shows that all config setting strings are honored
	// by `render()`.
	metaOptions, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	identityContext := toIdentityContext(&identityWithAnchorsAndTrustDomain{
		TrustAnchorsPEM: "test-trust-anchor",
		Identity: &charts.Identity{
			Issuer: &charts.Issuer{
				ClockSkewAllowance: "20s",
				IssuanceLifetime:   "86400s",
			},
		},
	})
	metaConfig := metaOptions.configs(identityContext)
	metaConfig.Global.LinkerdNamespace = "Namespace"
	metaValues := &charts.Values{
		ControllerImage:             "ControllerImage",
		WebImage:                    "WebImage",
		ControllerUID:               2103,
		EnableH2Upgrade:             true,
		WebhookFailurePolicy:        "WebhookFailurePolicy",
		OmitWebhookSideEffects:      false,
		RestrictDashboardPrivileges: false,
		InstallNamespace:            true,
		Identity:                    defaultValues.Identity,
		NodeSelector:                defaultValues.NodeSelector,
		Tolerations:                 defaultValues.Tolerations,
		Global: &charts.Global{
			Namespace:                "Namespace",
			ClusterDomain:            "cluster.local",
			ImagePullPolicy:          "ImagePullPolicy",
			CliVersion:               "CliVersion",
			ControllerComponentLabel: "ControllerComponentLabel",
			ControllerLogLevel:       "ControllerLogLevel",
			ControllerImageVersion:   "ControllerImageVersion",
			ControllerNamespaceLabel: "ControllerNamespaceLabel",
			WorkloadNamespaceLabel:   "WorkloadNamespaceLabel",
			CreatedByAnnotation:      "CreatedByAnnotation",
			ProxyInjectAnnotation:    "ProxyInjectAnnotation",
			ProxyInjectDisabled:      "ProxyInjectDisabled",
			LinkerdNamespaceLabel:    "LinkerdNamespaceLabel",
			ProxyContainerName:       "ProxyContainerName",
			CNIEnabled:               false,
			IdentityTrustDomain:      defaultValues.Global.IdentityTrustDomain,
			IdentityTrustAnchorsPEM:  defaultValues.Global.IdentityTrustAnchorsPEM,
			Proxy: &charts.Proxy{
				DestinationGetNetworks: "DestinationGetNetworks",
				Image: &charts.Image{
					Name:       "ProxyImageName",
					PullPolicy: "ImagePullPolicy",
					Version:    "ProxyVersion",
				},
				LogLevel:  "warn,linkerd=info",
				LogFormat: "plain",
				Resources: &charts.Resources{
					CPU: charts.Constraints{
						Limit:   "cpu-limit",
						Request: "cpu-request",
					},
					Memory: charts.Constraints{
						Limit:   "memory-limit",
						Request: "memory-request",
					},
				},
				Ports: &charts.Ports{
					Admin:    4191,
					Control:  4190,
					Inbound:  4143,
					Outbound: 4140,
				},
				UID:   2102,
				Trace: &charts.Trace{},
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
				XTMountPath: &charts.VolumeMountPath{
					MountPath: "/run",
					Name:      "linkerd-proxy-init-xtables-lock",
				},
			},
		},
		Configs: charts.ConfigJSONs{
			Global:  "GlobalConfig",
			Proxy:   "ProxyConfig",
			Install: "InstallConfig",
		},
		DebugContainer: &charts.DebugContainer{
			Image: &charts.Image{
				Name:       "DebugImageName",
				PullPolicy: "DebugImagePullPolicy",
				Version:    "DebugVersion",
			},
		},
		ControllerReplicas: 1,
		ProxyInjector:      defaultValues.ProxyInjector,
		ProfileValidator:   defaultValues.ProfileValidator,
		Tap:                defaultValues.Tap,
		Dashboard: &charts.Dashboard{
			Replicas: 1,
		},
		Prometheus: charts.Prometheus{
			"enabled": true,
			"image":   "PrometheusImage",
		},
		Tracing: map[string]interface{}{
			"enabled": false,
		},
		Grafana: defaultValues.Grafana,
	}

	haOptions, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	haOptions.recordedFlags = []*config.Install_Flag{{Name: "ha", Value: "true"}}
	haOptions.highAvailability = true
	haValues, _ := haOptions.validateAndBuild(ctx, "", nil)
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
	haWithOverridesValues, _ := haWithOverridesOptions.validateAndBuild(ctx, "", nil)
	addFakeTLSSecrets(haWithOverridesValues)

	cniEnabledOptions, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	cniEnabledOptions.recordedFlags = []*config.Install_Flag{{Name: "linkerd-cni-enabled", Value: "true"}}
	cniEnabledOptions.cniEnabled = true
	cniEnabledValues, _ := cniEnabledOptions.validateAndBuild(ctx, "", nil)
	addFakeTLSSecrets(cniEnabledValues)

	withProxyIgnoresOptions, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withProxyIgnoresOptions.ignoreInboundPorts = []string{"22", "8100-8102"}
	withProxyIgnoresOptions.ignoreOutboundPorts = []string{"5432"}
	withProxyIgnoresValues, _ := withProxyIgnoresOptions.validateAndBuild(ctx, "", nil)
	addFakeTLSSecrets(withProxyIgnoresValues)

	withHeartBeatDisabled, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withHeartBeatDisabled.disableHeartbeat = true
	withHeartBeatDisabledValues, _ := withHeartBeatDisabled.validateAndBuild(ctx, "", nil)
	addFakeTLSSecrets(withHeartBeatDisabledValues)

	withRestrictedDashboardPrivileges, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withRestrictedDashboardPrivileges.restrictDashboardPrivileges = true
	withRestrictedDashboardPrivilegesValues, _ := withRestrictedDashboardPrivileges.validateAndBuild(ctx, "", nil)
	addFakeTLSSecrets(withRestrictedDashboardPrivilegesValues)

	withControlPlaneTracing, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withControlPlaneTracing.controlPlaneTracing = true
	withControlPlaneTracingValues, _ := withControlPlaneTracing.validateAndBuild(ctx, "", nil)
	addFakeTLSSecrets(withControlPlaneTracingValues)

	customRegistryOverride := "my.custom.registry/linkerd-io"
	withCustomRegistryOptions, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withCustomRegistryOptions.dockerRegistry = customRegistryOverride
	withCustomRegistryOptions.recordedFlags = []*config.Install_Flag{
		{Name: "registry", Value: customRegistryOverride},
	}
	withCustomRegistryValues, _ := withCustomRegistryOptions.validateAndBuild(ctx, "", nil)
	addFakeTLSSecrets(withCustomRegistryValues)

	withAddOnConfigStage, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withAddOnConfigStageValues, _ := withAddOnConfigStage.validateAndBuild(ctx, configStage, nil)
	withAddOnConfigStageValues.Tracing["enabled"] = true
	addFakeTLSSecrets(withAddOnConfigStageValues)

	withAddOnControlPlaneStage, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withAddOnControlPlaneStageValues, _ := withAddOnControlPlaneStage.validateAndBuild(ctx, controlPlaneStage, nil)
	withAddOnControlPlaneStageValues.Tracing["enabled"] = true
	addFakeTLSSecrets(withAddOnControlPlaneStageValues)

	withCustomDestinationGetNets, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withCustomDestinationGetNets.destinationGetNetworks = []string{"10.0.0.0/8", "172.0.0.0/8"}
	withCustomDestinationGetNetsValues, _ := withCustomDestinationGetNets.validateAndBuild(ctx, "", nil)
	addFakeTLSSecrets(withCustomDestinationGetNetsValues)

	testCases := []struct {
		values         *charts.Values
		goldenFileName string
	}{
		{defaultValues, "install_default.golden"},
		{configValues, "install_config.golden"},
		{controlPlaneValues, "install_control-plane.golden"},
		{metaValues, "install_output.golden"},
		{haValues, "install_ha_output.golden"},
		{haWithOverridesValues, "install_ha_with_overrides_output.golden"},
		{cniEnabledValues, "install_no_init_container.golden"},
		{withProxyIgnoresValues, "install_proxy_ignores.golden"},
		{withHeartBeatDisabledValues, "install_heartbeat_disabled_output.golden"},
		{withRestrictedDashboardPrivilegesValues, "install_restricted_dashboard.golden"},
		{withControlPlaneTracingValues, "install_controlplane_tracing_output.golden"},
		{withCustomRegistryValues, "install_custom_registry.golden"},
		{withAddOnConfigStageValues, "install_addon_config.golden"},
		{withAddOnControlPlaneStageValues, "install_addon_control-plane.golden"},
		{withCustomDestinationGetNetsValues, "install_default_override_dst_get_nets.golden"},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			var buf bytes.Buffer
			if err := render(&buf, tc.values); err != nil {
				t.Fatalf("Failed to render templates: %v", err)
			}
			diffTestdata(t, tc.goldenFileName, buf.String())
		})
	}
}

func TestValidateAndBuild_Errors(t *testing.T) {
	ctx := context.Background()
	t.Run("Fails validation for invalid ignoreInboundPorts", func(t *testing.T) {
		installOptions, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}
		installOptions.ignoreInboundPorts = []string{"-25"}
		_, err = installOptions.validateAndBuild(ctx, "", nil)
		if err == nil {
			t.Fatal("expected error but got nothing")
		}
	})

	t.Run("Fails validation for invalid ignoreOutboundPorts", func(t *testing.T) {
		installOptions, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}
		installOptions.ignoreOutboundPorts = []string{"-25"}
		_, err = installOptions.validateAndBuild(ctx, "", nil)
		if err == nil {
			t.Fatal("expected error but got nothing")
		}
	})
}

func testInstallOptions() (*installOptions, error) {
	o, err := newInstallOptionsWithDefaults()
	if err != nil {
		return nil, err
	}

	o.ignoreCluster = true
	o.proxyVersion = installProxyVersion
	o.debugImageVersion = installDebugVersion
	o.controlPlaneVersion = installControlPlaneVersion
	o.heartbeatSchedule = fakeHeartbeatSchedule
	o.identityOptions.crtPEMFile = filepath.Join("testdata", "valid-crt.pem")
	o.identityOptions.keyPEMFile = filepath.Join("testdata", "valid-key.pem")
	o.identityOptions.trustPEMFile = filepath.Join("testdata", "valid-trust-anchors.pem")
	return o, nil
}

func testInstallValues() (*linkerd2.Values, error) {
	values, err := linkerd2.NewValues(false)
	if err != nil {
		return nil, err
	}

	values.Global.Proxy.Image.Version = installProxyVersion
	values.Global.ProxyInit.Image.Version = installProxyVersion
	values.DebugContainer.Image.Version = installDebugVersion
	values.Global.LinkerdVersion = installControlPlaneVersion
	values.Global.ControllerImageVersion = installControlPlaneVersion
	values.HeartbeatSchedule = fakeHeartbeatSchedule()

	identityCert, err := ioutil.ReadFile(filepath.Join("testdata", "valid-crt.pem"))
	if err != nil {
		return nil, err
	}
	identityKey, err := ioutil.ReadFile(filepath.Join("testdata", "valid-key.pem"))
	if err != nil {
		return nil, err
	}
	trustAnchorsPEM, err := ioutil.ReadFile(filepath.Join("testdata", "valid-trust-anchors.pem"))
	if err != nil {
		return nil, err
	}

	values.Identity.Issuer.TLS.CrtPEM = string(identityCert)
	values.Identity.Issuer.TLS.KeyPEM = string(identityKey)
	values.Global.IdentityTrustAnchorsPEM = string(trustAnchorsPEM)
	return values, nil
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

	t.Run("Rejects invalid destination networks", func(t *testing.T) {
		options, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		options.destinationGetNetworks = []string{"wrong"}
		expected := "cannot parse destination get networks: invalid CIDR address: wrong"

		err = options.validate()
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != expected {
			t.Fatalf("Expected error string\"%s\", got \"%s\"", expected, err)
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
			{"warn,linkerd=info", true},
			{"warn,linkerd=foobar", false},
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

	t.Run("Validates the issuer certs upon install", func(t *testing.T) {

		testCases := []struct {
			crtFilePrefix string
			expectedError string
		}{
			{"valid", ""},
			{"expired", "failed to verify issuer certs stored on disk: not valid anymore. Expired on 1990-01-01T01:01:11Z"},
			{"not-valid-yet", "failed to verify issuer certs stored on disk: not valid before: 2100-01-01T01:00:51Z"},
			{"wrong-domain", "failed to verify issuer certs stored on disk: x509: certificate is valid for wrong.linkerd.cluster.local, not identity.linkerd.cluster.local"},
			{"wrong-algo", "failed to verify issuer certs stored on disk: must use P-256 curve for public key, instead P-521 was used"},
		}
		for _, tc := range testCases {

			options, err := testInstallOptions()
			if err != nil {
				t.Fatalf("Unexpected error: %v\n", err)
			}

			options.identityOptions.crtPEMFile = filepath.Join("testdata", tc.crtFilePrefix+"-crt.pem")
			options.identityOptions.keyPEMFile = filepath.Join("testdata", tc.crtFilePrefix+"-key.pem")
			options.identityOptions.trustPEMFile = filepath.Join("testdata", tc.crtFilePrefix+"-trust-anchors.pem")

			_, err = options.identityOptions.validateAndBuild(context.Background())

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
	values.ProxyInjector.CaBundle = "proxy injector CA bundle"
	values.ProfileValidator.CrtPEM = "profile validator crt"
	values.ProfileValidator.KeyPEM = "profile validator key"
	values.ProfileValidator.CaBundle = "profile validator CA bundle"
	values.Tap.CrtPEM = "tap crt"
	values.Tap.KeyPEM = "tap key"
	values.Tap.CaBundle = "tap CA bundle"
}
