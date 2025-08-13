package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/cli/flag"
	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"helm.sh/helm/v3/pkg/cli/values"
	corev1 "k8s.io/api/core/v1"
)

const (
	installProxyVersion        = "install-proxy-version"
	installControlPlaneVersion = "install-control-plane-version"
	installDebugVersion        = "install-debug-version"
)

func TestRender(t *testing.T) {
	defaultValues, err := testInstallOptionsFakeCerts()
	if err != nil {
		t.Fatal(err)
	}

	gidValues, err := testInstallOptionsFakeCerts()
	if err != nil {
		t.Fatal(err)
	}
	gidValues.ControllerGID = 1234
	gidValues.Proxy.GID = 4321

	// A configuration that shows that all config setting strings are honored
	// by `render()`.
	var controllerGID int64 = 2103
	var proxyGID int64 = 2102
	metaValues := &charts.Values{
		ControllerImage:         "ControllerImage",
		LinkerdVersion:          "LinkerdVersion",
		ControllerUID:           2103,
		ControllerGID:           controllerGID,
		EnableH2Upgrade:         true,
		WebhookFailurePolicy:    "WebhookFailurePolicy",
		HeartbeatSchedule:       "1 2 3 4 5",
		Identity:                defaultValues.Identity,
		NodeSelector:            defaultValues.NodeSelector,
		Tolerations:             defaultValues.Tolerations,
		ClusterDomain:           "cluster.local",
		ClusterNetworks:         "ClusterNetworks",
		ImagePullPolicy:         "ImagePullPolicy",
		CliVersion:              "CliVersion",
		ControllerLogLevel:      "ControllerLogLevel",
		ControllerLogFormat:     "ControllerLogFormat",
		ProxyContainerName:      "ProxyContainerName",
		RevisionHistoryLimit:    10,
		CNIEnabled:              false,
		IdentityTrustDomain:     defaultValues.IdentityTrustDomain,
		IdentityTrustAnchorsPEM: defaultValues.IdentityTrustAnchorsPEM,
		DestinationController:   defaultValues.DestinationController,
		PodAnnotations:          map[string]string{},
		PodLabels:               map[string]string{},
		PriorityClassName:       "PriorityClassName",
		PolicyController: &charts.PolicyController{
			LogLevel: "log-level",
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
			ProbeNetworks: []string{"1.0.0.0/0", "2.0.0.0/0"},
		},
		Proxy: &charts.Proxy{
			Image: &charts.Image{
				Name:       "ProxyImageName",
				PullPolicy: "ImagePullPolicy",
				Version:    "ProxyVersion",
			},
			LogLevel:       "warn,linkerd=info",
			LogFormat:      "plain",
			LogHTTPHeaders: "off",
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
			UID:                  2102,
			GID:                  proxyGID,
			OpaquePorts:          "25,443,587,3306,5432,11211",
			Await:                true,
			DefaultInboundPolicy: "default-allow-policy",
			Metrics: &charts.ProxyMetrics{
				HostnameLabels: false,
			},
			Tracing: &charts.ProxyTracing{
				Enable:           false,
				TraceServiceName: "linkerd-proxy",
				Collector: &charts.ProxyTracingCollector{
					Endpoint: "",
					MeshIdentity: &charts.ProxyTracingCollectorIdentity{
						ServiceAccountName: "",
					},
				},
			},
			LivenessProbe: &charts.Probe{
				InitialDelaySeconds: 10,
				TimeoutSeconds:      1,
			},
			ReadinessProbe: &charts.Probe{
				InitialDelaySeconds: 2,
				TimeoutSeconds:      1,
			},
		},
		ProxyInit: &charts.ProxyInit{
			IptablesMode: "legacy",
			Image: &charts.Image{
				Name:       "ProxyInitImageName",
				PullPolicy: "ImagePullPolicy",
				Version:    "ProxyInitVersion",
			},
			IgnoreOutboundPorts: "443",
			XTMountPath: &charts.VolumeMountPath{
				MountPath: "/run",
				Name:      "linkerd-proxy-init-xtables-lock",
			},
			RunAsRoot:  false,
			RunAsUser:  65534,
			RunAsGroup: 65534,
		},
		NetworkValidator: &charts.NetworkValidator{
			LogLevel:    "debug",
			LogFormat:   "plain",
			ConnectAddr: "1.1.1.1:20001",
			ListenAddr:  "[::]:4140",
			Timeout:     "10s",
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
		PolicyValidator:    defaultValues.PolicyValidator,
		Egress:             defaultValues.Egress,
	}

	haValues, err := testInstallOptionsHA(true)
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	addFakeTLSSecrets(haValues)

	haWithOverridesValues, err := testInstallOptionsHA(true)
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	haWithOverridesValues.HighAvailability = true
	haWithOverridesValues.ControllerReplicas = 2
	haWithOverridesValues.Proxy.Resources.CPU.Request = "400m"
	haWithOverridesValues.Proxy.Resources.Memory.Request = "300Mi"
	haWithOverridesValues.EnablePodDisruptionBudget = true
	addFakeTLSSecrets(haWithOverridesValues)

	cniEnabledValues, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}

	cniEnabledValues.CNIEnabled = true
	addFakeTLSSecrets(cniEnabledValues)

	withProxyIgnoresValues, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withProxyIgnoresValues.ProxyInit.IgnoreInboundPorts = "22,8100-8102"
	withProxyIgnoresValues.ProxyInit.IgnoreOutboundPorts = "5432"
	addFakeTLSSecrets(withProxyIgnoresValues)

	withHeartBeatDisabledValues, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withHeartBeatDisabledValues.DisableHeartBeat = true
	addFakeTLSSecrets(withHeartBeatDisabledValues)

	withControlPlaneTracingValues, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withControlPlaneTracingValues.ControlPlaneTracing = true
	addFakeTLSSecrets(withControlPlaneTracingValues)

	customRegistryOverride := "my.custom.registry/linkerd-io"
	withCustomRegistryValues, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	flags, flagSet := makeProxyFlags(withCustomRegistryValues)
	err = flagSet.Set("registry", customRegistryOverride)
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	err = flag.ApplySetFlags(withCustomRegistryValues, flags)
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	addFakeTLSSecrets(withCustomRegistryValues)

	withCustomDestinationGetNetsValues, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	withCustomDestinationGetNetsValues.ClusterNetworks = "10.0.0.0/8,100.64.0.0/10,172.0.0.0/8"
	addFakeTLSSecrets(withCustomDestinationGetNetsValues)

	testCases := []struct {
		values         *charts.Values
		goldenFileName string
		options        values.Options
	}{
		{defaultValues, "install_default.golden", values.Options{}},
		{metaValues, "install_output.golden", values.Options{}},
		{haValues, "install_ha_output.golden", values.Options{}},
		{haWithOverridesValues, "install_ha_with_overrides_output.golden", values.Options{}},
		{cniEnabledValues, "install_no_init_container.golden", values.Options{}},
		{withProxyIgnoresValues, "install_proxy_ignores.golden", values.Options{}},
		{withHeartBeatDisabledValues, "install_heartbeat_disabled_output.golden", values.Options{}},
		{withControlPlaneTracingValues, "install_controlplane_tracing_output.golden", values.Options{}},
		{withCustomRegistryValues, "install_custom_registry.golden", values.Options{}},
		{withCustomDestinationGetNetsValues, "install_default_override_dst_get_nets.golden", values.Options{}},
		{defaultValues, "install_custom_domain.golden", values.Options{}},
		{defaultValues, "install_values_file.golden", values.Options{ValueFiles: []string{filepath.Join("testdata", "install_config.yaml")}}},
		{defaultValues, "install_default_token.golden", values.Options{Values: []string{"identity.serviceAccountTokenProjection=false"}}},
		{gidValues, "install_gid_output.golden", values.Options{}},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			valuesOverrides, err := tc.options.MergeValues(nil)
			if err != nil {
				t.Fatalf("Failed to get values overrides: %v", err)
			}
			var buf bytes.Buffer
			if err := renderControlPlane(&buf, tc.values, valuesOverrides, "yaml"); err != nil {
				t.Fatalf("Failed to render templates: %v", err)
			}

			if err := testDataDiffer.DiffTestYAML(tc.goldenFileName, buf.String()); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestIgnoreCluster(t *testing.T) {
	defaultValues, err := testInstallOptions()
	if err != nil {
		t.Fatal(err)
	}
	addFakeTLSSecrets(defaultValues)

	var buf bytes.Buffer
	if err := installControlPlane(context.Background(), nil, &buf, defaultValues, nil, values.Options{}, "yaml"); err != nil {
		t.Fatal(err)
	}
}

func TestGWApi(t *testing.T) {
	externalGatewayAPIManifest := `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: httproutes.gateway.networking.k8s.io
spec:
  versions:
    - name: v1
`

	unsupportedGatewayAPIVersionManifest := `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: httproutes.gateway.networking.k8s.io
spec:
  versions:
    - name: v1alpha1
`

	linkerdGatewayAPIManifest := `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: httproutes.gateway.networking.k8s.io
  annotations:
    linkerd.io/created-by: linkerd
`

	testCases := []struct {
		name        string
		resources   string
		values      values.Options
		expectError bool
		goldenFile  string
	}{
		{
			"render with external GW API",
			externalGatewayAPIManifest,
			values.Options{},
			false,
			"install_crds.golden",
		},
		{
			"error with unsupported external GW API",
			unsupportedGatewayAPIVersionManifest,
			values.Options{},
			true,
			"",
		},
		{
			"render with missing GW API",
			"",
			values.Options{},
			true,
			"",
		},
		{
			"render with GW API (installGatewayAPI flag)",
			"",
			values.Options{
				Values: []string{"installGatewayAPI=true"},
			},
			false,
			"install_crds_with_gateway_api.golden",
		},
		{
			"render with GW API (lagecy flags)",
			"",
			values.Options{
				Values: []string{
					"enableHttpRoutes=true",
					"enableTlsRoutes=true",
					"enableTcpRoutes=true",
				},
			},
			false,
			"install_crds_with_gateway_api.golden",
		},
		{
			"render with conflicting GW API (installGatewayAPI flag)",
			externalGatewayAPIManifest,
			values.Options{
				Values: []string{"installGatewayAPI=true"},
			},
			true,
			"",
		},
		{
			"render with conflicting GW API (legacy flags)",
			externalGatewayAPIManifest,
			values.Options{
				Values: []string{
					"enableHttpRoutes=true",
					"enableTlsRoutes=true",
					"enableTcpRoutes=true",
				},
			},
			true,
			"",
		},
		{
			"error on attempt to remove a Linkerd managed version via installGatewayAPI=false",
			linkerdGatewayAPIManifest,
			values.Options{
				Values: []string{"installGatewayAPI=false"},
			},
			true,
			"",
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.name, func(t *testing.T) {
			k, err := k8s.NewFakeAPIFromManifests([]io.Reader{strings.NewReader(tc.resources)})
			if err != nil {
				t.Fatalf("failed to initialize fake API: %s", err)
			}

			var buf bytes.Buffer
			err = renderCRDs(context.Background(), k, &buf, tc.values, "yaml")
			if err != nil {
				if tc.expectError {
					return
				}
				t.Fatalf("Failed to render templates: %v", err)
			}

			if tc.expectError && err == nil {
				t.Fatal("an error was expected")

			}

			if err := testDataDiffer.DiffTestYAML(tc.goldenFile, buf.String()); err != nil {
				t.Error(err)
			}
		})
	}

}

func TestValidateAndBuild_Errors(t *testing.T) {
	t.Run("Fails validation for invalid ignoreInboundPorts", func(t *testing.T) {
		values, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}
		values.ProxyInit.IgnoreInboundPorts = "-25"
		err = validateValues(context.Background(), nil, values)
		if err == nil {
			t.Fatal("expected error but got nothing")
		}
	})

	t.Run("Fails validation for invalid ignoreOutboundPorts", func(t *testing.T) {
		values, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}
		values.ProxyInit.IgnoreOutboundPorts = "-25"
		err = validateValues(context.Background(), nil, values)
		if err == nil {
			t.Fatal("expected error but got nothing")
		}
	})
}

