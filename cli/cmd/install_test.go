package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-openapi/testify/v2/assert"
	"github.com/linkerd/linkerd2/cli/flag"
	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"helm.sh/helm/v3/pkg/cli/values"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	installProxyVersion        = "install-proxy-version"
	installControlPlaneVersion = "install-control-plane-version"
	installDebugVersion        = "install-debug-version"

	externalGatewayAPIManifest = `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: httproutes.gateway.networking.k8s.io
spec:
  versions:
    - name: v1
`
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
		Controller:              defaultValues.Controller,
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
			Tracing: &charts.Tracing{
				Enabled: false,
				Labels: map[string]string{
					"k8s.pod.ip":         "$(_pod_ip)",
					"k8s.pod.uid":        "$(_pod_uid)",
					"k8s.container.name": "$(_pod_containerName)",
				},
				TraceServiceName: "linkerd-proxy",
				Collector: &charts.TracingCollector{
					Endpoint: "",
					MeshIdentity: &charts.TracingCollectorIdentity{
						ServiceAccountName: "",
						Namespace:          "",
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
			IptablesMode:        "legacy",
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
	withControlPlaneTracingValues.Controller.Tracing = &charts.Tracing{
		Enabled: true,
		Collector: &charts.TracingCollector{
			Endpoint: "tracing.foo:4317",
		},
	}
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

	tracingValues, err := testInstallOptions()
	if err != nil {
		t.Fatalf("Unexpected error: %v\n", err)
	}
	tracingValues.Proxy.Tracing.Enabled = true
	tracingValues.Proxy.Tracing.Collector.Endpoint = "tracing.foo:4317"
	tracingValues.Proxy.Tracing.Collector.MeshIdentity.ServiceAccountName = "default"
	tracingValues.Proxy.Tracing.Collector.MeshIdentity.Namespace = "foo"
	addFakeTLSSecrets(tracingValues)

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
		{tracingValues, "install_tracing.golden", values.Options{}},
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

// TestOverrideIssuer calls install control plane with the goal of testing
// options overrides for initialize issuer credentials.
func TestOverrideIssuer(t *testing.T) {
	removeIssuerCrt := func() (*charts.Values, error) {
		t.Helper()
		values, err := testInstallOptionsFakeCerts()
		if err != nil {
			return nil, err
		}
		values.Identity.Issuer.TLS.CrtPEM = ""
		return values, nil
	}
	removeIssuerKey := func() (*charts.Values, error) {
		t.Helper()
		values, err := testInstallOptionsFakeCerts()
		if err != nil {
			return nil, err
		}
		values.Identity.Issuer.TLS.KeyPEM = ""
		return values, nil
	}
	removeTrustAnchor := func() (*charts.Values, error) {
		t.Helper()
		values, err := testInstallOptionsFakeCerts()
		if err != nil {
			return nil, err
		}
		values.IdentityTrustAnchorsPEM = ""
		return values, nil
	}
	assert := assert.New(t)
	read := func(filename string) []byte {
		t.Helper()
		data, err := os.ReadFile(path.Join("testdata", filename))
		if assert.NoError(err, "cannot read-file filename=%s", filename) {
			return data
		}
		return nil
	}
	// newK8S returns a test implementation of the k8s API; after setting the
	// issuer trust anchor and tls crt+key as a secret.
	newK8S := func(opts values.Options) *k8s.KubernetesAPI {
		t.Helper()
		buf := &bytes.Buffer{}
		err := renderCRDs(context.Background(), nil, buf, opts, "yaml")
		assert.NoError(err, "cannot render-crds for new-k8s-api opts=%+v", opts)
		api, err := k8s.NewFakeAPIFromManifests([]io.Reader{buf})
		if assert.NoError(err, "cannot create k8s api from manifests") {
			_, err = api.CoreV1().Secrets(controlPlaneNamespace).Create(context.Background(),
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      k8s.IdentityIssuerSecretName,
						Namespace: controlPlaneNamespace,
					},
					Data: map[string][]byte{
						k8s.IdentityIssuerTrustAnchorsNameExternal: read("valid-trust-anchors.pem"),
						corev1.TLSCertKey:                          read("valid-crt.pem"),
						corev1.TLSPrivateKeyKey:                    read("valid-key.pem"),
					}}, metav1.CreateOptions{})
			if assert.NoError(err, "cannot create secrets for new-k8s-api") {
				return api
			}
		}
		return nil
	}
	controlPlaneNamespace = defaultLinkerdNamespace
	for i, test := range []struct {
		options                values.Options
		values                 func() (*charts.Values, error)
		k8sAPI                 *k8s.KubernetesAPI
		expErr                 string
		expIdentityTrustAnchor bool
		expIssuerCrt           bool
		expIssuerKey           bool
		expIssuerName          string
	}{
		{
			// no options; no certs in values -> generated anchor; key + crt
			options:                values.Options{},
			values:                 testInstallValuesNoCertsNoHA,
			k8sAPI:                 nil,
			expIdentityTrustAnchor: true,
			expIssuerKey:           true,
			expIssuerCrt:           true,
			expIssuerName: fmt.Sprintf("identity.%s.%s",
				controlPlaneNamespace, "test-override-issuer"),
		},
		{
			// no options; fake certs in values -> fake certs untouched
			options:                values.Options{},
			values:                 testInstallOptionsFakeCerts,
			k8sAPI:                 nil,
			expIdentityTrustAnchor: true,
			expIssuerKey:           true,
			expIssuerCrt:           true,
			expIssuerName:          "identity.linkerd.cluster.local",
		},
		{
			// issuer scheme in options; no certs in values; nil k8s api ->
			// error trying to call k8s
			options: values.Options{
				Values: []string{"identity.issuer.scheme=kubernetes.io/tls"},
			},
			values:                 testInstallValuesNoCertsNoHA,
			k8sAPI:                 nil,
			expErr:                 "--ignore-cluster is not supported when --identity-external-issuer=true",
			expIdentityTrustAnchor: false,
			expIssuerKey:           false,
			expIssuerCrt:           false,
			expIssuerName:          "",
		},
		{
			// issuer scheme in options; no certs in values; fake k8s api ->
			// trust anchor is set
			options: values.Options{
				Values: []string{"identity.issuer.scheme=kubernetes.io/tls"},
			},
			values:                 testInstallValuesNoCertsNoHA,
			k8sAPI:                 newK8S(values.Options{}),
			expErr:                 "",
			expIdentityTrustAnchor: true,
			expIssuerKey:           false,
			expIssuerCrt:           false,
			expIssuerName:          "identity.linkerd.cluster.local",
		},
		{
			// no options; fake certs in values; remove trust anchor -> err
			options:                values.Options{},
			values:                 removeTrustAnchor,
			k8sAPI:                 nil,
			expErr:                 "a trust anchors file must be specified if other credentials are provided",
			expIdentityTrustAnchor: false,
			expIssuerCrt:           true,
			expIssuerKey:           true,
			expIssuerName:          "identity.linkerd.cluster.local",
		},
		{
			// no options; fake certs in values; remove issuer crt -> err
			options:                values.Options{},
			values:                 removeIssuerCrt,
			k8sAPI:                 nil,
			expErr:                 "a certificate file must be specified if other credentials are provided",
			expIdentityTrustAnchor: true,
			expIssuerCrt:           false,
			expIssuerName:          "identity.linkerd.cluster.local",
			expIssuerKey:           true,
		},
		{
			// no options; fake certs in values; remove issuer key -> err
			options:                values.Options{},
			values:                 removeIssuerKey,
			k8sAPI:                 nil,
			expErr:                 "a private key file must be specified if other credentials are provided",
			expIdentityTrustAnchor: true,
			expIssuerCrt:           true,
			expIssuerName:          "identity.linkerd.cluster.local",
			expIssuerKey:           false,
		},
	} {
		values, err := test.values()
		assert.NoError(err, "%02d/test install options failed with an error", i)
		values.IdentityTrustDomain = "test-override-issuer"
		// ensure the install options created above meet expectations (we are
		// testing the override not the values)
		assert.Equal(k8s.IdentityIssuerSchemeLinkerd, values.Identity.Issuer.Scheme)
		var buf bytes.Buffer
		err = installControlPlane(context.Background(), test.k8sAPI, &buf, values, nil, test.options, "yaml")
		if test.expErr != "" {
			assert.EqualError(err, test.expErr, "%02d/install control plane returned incorrect error", i)
		} else {
			assert.NoError(err, "%02d/install control plane failed with an error", i)
		}
		if test.expIdentityTrustAnchor {
			assert.NotEmpty(t, values.IdentityTrustAnchorsPEM, "%02d/identity trust anchor is not set", i)
			crt, err := tls.DecodePEMCrt(values.IdentityTrustAnchorsPEM)
			assert.NoError(err, "%02d/generated identity-trust-anchors-pem cannot be decoded", i)
			assert.NotNil(crt, "%02d/generated identity-trust-anchors-pem cannot be decoded (nil)", i)
			assert.NotNil(crt.Certificate, "%02d/generated identity-trust-anchors-pem certificate is invalid", i)
			assert.Equal(
				test.expIssuerName,
				crt.Certificate.Issuer.CommonName,
				"%02/generated identity-trust-anchors-pem certificate common-name is incorrect", i)
		} else {
			assert.Empty(values.IdentityTrustAnchorsPEM, "%02d/identity was incorrectly set", i)
		}
		if test.expIssuerCrt {
			assert.NotEmpty(values.Identity.Issuer.TLS.CrtPEM, "%02d/identity issuer crt is not set", i)
			assert.NotEmpty(values.Identity.Issuer.TLS.CrtPEM, "%02d/generated identity-issuer-tls-crt-pem is empty", i)
			crt, err := tls.DecodePEMCrt(values.Identity.Issuer.TLS.CrtPEM)
			assert.NoError(err, "%02d/generated identity-issuer-tls-crt-pem cannot be decoded", i)
			assert.NotNil(crt, "%02d/generated identity-issuer-tls-crt-pem cannot be decoded (nil)", i)
			assert.NotNil(crt.Certificate, "%02d/generated identity-issuer-tls-crt-pem certificate is invalid", i)
		} else {
			assert.Empty(values.Identity.Issuer.TLS.CrtPEM, "%02d/identity issuer crt was incorrectly set", i)
		}
		if test.expIssuerKey {
			assert.NotEmpty(values.Identity.Issuer.TLS.KeyPEM, "%02d/identity issuer tls key is not set", i)
			assert.NotEmpty(values.Identity.Issuer.TLS.KeyPEM, "%02d/generated identity-issuer-tls-key-pem is empty", i)
			key, err := tls.DecodePEMKey(values.Identity.Issuer.TLS.KeyPEM)
			assert.NoError(err, "%02d/generated identity-issuer-tls-key-pem cannot be decoded", i)
			assert.NotNil(key, "%02d/generated identity-issuer-tls-key-pem cannot be decoded (nil)", i)
		} else {
			assert.Empty(values.Identity.Issuer.TLS.KeyPEM, "%02d/identity issuer tls key was incorrectly set", i)
		}
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

func testInstallValuesNoCertsNoHA() (*charts.Values, error) {
	return testInstallOptionsNoCerts(false)
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
