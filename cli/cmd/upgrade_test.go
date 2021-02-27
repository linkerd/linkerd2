package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/cli/flag"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/spf13/pflag"
	valuespkg "helm.sh/helm/v3/pkg/cli/values"
	corev1 "k8s.io/api/core/v1"
)

const (
	upgradeProxyVersion        = "UPGRADE-PROXY-VERSION"
	upgradeControlPlaneVersion = "UPGRADE-CONTROL-PLANE-VERSION"
	upgradeDebugVersion        = "UPGRADE-DEBUG-VERSION"
	overridesSecret            = "Secret/linkerd-config-overrides"
	linkerdConfigMap           = "ConfigMap/linkerd-config"
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
)

/* Test cases */

/* Most test cases in this file work by first rendering an install manifest
   list, creating a fake k8s client initialized with those manifests, rendering
   an upgrade manifest list, and comparing the install manifests to the upgrade
   manifests. In some cases we expect these manifests to be identical and in
   others there are certain expected differences */

func TestUpgradeDefault(t *testing.T) {
	installOpts, upgradeOpts, _ := testOptions(t)
	install, upgrade, err := renderInstallAndUpgrade(t, installOpts, upgradeOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Install and upgrade manifests should be identical except for the version.
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	for id, diffs := range diffManifestLists(expectedManifests, upgradeManifests) {
		for _, diff := range diffs {
			if ignorableDiff(id, diff) {
				continue
			}
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeHA(t *testing.T) {
	installOpts, upgradeOpts, _ := testOptions(t)
	installOpts.HighAvailability = true
	install, upgrade, err := renderInstallAndUpgrade(t, installOpts, upgradeOpts)
	if err != nil {
		t.Fatal(err)
	}
	// Install and upgrade manifests should be identical except for the version.
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	for id, diffs := range diffManifestLists(expectedManifests, upgradeManifests) {
		for _, diff := range diffs {
			if ignorableDiff(id, diff) {
				continue
			}
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeExternalIssuer(t *testing.T) {
	installOpts, err := testInstallOptionsNoCerts(false)
	if err != nil {
		t.Fatalf("failed to create install options: %s", err)
	}
	upgradeOpts, _, err := testUpgradeOptions()
	if err != nil {
		t.Fatalf("failed to create upgrade options: %s", err)
	}

	issuer := generateIssuerCerts(t, true)
	defer issuer.cleanup()

	installOpts.Identity.Issuer.Scheme = string(corev1.SecretTypeTLS)
	ca, err := base64.StdEncoding.DecodeString(issuer.ca)
	if err != nil {
		t.Fatal(err)
	}
	installOpts.IdentityTrustAnchorsPEM = string(ca)
	install := renderInstall(t, installOpts)
	upgrade, err := renderUpgrade(install.String()+externalIssuerSecret(issuer), upgradeOpts)

	if err != nil {
		t.Fatal(err)
	}
	// Install and upgrade manifests should be identical except for the version.
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	for id, diffs := range diffManifestLists(expectedManifests, upgradeManifests) {
		for _, diff := range diffs {
			if ignorableDiff(id, diff) {
				continue
			}
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func TestUpgradeIssuerWithExternalIssuerFails(t *testing.T) {
	installOpts, upgradeOpts, flagSet := testOptions(t)

	issuer := generateIssuerCerts(t, true)
	defer issuer.cleanup()

	installOpts.IdentityTrustDomain = "cluster.local"
	installOpts.IdentityTrustDomain = issuer.ca
	installOpts.Identity.Issuer.Scheme = string(corev1.SecretTypeTLS)
	installOpts.Identity.Issuer.TLS.CrtPEM = issuer.crt
	installOpts.Identity.Issuer.TLS.KeyPEM = issuer.key
	install := renderInstall(t, installOpts)

	upgradedIssuer := generateIssuerCerts(t, true)
	defer upgradedIssuer.cleanup()

	flagSet.Set("identity-trust-anchors-file", upgradedIssuer.caFile)
	flagSet.Set("identity-issuer-certificate-file", upgradedIssuer.crtFile)
	flagSet.Set("identity-issuer-key-file", upgradedIssuer.keyFile)

	_, err := renderUpgrade(install.String()+externalIssuerSecret(issuer), upgradeOpts)

	expectedErr := "cannot update issuer certificates if you are using external cert management solution"

	if err == nil || err.Error() != expectedErr {
		t.Errorf("Expected error: %s but got %s", expectedErr, err)
	}
}

func TestUpgradeOverwriteIssuer(t *testing.T) {
	installOpts, upgradeOpts, flagSet := testOptions(t)

	issuerCerts := generateIssuerCerts(t, true)
	defer issuerCerts.cleanup()

	flagSet.Set("identity-trust-anchors-file", issuerCerts.caFile)
	flagSet.Set("identity-issuer-certificate-file", issuerCerts.crtFile)
	flagSet.Set("identity-issuer-key-file", issuerCerts.keyFile)

	install, upgrade, err := renderInstallAndUpgrade(t, installOpts, upgradeOpts)
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
			if ignorableDiff(id, diff) {
				continue
			}
			if isProxyEnvDiff(diff.path) {
				// Trust root has changed.
				continue
			}

			if id == "Deployment/linkerd-identity" || id == "Deployment/linkerd-proxy-injector" {
				if pathMatch(diff.path, []string{"spec", "template", "spec", "containers", "*", "env", "*", "value"}) && diff.b.(string) == issuerCerts.ca {
					continue
				}
				t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
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
					// Differences in expiry are expected; do nothing.
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
	installOpts, upgradeOpts, flagSet := testOptions(t)

	issuerCerts := generateIssuerCerts(t, true)
	defer issuerCerts.cleanup()

	flagSet.Set("identity-trust-anchors-file", issuerCerts.caFile)
	flagSet.Set("identity-issuer-certificate-file", issuerCerts.crtFile)

	_, _, err := renderInstallAndUpgrade(t, installOpts, upgradeOpts)

	expectedErr := "failed to validate issuer credentials: failed to read CA: tls: Public and private key do not match"

	if err == nil || err.Error() != expectedErr {
		t.Errorf("Expected error: %s but got %s", expectedErr, err)
	}
}

func TestUpgradeFailsWithOnlyIssuerKey(t *testing.T) {
	installOpts, upgradeOpts, flagSet := testOptions(t)

	issuerCerts := generateIssuerCerts(t, false)
	defer issuerCerts.cleanup()

	flagSet.Set("identity-trust-anchors-file", issuerCerts.caFile)
	flagSet.Set("identity-issuer-certificate-file", issuerCerts.crtFile)

	_, _, err := renderInstallAndUpgrade(t, installOpts, upgradeOpts)

	expectedErr := "failed to validate issuer credentials: failed to read CA: tls: Public and private key do not match"

	if err == nil || err.Error() != expectedErr {
		t.Errorf("Expected error: %s but got %s", expectedErr, err)
	}
}

func TestUpgradeRootFailsWithOldPods(t *testing.T) {
	installOpts, upgradeOpts, flagSet := testOptions(t)

	oldIssuer := generateIssuerCerts(t, false)
	defer oldIssuer.cleanup()

	install := renderInstall(t, installOpts)

	issuerCerts := generateIssuerCerts(t, true)
	defer issuerCerts.cleanup()

	flagSet.Set("identity-trust-anchors-file", issuerCerts.caFile)
	flagSet.Set("identity-issuer-certificate-file", issuerCerts.crtFile)
	flagSet.Set("identity-issuer-key-file", issuerCerts.keyFile)

	_, err := renderUpgrade(install.String()+podWithSidecar(oldIssuer), upgradeOpts)

	expectedErr := "You are attempting to use an issuer certificate which does not validate against the trust anchors of the following pods"
	if err == nil || !strings.HasPrefix(err.Error(), expectedErr) {
		t.Errorf("Expected error: %s but got %s", expectedErr, err)
	}
}

// this test constructs a set of secrets resources
func TestUpgradeWebhookCrtsNameChange(t *testing.T) {
	installOpts, upgradeOpts, _ := testOptions(t)

	injectorCerts := generateCerts(t, "linkerd-proxy-injector.linkerd.svc", false)
	defer injectorCerts.cleanup()
	installOpts.ProxyInjector.TLS = &linkerd2.TLS{
		CaBundle: injectorCerts.ca,
		CrtPEM:   injectorCerts.crt,
		KeyPEM:   injectorCerts.key,
	}

	validatorCerts := generateCerts(t, "linkerd-sp-validator.linkerd.svc", false)
	defer validatorCerts.cleanup()
	installOpts.ProfileValidator.TLS = &linkerd2.TLS{
		CaBundle: validatorCerts.ca,
		CrtPEM:   validatorCerts.crt,
		KeyPEM:   validatorCerts.key,
	}

	rendered := renderInstall(t, installOpts)
	expected := replaceVersions(rendered.String())

	// switch back to old tls secret names.
	install := replaceK8sSecrets(expected)

	upgrade, err := renderUpgrade(install, upgradeOpts)
	if err != nil {
		t.Fatal(err)
	}
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	for id, diffs := range diffManifestLists(expectedManifests, upgradeManifests) {
		for _, diff := range diffs {
			if ignorableDiff(id, diff) {
				continue
			}
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

func replaceK8sSecrets(input string) string {
	manifest := strings.ReplaceAll(input, "kubernetes.io/tls", "Opaque")
	manifest = strings.ReplaceAll(manifest, "tls.key", "key.pem")
	manifest = strings.ReplaceAll(manifest, "tls.crt", "crt.pem")
	manifest = strings.ReplaceAll(manifest, "linkerd-proxy-injector-k8s-tls", "linkerd-proxy-injector-tls")
	manifest = strings.ReplaceAll(manifest, "linkerd-tap-k8s-tls", "linkerd-tap-tls")
	manifest = strings.ReplaceAll(manifest, "linkerd-sp-validator-k8s-tls", "linkerd-sp-validator-tls")
	return manifest
}

func TestUpgradeTwoLevelWebhookCrts(t *testing.T) {
	installOpts, upgradeOpts, _ := testOptions(t)

	// This tests the case where the webhook certs are not self-signed.
	injectorCerts := generateCerts(t, "linkerd-proxy-injector.linkerd.svc", false)
	defer injectorCerts.cleanup()
	installOpts.ProxyInjector.TLS = &linkerd2.TLS{
		CaBundle: injectorCerts.ca,
		CrtPEM:   injectorCerts.crt,
		KeyPEM:   injectorCerts.key,
	}

	validatorCerts := generateCerts(t, "linkerd-sp-validator.linkerd.svc", false)
	defer validatorCerts.cleanup()
	installOpts.ProfileValidator.TLS = &linkerd2.TLS{
		CaBundle: validatorCerts.ca,
		CrtPEM:   validatorCerts.crt,
		KeyPEM:   validatorCerts.key,
	}

	install := renderInstall(t, installOpts)
	upgrade, err := renderUpgrade(install.String(), upgradeOpts)
	if err != nil {
		t.Fatal(err)
	}
	expected := replaceVersions(install.String())
	expectedManifests := parseManifestList(expected)
	upgradeManifests := parseManifestList(upgrade.String())
	for id, diffs := range diffManifestLists(expectedManifests, upgradeManifests) {
		for _, diff := range diffs {
			if ignorableDiff(id, diff) {
				continue
			}
			t.Errorf("Unexpected diff in %s:\n%s", id, diff.String())
		}
	}
}

/* Helpers */

func testUpgradeOptions() ([]flag.Flag, *pflag.FlagSet, error) {
	defaults, err := charts.NewValues()
	if err != nil {
		return nil, nil, err
	}

	allStageFlags, allStageFlagSet := makeAllStageFlags(defaults)
	installUpgradeFlags, installUpgradeFlagSet, err := makeInstallUpgradeFlags(defaults)
	if err != nil {
		return nil, nil, err
	}
	proxyFlags, proxyFlagSet := makeProxyFlags(defaults)
	upgradeFlagSet := makeUpgradeFlags()

	flags := flattenFlags(allStageFlags, installUpgradeFlags, proxyFlags)
	flagSet := pflag.NewFlagSet("upgrade", pflag.ExitOnError)
	flagSet.AddFlagSet(allStageFlagSet)
	flagSet.AddFlagSet(installUpgradeFlagSet)
	flagSet.AddFlagSet(proxyFlagSet)
	flagSet.AddFlagSet(upgradeFlagSet)

	flagSet.Set("control-plane-version", upgradeControlPlaneVersion)
	flagSet.Set("proxy-version", upgradeProxyVersion)

	return flags, flagSet, nil
}

func testOptions(t *testing.T) (*charts.Values, []flag.Flag, *pflag.FlagSet) {
	installValues, err := testInstallOptions()
	if err != nil {
		t.Fatalf("failed to create install options: %s", err)
	}
	upgradeFlags, upgradeFlagSet, err := testUpgradeOptions()
	if err != nil {
		t.Fatalf("failed to create upgrade options: %s", err)
	}
	return installValues, upgradeFlags, upgradeFlagSet
}

func replaceVersions(manifest string) string {
	manifest = strings.ReplaceAll(manifest, installProxyVersion, upgradeProxyVersion)
	manifest = strings.ReplaceAll(manifest, installControlPlaneVersion, upgradeControlPlaneVersion)
	manifest = strings.ReplaceAll(manifest, installDebugVersion, upgradeDebugVersion)
	return manifest
}

func generateIssuerCerts(t *testing.T, b64encode bool) issuerCerts {
	return generateCerts(t, "identity.linkerd.cluster.local", b64encode)
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

func (ic issuerCerts) cleanup() {
	os.Remove(ic.caFile)
	os.Remove(ic.crtFile)
	os.Remove(ic.keyFile)
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
    image: cr.l5d.io/linkerd/proxy:some-version
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

func renderInstall(t *testing.T, values *linkerd2.Values) bytes.Buffer {
	var installBuf bytes.Buffer
	if err := render(&installBuf, values, "", valuespkg.Options{}); err != nil {
		t.Fatalf("could not render install manifests: %s", err)
	}
	return installBuf
}

func renderUpgrade(installManifest string, upgradeOpts []flag.Flag) (bytes.Buffer, error) {
	k, err := k8s.NewFakeAPIFromManifests([]io.Reader{strings.NewReader(installManifest)})
	if err != nil {
		return bytes.Buffer{}, err
	}

	return upgrade(context.Background(), k, upgradeOpts, "", valuespkg.Options{})
}

func renderInstallAndUpgrade(t *testing.T, installOpts *charts.Values, upgradeOpts []flag.Flag) (bytes.Buffer, bytes.Buffer, error) {
	err := validateValues(context.Background(), nil, installOpts)
	if err != nil {
		return bytes.Buffer{}, bytes.Buffer{}, err
	}
	installBuf := renderInstall(t, installOpts)
	upgradeBuf, err := renderUpgrade(installBuf.String(), upgradeOpts)
	return installBuf, upgradeBuf, err
}

// Certain resources are expected to change during an upgrade. We can safely
// ignore these diffs in every test.
func ignorableDiff(id string, diff diff) bool {
	if id == overridesSecret {
		// The config overrides will always change because at least the control
		// plane and proxy versions will change.
		return true
	}
	if id == linkerdConfigMap {
		// The linkerd-config values will always change because at least the control
		// plane and proxy versions will change.
		return true
	}
	if (strings.HasPrefix(id, "MutatingWebhookConfiguration") || strings.HasPrefix(id, "ValidatingWebhookConfiguration")) &&
		pathMatch(diff.path, []string{"webhooks", "*", "clientConfig", "caBundle"}) {
		// Webhook TLS chains are regenerated upon upgrade so we expect the
		// caBundle to change.
		return true
	}
	if strings.HasPrefix(id, "APIService") &&
		pathMatch(diff.path, []string{"spec", "caBundle"}) {
		// APIService TLS chains are regenerated upon upgrade so we expect the
		// caBundle to change.
		return true
	}

	if (id == "Deployment/linkerd-sp-validator" || id == "Deployment/linkerd-proxy-injector" || id == "Deployment/linkerd-tap") &&
		pathMatch(diff.path, []string{"spec", "template", "metadata", "annotations", "checksum/config"}) {
		// APIService TLS chains are regenerated upon upgrade so we expect the
		// caBundle to change.
		return true
	}

	if id == "Secret/linkerd-proxy-injector-tls" || id == "Secret/linkerd-sp-validator-tls" ||
		id == "Secret/linkerd-tap-tls" || id == "Secret/linkerd-sp-validator-k8s-tls" ||
		id == "Secret/linkerd-proxy-injector-k8s-tls" || id == "Secret/linkerd-tap-k8s-tls" {
		// Webhook and APIService TLS chains are regenerated upon upgrade.
		return true
	}
	return false
}
