package cmd

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
)

const (
	upgradeProxyVersion        = "UPGRADE-PROXY-VERSION"
	upgradeControlPlaneVersion = "UPGRADE-CONTROL-PLANE-VERSION"
	upgradeDebugVersion        = "UPGRADE-DEBUG-VERSION"
)

type (
	issuerCerts struct {
		caFile  string
		ca      string
		crtFile string
		crt     string
		keyFile string
		key     string
	}

	manifest = map[string]interface{}

	diff struct {
		path []string
		a    interface{}
		b    interface{}
	}
)

/* Test cases */

/* Most test cases in this file work by first rendering an install manifest
   list, creating a fake k8s client initialized with those manifests, rendering
   an upgrade manifest list, and comparing the install manifests to the upgrade
   manifests. In some cases we expect these manifests to be identical and in
   others there are certain expected differences */

func TestUpgradeDefault(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)
	install, upgrade, err := renderInstallAndUpgrade(t, installOpts, installFlags, upgradeOpts, upgradeFlags)
	if err != nil {
		t.Fatal(err)
	}
	// Install and upgrade manifests should be identical except for the version.
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	for id, diffs := range diffManifestLists(expectedManifests, upgradeManifests) {
		for _, diff := range diffs {
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeHA(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)
	installFlags.Set("ha", "true")
	install, upgrade, err := renderInstallAndUpgrade(t, installOpts, installFlags, upgradeOpts, upgradeFlags)
	if err != nil {
		t.Fatal(err)
	}
	// Install and upgrade manifests should be identical except for the version.
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	for id, diffs := range diffManifestLists(expectedManifests, upgradeManifests) {
		for _, diff := range diffs {
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeExternalIssuer(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	issuer := generateIssuerCerts(t, true)

	identity := identityWithAnchorsAndTrustDomain{
		TrustDomain:     "cluster.local",
		TrustAnchorsPEM: issuer.ca,
		Identity: &linkerd2.Identity{
			Issuer: &linkerd2.Issuer{
				Scheme: string(corev1.SecretTypeTLS),
				TLS: &linkerd2.IssuerTLS{
					CrtPEM: issuer.crt,
					KeyPEM: issuer.key,
				},
			},
		},
	}
	installOpts.recordFlags(installFlags)
	values, _, err := installOpts.validateAndBuildWithIdentity("", &identity)
	install := renderInstall(t, values)
	if err != nil {
		t.Fatalf("Failed to build install values: %s", err)
	}
	upgrade, err := renderUpgrade(t, install.String()+externalIssuerSecret(issuer), upgradeOpts, upgradeFlags)

	if err != nil {
		t.Fatal(err)
	}
	// Install and upgrade manifests should be identical except for the version.
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	for id, diffs := range diffManifestLists(expectedManifests, upgradeManifests) {
		for _, diff := range diffs {
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeIssuerWithExternalIssuerFails(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	issuer := generateIssuerCerts(t, true)

	identity := identityWithAnchorsAndTrustDomain{
		TrustDomain:     "cluster.local",
		TrustAnchorsPEM: issuer.ca,
		Identity: &linkerd2.Identity{
			Issuer: &linkerd2.Issuer{
				Scheme: string(corev1.SecretTypeTLS),
				TLS: &linkerd2.IssuerTLS{
					CrtPEM: issuer.crt,
					KeyPEM: issuer.key,
				},
			},
		},
	}
	installOpts.recordFlags(installFlags)
	values, _, err := installOpts.validateAndBuildWithIdentity("", &identity)
	install := renderInstall(t, values)
	if err != nil {
		t.Fatalf("Failed to build install values: %s", err)
	}

	upgradedIssuer := generateIssuerCerts(t, true)
	upgradeFlags.Set("identity-trust-anchors-file", upgradedIssuer.caFile)
	upgradeFlags.Set("identity-issuer-certificate-file", upgradedIssuer.crtFile)
	upgradeFlags.Set("identity-issuer-key-file", upgradedIssuer.keyFile)

	_, err = renderUpgrade(t, install.String()+externalIssuerSecret(issuer), upgradeOpts, upgradeFlags)

	expectedErr := "cannot update issuer certificates if you are using external cert management solution"

	if err == nil || err.Error() != expectedErr {
		t.Errorf("Expected error: %s but got %s", expectedErr, err)
	}
}

func TestUpgradeOverwriteIssuer(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	issuerCerts := generateIssuerCerts(t, true)

	upgradeFlags.Set("identity-trust-anchors-file", issuerCerts.caFile)
	upgradeFlags.Set("identity-issuer-certificate-file", issuerCerts.crtFile)
	upgradeFlags.Set("identity-issuer-key-file", issuerCerts.keyFile)
	install, upgrade, err := renderInstallAndUpgrade(t, installOpts, installFlags, upgradeOpts, upgradeFlags)
	if err != nil {
		t.Fatal(err)
	}
	// When upgrading the trust root, we expect to see the new trust root passed
	// to each proxy, the trust root updated in the linkerd-config, and the
	// updated credentials in the linkerd-identity-issuer secret.
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	for id, diffs := range diffManifestLists(expectedManifests, upgradeManifests) {
		for _, diff := range diffs {
			if isProxyEnvDiff(diff.path) {
				continue
			}
			if id == "ConfigMap/linkerd-config" {
				continue
			}
			if id == "Secret/linkerd-identity-issuer" {
				if pathMatch(diff.path, []string{"data", "crt.pem"}) {
					if diff.b.(string) != issuerCerts.crt {
						diff.a = issuerCerts.crt
						t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
					}
				} else if pathMatch(diff.path, []string{"data", "key.pem"}) {
					if diff.b.(string) != issuerCerts.key {
						diff.a = issuerCerts.key
						t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
					}
				} else if pathMatch(diff.path, []string{"metadata", "annotations", "linkerd.io/identity-issuer-expiry"}) {
				} else {
					t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
				}
				continue
			}
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeFailsWithOnlyIssuerCert(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	issuerCerts := generateIssuerCerts(t, true)
	upgradeOpts.identityOptions.identityExternalIssuer = true
	upgradeFlags.Set("identity-trust-anchors-file", issuerCerts.caFile)
	upgradeFlags.Set("identity-issuer-certificate-file", issuerCerts.crtFile)
	_, _, err := renderInstallAndUpgrade(t, installOpts, installFlags, upgradeOpts, upgradeFlags)

	expectedErr := "a private key file must be specified if a certificate is provided"

	if err == nil || err.Error() != expectedErr {
		t.Errorf("Expected error: %s but got %s", expectedErr, err)
	}
}

func TestUpgradeFailsWithOnlyIssuerKey(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	issuerCerts := generateIssuerCerts(t, true)
	upgradeOpts.identityOptions.identityExternalIssuer = true
	upgradeFlags.Set("identity-trust-anchors-file", issuerCerts.caFile)
	upgradeFlags.Set("identity-issuer-key-file", issuerCerts.keyFile)
	_, _, err := renderInstallAndUpgrade(t, installOpts, installFlags, upgradeOpts, upgradeFlags)

	expectedErr := "a certificate file must be specified if a private key is provided"

	if err == nil || err.Error() != expectedErr {
		t.Errorf("Expected error: %s but got %s", expectedErr, err)
	}
}

func TestUpgradeRootFailsWithOldPods(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	oldIssuer := generateIssuerCerts(t, false)

	install := renderInstall(t, installValues(t, installOpts, installFlags))

	issuerCerts := generateIssuerCerts(t, true)
	upgradeFlags.Set("identity-trust-anchors-file", issuerCerts.caFile)
	upgradeFlags.Set("identity-issuer-key-file", issuerCerts.keyFile)
	upgradeFlags.Set("identity-issuer-certificate-file", issuerCerts.crtFile)
	_, err := renderUpgrade(t, install.String()+podWithSidecar(oldIssuer), upgradeOpts, upgradeFlags)

	expectedErr := "You are attempting to use an issuer certificate which does not validate against the trust anchors of the following pods"
	if err == nil || !strings.HasPrefix(err.Error(), expectedErr) {
		t.Errorf("Expected error: %s but got %s", expectedErr, err)
	}
}

func TestUpgradeTracingAddon(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	upgradeOpts.addOnConfig = filepath.Join("testdata", "addon_config.yaml")
	install, upgrade, err := renderInstallAndUpgrade(t, installOpts, installFlags, upgradeOpts, upgradeFlags)
	if err != nil {
		t.Fatal(err)
	}
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	diffMap := diffManifestLists(expectedManifests, upgradeManifests)
	tracingManifests := []string{
		"Service/linkerd-jaeger", "Deployment/linkerd-jaeger", "ConfigMap/linkerd-config-addons",
		"ServiceAccount/linkerd-jaeger", "Service/linkerd-collector", "ConfigMap/linkerd-collector-config",
		"ServiceAccount/linkerd-collector", "Deployment/linkerd-collector",
	}
	for _, id := range tracingManifests {
		if _, ok := diffMap[id]; ok {
			delete(diffMap, id)
		} else {
			t.Errorf("Expected %s in upgrade output but was absent", id)
		}
	}
	for id, diffs := range diffMap {
		for _, diff := range diffs {
			if id == "Deployment/linkerd-web" && pathMatch(diff.path, []string{"spec", "template", "spec", "containers", "*", "args"}) {
				continue
			}
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeOverwriteTracingAddon(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	installOpts.addOnConfig = filepath.Join("testdata", "addon_config.yaml")
	upgradeOpts.addOnConfig = filepath.Join("testdata", "addon_config_overwrite.yaml")
	upgradeOpts.traceCollector = "overwrite-collector"
	upgradeOpts.traceCollectorSvcAccount = "overwrite-collector.default"
	install, upgrade, err := renderInstallAndUpgrade(t, installOpts, installFlags, upgradeOpts, upgradeFlags)
	if err != nil {
		t.Fatal(err)
	}
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	diffMap := diffManifestLists(expectedManifests, upgradeManifests)
	tracingManifests := []string{
		"ConfigMap/linkerd-config-addons",
		"Service/overwrite-collector", "ConfigMap/overwrite-collector-config",
		"ServiceAccount/overwrite-collector", "Deployment/overwrite-collector",
		"Service/linkerd-collector", "ConfigMap/linkerd-collector-config",
		"ServiceAccount/linkerd-collector", "Deployment/linkerd-collector",
	}
	for _, id := range tracingManifests {
		if _, ok := diffMap[id]; ok {
			delete(diffMap, id)
		} else {
			t.Errorf("Expected %s in upgrade output diff but was absent", id)
		}
	}
	for id, diffs := range diffMap {
		for _, diff := range diffs {
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeTwoLevelWebhookCrts(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	// This tests the case where the webhook certs are not self-signed.
	values := installValues(t, installOpts, installFlags)
	injectorCerts := generateCerts(t, "linkerd-proxy-injector.linkerd.svc", false)
	values.ProxyInjector.TLS = &linkerd2.TLS{
		CaBundle: injectorCerts.ca,
		CrtPEM:   injectorCerts.crt,
		KeyPEM:   injectorCerts.key,
	}
	tapCerts := generateCerts(t, "linkerd-tap.linkerd.svc", false)
	values.Tap.TLS = &linkerd2.TLS{
		CaBundle: tapCerts.ca,
		CrtPEM:   tapCerts.crt,
		KeyPEM:   tapCerts.key,
	}
	validatorCerts := generateCerts(t, "linkerd-sp-validator.linkerd.svc", false)
	values.ProfileValidator.TLS = &linkerd2.TLS{
		CaBundle: validatorCerts.ca,
		CrtPEM:   validatorCerts.crt,
		KeyPEM:   validatorCerts.key,
	}

	install := renderInstall(t, values)
	upgrade, err := renderUpgrade(t, install.String(), upgradeOpts, upgradeFlags)
	if err != nil {
		t.Fatal(err)
	}
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	for id, diffs := range diffManifestLists(expectedManifests, upgradeManifests) {
		for _, diff := range diffs {
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeWithAddonDisabled(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	installOpts.addOnConfig = filepath.Join("testdata", "grafana_disabled.yaml")
	install, upgrade, err := renderInstallAndUpgrade(t, installOpts, installFlags, upgradeOpts, upgradeFlags)
	if err != nil {
		t.Fatal(err)
	}
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	for id, diffs := range diffManifestLists(expectedManifests, upgradeManifests) {
		for _, diff := range diffs {
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeEnableAddon(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	installOpts.addOnConfig = filepath.Join("testdata", "grafana_disabled.yaml")
	upgradeOpts.addOnConfig = filepath.Join("testdata", "grafana_enabled.yaml")
	install, upgrade, err := renderInstallAndUpgrade(t, installOpts, installFlags, upgradeOpts, upgradeFlags)
	if err != nil {
		t.Fatal(err)
	}
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	diffMap := diffManifestLists(expectedManifests, upgradeManifests)
	addonManifests := []string{
		"ServiceAccount/linkerd-grafana", "Deployment/linkerd-grafana", "Service/linkerd-grafana",
		"ConfigMap/linkerd-grafana-config", "ConfigMap/linkerd-config-addons",
	}
	for _, id := range addonManifests {
		if _, ok := diffMap[id]; ok {
			delete(diffMap, id)
		} else {
			t.Errorf("Expected %s in upgrade output but was absent", id)
		}
	}
	for id, diffs := range diffMap {
		for _, diff := range diffs {
			if id == "RoleBinding/linkerd-psp" && pathMatch(diff.path, []string{"subjects"}) {
				continue
			}
			if id == "Deployment/linkerd-web" && pathMatch(diff.path, []string{"spec", "template", "spec", "containers", "*", "args"}) {
				continue
			}
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeRemoveAddonKeys(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	installOpts.addOnConfig = filepath.Join("testdata", "grafana_enabled_resources.yaml")
	upgradeOpts.addOnConfig = filepath.Join("testdata", "grafana_enabled.yaml")
	install, upgrade, err := renderInstallAndUpgrade(t, installOpts, installFlags, upgradeOpts, upgradeFlags)
	if err != nil {
		t.Fatal(err)
	}
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	for id, diffs := range diffManifestLists(expectedManifests, upgradeManifests) {
		for _, diff := range diffs {
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeOverwriteRemoveAddonKeys(t *testing.T) {
	installOpts, installFlags, upgradeOpts, upgradeFlags := testOptionsAndFlags(t)

	installOpts.addOnConfig = filepath.Join("testdata", "grafana_enabled_resources.yaml")
	upgradeOpts.addOnConfig = filepath.Join("testdata", "grafana_enabled.yaml")
	upgradeOpts.addOnOverwrite = true
	install, upgrade, err := renderInstallAndUpgrade(t, installOpts, installFlags, upgradeOpts, upgradeFlags)
	if err != nil {
		t.Fatal(err)
	}
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	diffMap := diffManifestLists(expectedManifests, upgradeManifests)
	if _, ok := diffMap["ConfigMap/linkerd-config-addons"]; ok {
		delete(diffMap, "ConfigMap/linkerd-config-addons")
	} else {
		t.Error("Expected ConfigMap/linkerd-config-addons in upgrade output diff but was absent")
	}
	for id, diffs := range diffMap {
		for _, diff := range diffs {
			if id == "Deployment/linkerd-grafana" && pathMatch(diff.path, []string{"spec", "template", "spec", "containers", "*", "resources"}) {
				continue
			}
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

/* Helpers */

func testUpgradeOptions() (*upgradeOptions, error) {
	o, err := newUpgradeOptionsWithDefaults()
	if err != nil {
		return nil, err
	}

	o.controlPlaneVersion = upgradeControlPlaneVersion
	o.proxyVersion = upgradeProxyVersion
	o.debugImageVersion = upgradeDebugVersion
	o.heartbeatSchedule = fakeHeartbeatSchedule
	return o, nil
}

func testOptionsAndFlags(t *testing.T) (*installOptions, *pflag.FlagSet, *upgradeOptions, *pflag.FlagSet) {
	installOpts, err := testInstallOptions()
	if err != nil {
		t.Fatalf("failed to create install options: %s", err)
	}
	upgradeOpts, err := testUpgradeOptions()
	if err != nil {
		t.Fatalf("failed to create upgrade options: %s", err)
	}
	return installOpts, installOpts.recordableFlagSet(), upgradeOpts, upgradeOpts.recordableFlagSet()
}

func replaceVersions(manifest string) string {
	manifest = strings.ReplaceAll(manifest, installProxyVersion, upgradeProxyVersion)
	manifest = strings.ReplaceAll(manifest, installControlPlaneVersion, upgradeControlPlaneVersion)
	manifest = strings.ReplaceAll(manifest, installDebugVersion, upgradeDebugVersion)
	return manifest
}

func generateIssuerCerts(t *testing.T, b64encode bool) issuerCerts {
	return generateCerts(t, "issuer", b64encode)
}

func generateCerts(t *testing.T, name string, b64encode bool) issuerCerts {
	ca, err := tls.GenerateRootCAWithDefaults("test")
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := ca.GenerateCA(name, -1)
	if err != nil {
		t.Fatal(err)
	}
	caPem := strings.TrimSpace(issuer.Cred.EncodePEM())
	keyPem := strings.TrimSpace(issuer.Cred.EncodePrivateKeyPEM())
	crtPem := strings.TrimSpace(issuer.Cred.EncodeCertificatePEM())

	caFile, err := ioutil.TempFile("", "ca.*.pem")
	if err != nil {
		t.Fatal(err)
	}
	crtFile, err := ioutil.TempFile("", "crt.*.pem")
	if err != nil {
		t.Fatal(err)
	}
	keyFile, err := ioutil.TempFile("", "key.*.pem")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		os.Remove(caFile.Name())
		os.Remove(crtFile.Name())
		os.Remove(keyFile.Name())
	})

	_, err = caFile.Write([]byte(caPem))
	if err != nil {
		t.Fatal(err)
	}
	_, err = crtFile.Write([]byte(crtPem))
	if err != nil {
		t.Fatal(err)
	}
	_, err = keyFile.Write([]byte(keyPem))
	if err != nil {
		t.Fatal(err)
	}

	if b64encode {
		caPem = base64.StdEncoding.EncodeToString([]byte(caPem))
		crtPem = base64.StdEncoding.EncodeToString([]byte(crtPem))
		keyPem = base64.StdEncoding.EncodeToString([]byte(keyPem))
	}

	return issuerCerts{
		caFile:  caFile.Name(),
		ca:      caPem,
		crtFile: crtFile.Name(),
		crt:     crtPem,
		keyFile: keyFile.Name(),
		key:     keyPem,
	}
}

func externalIssuerSecret(certs issuerCerts) string {
	return fmt.Sprintf(`---
apiVersion: v1
kind: Secret
metadata:
  name: linkerd-identity-issuer
  namespace: linkerd
data:
  tls.crt: %s
  tls.key: %s
  ca.crt: %s
type: kubernetes.io/tls
`, certs.crt, certs.key, certs.ca)
}

func indentLines(s string, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func podWithSidecar(certs issuerCerts) string {
	return fmt.Sprintf(`---
apiVersion: v1
kind: Pod
metadata:
  annotations:
    linkerd.io/created-by: linkerd/cli some-version
    linkerd.io/identity-mode: default
    linkerd.io/proxy-version: some-version
  labels:
    linkerd.io/control-plane-ns: linkerd
  name: backend-wrong-anchors
  namespace: some-namespace
spec:
  containers:
  - env:
    - name: LINKERD2_PROXY_IDENTITY_TRUST_ANCHORS
      value: |
%s
    image: gcr.io/linkerd-io/proxy:some-version
    name: linkerd-proxy
`, indentLines(certs.ca, "        "))
}

func isProxyEnvDiff(path []string) bool {
	template := []string{"spec", "template", "spec", "containers", "*", "env", "*", "value"}
	return pathMatch(path, template)
}

func pathMatch(path []string, template []string) bool {
	if len(path) != len(template) {
		return false
	}
	for i, elem := range template {
		if elem != "*" && elem != path[i] {
			return false
		}
	}
	return true
}

func installValues(t *testing.T, installOpts *installOptions, installFlags *pflag.FlagSet) *linkerd2.Values {
	installValues, _, err := installOpts.validateAndBuild("", installFlags)
	if err != nil {
		t.Fatalf("Unexpected error validating install options: %v", err)
	}
	return installValues
}

func renderInstall(t *testing.T, values *linkerd2.Values) bytes.Buffer {
	var installBuf bytes.Buffer
	if err := render(&installBuf, values); err != nil {
		t.Fatalf("could not render install manifests: %s", err)
	}
	return installBuf
}

func renderUpgrade(t *testing.T, installManifest string, upgradeOpts *upgradeOptions, upgradeFlags *pflag.FlagSet) (bytes.Buffer, error) {
	manifests := splitManifests(installManifest)
	clientset, err := k8s.NewFakeAPI(manifests...)
	if err != nil {
		t.Fatalf("could not initialize fake k8s API: %s", err)
	}

	upgradeValues, err := upgradeOpts.validateAndBuild("", clientset, upgradeFlags)

	if err != nil {
		return bytes.Buffer{}, err
	}

	var upgradeBuf bytes.Buffer
	err = render(&upgradeBuf, upgradeValues)
	if err != nil {
		t.Fatalf("could not render upgrade configuration: %s", err)
	}

	return upgradeBuf, nil
}

func renderInstallAndUpgrade(t *testing.T, installOpts *installOptions, installFlags *pflag.FlagSet, upgradeOpts *upgradeOptions, upgradeFlags *pflag.FlagSet) (bytes.Buffer, bytes.Buffer, error) {
	installBuf := renderInstall(t, installValues(t, installOpts, installFlags))
	upgradeBuf, err := renderUpgrade(t, installBuf.String(), upgradeOpts, upgradeFlags)
	return installBuf, upgradeBuf, err
}

func splitManifests(manifest string) []string {
	manifests := strings.Split(manifest, "\n---\n")
	filtered := []string{}
	for _, m := range manifests {
		if !isManifestEmpty(m) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func isManifestEmpty(manifest string) bool {
	lines := strings.Split(manifest, "\n")
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") || line == "---" {
			continue
		}
		return false
	}
	return true
}

func manifestKey(m manifest) string {
	kind := m["kind"].(string)
	meta := m["metadata"].(map[string]interface{})
	name := meta["name"].(string)
	return fmt.Sprintf("%s/%s", kind, name)
}

func (d diff) String() string {
	expected, _ := yaml.Marshal(d.a)
	actual, _ := yaml.Marshal(d.b)
	return fmt.Sprintf("Diff at [%s]:\nExpected:\n%s\nActual:\n%s", d.path, string(expected), string(actual))
}

func parseManifestList(in string) map[string]manifest {
	manifestList := splitManifests(in)
	manifestMap := map[string]manifest{}
	for _, m := range manifestList {
		manifest := manifest{}
		yaml.Unmarshal([]byte(m), &manifest)
		manifestMap[manifestKey(manifest)] = manifest
	}
	return manifestMap
}

func diffManifest(a manifest, b manifest, path []string) []diff {
	diffs := []diff{}
	for k, v := range a {
		bv, bvExists := b[k]
		switch val := v.(type) {
		case manifest:
			if !bvExists {
				diffs = append(diffs, diff{
					path: extend(path, k),
					a:    val,
					b:    nil,
				})
			} else {
				bvm, ok := bv.(manifest)
				if !ok {
					diffs = append(diffs, diff{
						path: extend(path, k),
						a:    val,
						b:    bv,
					})
				} else {
					diffs = append(diffs, diffManifest(val, bvm, extend(path, k))...)
				}
			}
		case []interface{}:
			bva, ok := bv.([]interface{})
			if !ok {
				diffs = append(diffs, diff{
					path: extend(path, k),
					a:    val,
					b:    bv,
				})
			} else if len(val) != len(bva) {
				diffs = append(diffs, diff{
					path: extend(path, k),
					a:    val,
					b:    bva,
				})
			} else {
				diffs = append(diffs, diffArray(val, bva, extend(path, k))...)
			}
		default:
			if !bvExists {
				diffs = append(diffs, diff{
					path: extend(path, k),
					a:    val,
					b:    nil,
				})
			} else {
				if !reflect.DeepEqual(val, bv) {
					diffs = append(diffs, diff{
						path: extend(path, k),
						a:    val,
						b:    bv,
					})
				}
			}
		}
	}
	for k, v := range b {
		_, avExists := a[k]
		if !avExists {
			diffs = append(diffs, diff{
				path: extend(path, k),
				a:    nil,
				b:    v,
			})
		}
	}
	return diffs
}

func diffArray(a, b []interface{}, path []string) []diff {
	diffs := []diff{}
	for i, v := range a {
		switch aVal := v.(type) {
		case manifest:
			bm, ok := b[i].(manifest)
			if !ok {
				diffs = append(diffs, diff{
					path: extend(path, fmt.Sprintf("%d", i)),
					a:    aVal,
					b:    b[i],
				})
			} else {
				diffs = append(diffs, diffManifest(aVal, bm, extend(path, fmt.Sprintf("%d", i)))...)
			}
		case []interface{}:
			ba, ok := b[i].([]interface{})
			if !ok {
				diffs = append(diffs, diff{
					path: extend(path, fmt.Sprintf("%d", i)),
					a:    aVal,
					b:    b[i],
				})
			} else if len(aVal) != len(ba) {
				diffs = append(diffs, diff{
					path: extend(path, fmt.Sprintf("%d", i)),
					a:    aVal,
					b:    b[i],
				})
			} else {
				diffs = append(diffs, diffArray(aVal, ba, extend(path, fmt.Sprintf("%d", i)))...)
			}
		default:
			if !reflect.DeepEqual(v, b[i]) {
				diffs = append(diffs, diff{
					path: extend(path, fmt.Sprintf("%d", i)),
					a:    v,
					b:    b[i],
				})
			}
		}
	}
	return diffs
}

func diffManifestLists(a map[string]manifest, b map[string]manifest) map[string][]diff {
	diffs := map[string][]diff{}
	for k, am := range a {
		bm, ok := b[k]
		if !ok {
			diffs[k] = []diff{{
				a:    am,
				b:    nil,
				path: []string{},
			}}
		} else {
			diffs[k] = diffManifest(am, bm, []string{})
		}
	}
	for k, bm := range b {
		_, ok := a[k]
		if !ok {
			diffs[k] = []diff{{
				a:    nil,
				b:    bm,
				path: []string{},
			}}
		}
	}
	return diffs
}

func extend(slice []string, next string) []string {
	new := make([]string, len(slice)+1)
	copy(new, slice)
	new[len(slice)] = next
	return new
}