func testInstallOptionsFakeCerts() (*charts.Values, error) {
	values, err := testInstallOptions()
	if err != nil {
		return nil, err
	}
	addFakeTLSSecrets(values)
	return values, nil
}

func testInstallOptions() (*charts.Values, error) {
	return testInstallOptionsHA(false)
}

func testInstallOptionsHA(ha bool) (*charts.Values, error) {
	values, err := testInstallOptionsNoCerts(ha)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join("testdata", "valid-crt.pem"))
	if err != nil {
		return nil, err
	}

	crt, err := tls.DecodePEMCrt(string(data))
	if err != nil {
		return nil, err
	}
	values.Identity.Issuer.TLS.CrtPEM = crt.EncodeCertificatePEM()

	key, err := loadKeyPEM(filepath.Join("testdata", "valid-key.pem"))
	if err != nil {
		return nil, err
	}
	values.Identity.Issuer.TLS.KeyPEM = key

	data, err = os.ReadFile(filepath.Join("testdata", "valid-trust-anchors.pem"))
	if err != nil {
		return nil, err
	}
	values.IdentityTrustAnchorsPEM = string(data)

	return values, nil
}

func testInstallOptionsNoCerts(ha bool) (*charts.Values, error) {
	values, err := charts.NewValues()
	if err != nil {
		return nil, err
	}
	if ha {
		if err = charts.MergeHAValues(values); err != nil {
			return nil, err
		}
	}

	values.Proxy.Image.Version = installProxyVersion
	values.DebugContainer.Image.Version = installDebugVersion
	values.LinkerdVersion = installControlPlaneVersion
	values.HeartbeatSchedule = fakeHeartbeatSchedule()

	return values, nil
}

func testInstallValues() (*charts.Values, error) {
	values, err := charts.NewValues()
	if err != nil {
		return nil, err
	}

	values.Proxy.Image.Version = installProxyVersion
	values.DebugContainer.Image.Version = installDebugVersion
	values.LinkerdVersion = installControlPlaneVersion
	values.HeartbeatSchedule = fakeHeartbeatSchedule()

	identityCert, err := os.ReadFile(filepath.Join("testdata", "valid-crt.pem"))
	if err != nil {
		return nil, err
	}
	identityKey, err := os.ReadFile(filepath.Join("testdata", "valid-key.pem"))
	if err != nil {
		return nil, err
	}
	trustAnchorsPEM, err := os.ReadFile(filepath.Join("testdata", "valid-trust-anchors.pem"))
	if err != nil {
		return nil, err
	}

	values.Identity.Issuer.TLS.CrtPEM = string(identityCert)
	values.Identity.Issuer.TLS.KeyPEM = string(identityKey)
	values.IdentityTrustAnchorsPEM = string(trustAnchorsPEM)
	return values, nil
}

func TestValidate(t *testing.T) {
	t.Run("Accepts the default options as valid", func(t *testing.T) {
		values, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		if err := validateValues(context.Background(), nil, values); err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	})

	t.Run("Rejects invalid destination networks", func(t *testing.T) {
		values, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		values.ClusterNetworks = "wrong"
		expected := "cannot parse destination get networks: invalid CIDR address: wrong"

		err = validateValues(context.Background(), nil, values)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != expected {
			t.Fatalf("Expected error string\"%s\", got \"%s\"", expected, err)
		}
	})

	t.Run("Rejects invalid controller log level", func(t *testing.T) {
		values, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		values.ControllerLogLevel = "super"
		expected := "--controller-log-level must be one of: panic, fatal, error, warn, info, debug, trace"

		err = validateValues(context.Background(), nil, values)
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
			{"off", true},
			{"info", true},
			{"somemodule", true},
			{"bad%name", false},
			{"linkerd=debug", true},
			{"linkerd2%proxy=debug", false},
			{"linkerd=foobar", false},
			{"linker2d_proxy,std::option", true},
			{"warn,linkerd=info", true},
			{"warn,linkerd=foobar", false},
		}

		values, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		for _, tc := range testCases {
			values.Proxy.LogLevel = tc.input
			err := validateValues(context.Background(), nil, values)
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
			{"valid-with-rsa-anchor", ""},
			{"expired", "failed to validate issuer credentials: not valid anymore. Expired on 1990-01-01T01:01:11Z"},
			{"not-valid-yet", "failed to validate issuer credentials: not valid before: 2100-01-01T01:00:51Z"},
			{"wrong-algo", "failed to validate issuer credentials: must use P-256 curve for public key, instead P-521 was used"},
		}
		for _, tc := range testCases {

			values, err := testInstallOptions()
			if err != nil {
				t.Fatalf("Unexpected error: %v\n", err)
			}

			crt, err := loadCrtPEM(filepath.Join("testdata", tc.crtFilePrefix+"-crt.pem"))
			if err != nil {
				t.Fatal(err)
			}
			values.Identity.Issuer.TLS.CrtPEM = crt

			key, err := loadKeyPEM(filepath.Join("testdata", tc.crtFilePrefix+"-key.pem"))
			if err != nil {
				t.Fatal(err)
			}
			values.Identity.Issuer.TLS.KeyPEM = key

			ca, err := os.ReadFile(filepath.Join("testdata", tc.crtFilePrefix+"-trust-anchors.pem"))
			if err != nil {
				t.Fatal(err)
			}
			values.IdentityTrustAnchorsPEM = string(ca)

			err = validateValues(context.Background(), nil, values)

			if tc.expectedError != "" {
				if err == nil {
					t.Fatal("Expected error, got nothing")
				}
				if err.Error() != tc.expectedError {
					t.Fatalf("Expected error string\"%s\", got \"%s\"", tc.expectedError, err)
				}
			} else if err != nil {
				t.Fatalf("Expected no error but got \"%s\"", err)
			}
		}
	})

	t.Run("Rejects identity cert files data when external issuer is set", func(t *testing.T) {

		values, err := testInstallOptionsNoCerts(false)
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}

		values.Identity.Issuer.Scheme = string(corev1.SecretTypeTLS)

		withoutCertDataOptions, _ := values.DeepCopy()

		withCrtFile, _ := values.DeepCopy()
		withCrtFile.Identity.Issuer.TLS.CrtPEM = "certificate"

		withKeyFile, _ := values.DeepCopy()
		withKeyFile.Identity.Issuer.TLS.KeyPEM = "key"

		testCases := []struct {
			input         *charts.Values
			expectedError string
		}{
			{withoutCertDataOptions, ""},
			{withCrtFile, "--identity-issuer-certificate-file must not be specified if --identity-external-issuer=true"},
			{withKeyFile, "--identity-issuer-key-file must not be specified if --identity-external-issuer=true"},
		}

		for _, tc := range testCases {
			err = validateValues(context.Background(), nil, tc.input)

			if tc.expectedError != "" {
				if err == nil {
					t.Fatalf("Expected error '%s', got nothing", tc.expectedError)
				}
				if err.Error() != tc.expectedError {
					t.Fatalf("Expected error string\"%s\", got \"%s\"", tc.expectedError, err)
				}
			} else if err != nil {
				t.Fatalf("Expected no error but got \"%s\"", err)
			}
		}
	})

	t.Run("Rejects invalid default-inbound-policy", func(t *testing.T) {
		values, err := testInstallOptions()
		if err != nil {
			t.Fatalf("Unexpected error: %v\n", err)
		}
		values.Proxy.DefaultInboundPolicy = "everybody"
		expected := "--default-inbound-policy must be one of: all-authenticated, all-unauthenticated, cluster-authenticated, cluster-unauthenticated, deny, audit (got everybody)"

		err = validateValues(context.Background(), nil, values)
		if err == nil {
			t.Fatal("Expected error, got nothing")
		}
		if err.Error() != expected {
			t.Fatalf("Expected error string \"%s\", got \"%s\"", expected, err)
		}
	})
}

func fakeHeartbeatSchedule() string {
	return "1 2 3 4 5"
}

func addFakeTLSSecrets(values *charts.Values) {
	values.ProxyInjector.ExternalSecret = true
	values.ProxyInjector.CaBundle = "proxy injector CA bundle"
	values.ProxyInjector.InjectCaFrom = ""
	values.ProxyInjector.InjectCaFromSecret = ""
	values.ProfileValidator.ExternalSecret = true
	values.ProfileValidator.CaBundle = "profile validator CA bundle"
	values.ProfileValidator.InjectCaFrom = ""
	values.ProfileValidator.InjectCaFromSecret = ""
	values.PolicyValidator.ExternalSecret = true
	values.PolicyValidator.CaBundle = "policy validator CA bundle"
	values.PolicyValidator.InjectCaFrom = ""
	values.PolicyValidator.InjectCaFromSecret = ""
}
