package multiclustertest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	mcHealthcheck "github.com/linkerd/linkerd2/multicluster/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var (
	TestHelper     *testutil.TestHelper
	contexts       map[string]string
	testDataDiffer testutil.TestDataDiffer
)

type (
	multiclusterCerts struct {
		ca         []byte
		issuerCert []byte
		issuerKey  []byte
	}
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

// TestInstall will install the linkerd control plane to be used in the rest of
// the deep suite tests.
func TestInstall(t *testing.T) {
	// Create temporary directory to create shared trust anchor and issuer
	// certificates
	tmpDir, err := os.MkdirTemp("", "multicluster-certs")
	if err != nil {
		testutil.AnnotatedFatal(t, "failed to create temp dir", err)
	}

	defer os.RemoveAll(tmpDir)

	// Generate CA certificate
	certs, err := createMulticlusterCertificates()
	if err != nil {
		testutil.AnnotatedFatal(t, "failed to create multicluster certificates", err)
	}

	// First, write CA to file
	rootPath := fmt.Sprintf("%s/%s", tmpDir, "ca.crt")
	// Write file with numberic mode 0400 -- u=r
	if err = os.WriteFile(rootPath, certs.ca, 0400); err != nil {
		testutil.AnnotatedFatal(t, "failed to create CA certificate", err)
	}

	// Second, write issuer key and cert to files
	issuerCertPath := fmt.Sprintf("%s/%s", tmpDir, "issuer.crt")
	issuerKeyPath := fmt.Sprintf("%s/%s", tmpDir, "issuer.key")

	if err = os.WriteFile(issuerCertPath, certs.issuerCert, 0400); err != nil {
		testutil.AnnotatedFatal(t, "failed to create issuer certificate", err)
	}

	if err = os.WriteFile(issuerKeyPath, certs.issuerKey, 0400); err != nil {
		testutil.AnnotatedFatal(t, "failed to create issuer key", err)
	}

	// Install CRDs
	cmd := []string{
		"install",
		"--crds",
		"--controller-log-level", "debug",
		"--set", fmt.Sprintf("proxy.image.version=%s", TestHelper.GetVersion()),
		"--set", "heartbeatSchedule=1 2 3 4 5",
		"--identity-trust-anchors-file", rootPath,
		"--identity-issuer-certificate-file", issuerCertPath,
		"--identity-issuer-key-file", issuerKeyPath,
	}

	// Global state to keep track of clusters
	contexts = TestHelper.GetMulticlusterContexts()
	for _, ctx := range contexts {
		// Pipe cmd & args to `linkerd`
		cmd := append([]string{"--context=" + ctx}, cmd...)
		out, err := TestHelper.LinkerdRun(cmd...)
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
		}
		// Apply manifest from stdin
		out, err = TestHelper.KubectlApplyWithContext(out, ctx, "-f", "-")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}
	}

	// Install control-plane
	cmd = []string{
		"install",
		"--controller-log-level", "debug",
		"--set", "proxyInit.image.name=ghcr.io/linkerd/proxy-init",
		"--set", fmt.Sprintf("proxyInit.image.version=%s", version.ProxyInitVersion),
		"--set", fmt.Sprintf("proxy.image.version=%s", TestHelper.GetVersion()),
		"--set", "heartbeatSchedule=1 2 3 4 5",
		"--identity-trust-anchors-file", rootPath,
		"--identity-issuer-certificate-file", issuerCertPath,
		"--identity-issuer-key-file", issuerKeyPath,
	}

	// Global state to keep track of clusters
	contexts = TestHelper.GetMulticlusterContexts()
	for _, ctx := range contexts {
		// Pipe cmd & args to `linkerd`
		cmd := append([]string{"--context=" + ctx}, cmd...)
		out, err := TestHelper.LinkerdRun(cmd...)
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
		}
		// Apply manifest from stdin
		out, err = TestHelper.KubectlApplyWithContext(out, ctx, "-f", "-")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}
	}

}

func TestInstallMulticluster(t *testing.T) {
	for k, ctx := range contexts {
		var out string
		var err error
		args := []string{"--context=" + ctx, "multicluster", "install",
			"--set", "localServiceMirror.excludedAnnotations=evil.linkerd/*\\,evil",
			"--set", "localServiceMirror.excludedLabels=evil.linkerd/*\\,evil",
		}

		// Source context should be installed without a gateway
		if k == testutil.SourceContextKey {
			args = append(args, "--gateway=false")
			if TestHelper.GetMulticlusterManageControllers() {
				args = append(
					args,
					"--set", "controllers[0].link.ref.name=target",
					"--set", "controllers[0].logFormat=json",
					"--set", "controllers[0].logLevel=debug",
					"--set", "controllers[0].enableHeadlessServices=true",
				)
			}
		} else if TestHelper.GetMulticlusterManageControllers() {
			args = append(
				args,
				"--set", "controllers[0].link.ref.name=source",
				"--set", "controllers[0].gateway.enabled=false",
				"--set", "controllers[0].logFormat=json",
				"--set", "controllers[0].logLevel=debug",
			)
		}

		out, err = TestHelper.LinkerdRun(args...)
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd multicluster install' command failed", err)
		}

		out, err = TestHelper.KubectlApplyWithContext(out, ctx, "-f", "-")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}
	}

	// Wait for gateways to come up in target cluster
	TestHelper.WaitRolloutWithContext(t, testutil.MulticlusterDeployReplicas, contexts[testutil.TargetContextKey])

	TestHelper.AddInstalledExtension("multicluster")
}

func TestMulticlusterResourcesPostInstall(t *testing.T) {
	multiclusterSvcs := []testutil.Service{
		{Namespace: "linkerd-multicluster", Name: "linkerd-gateway"},
	}

	TestHelper.SwitchContext(contexts[testutil.TargetContextKey])
	testutil.TestResourcesPostInstall(TestHelper.GetMulticlusterNamespace(), multiclusterSvcs, testutil.MulticlusterDeployReplicas, TestHelper, t)
}

func TestLinkClusters(t *testing.T) {
	// Get the control plane node IP, this is used to communicate with the
	// API Server address.
	// k3s runs an API server on the control plane node, the docker
	// container IP suffices for a connection between containers to happen
	// since they run on a shared network.
	lbCmd := []string{
		"get", "node",
		"-n", " -l=node-role.kubernetes.io/control-plane=true",
		"-o", "go-template={{ (index (index .items 0).status.addresses 0).address }}",
	}

	// Link target cluster to source
	// * source cluster should support headless services
	linkName := "target"
	lbIP, err := TestHelper.KubectlWithContext("", contexts[testutil.TargetContextKey], lbCmd...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl get' command failed",
			"'kubectl get' command failed\n%s", lbIP)
	}

	linkCmd := []string{
		"--context=" + contexts[testutil.TargetContextKey],
		"--cluster-name", linkName,
		"--api-server-address", fmt.Sprintf("https://%s:6443", lbIP),
		"multicluster", "link",
		"--excluded-annotations", "evil.linkerd/*,evil",
		"--excluded-labels", "evil.linkerd/*,evil",
	}
	if TestHelper.GetMulticlusterManageControllers() {
		linkCmd = append(
			linkCmd,
			"--service-mirror=false",
		)
	} else {
		linkCmd = append(
			linkCmd,
			"--set", "enableHeadlessServices=true",
			"--log-format", "json",
			"--log-level", "debug",
		)
	}

	out, err := TestHelper.LinkerdRun(linkCmd...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd multicluster link' command failed", "'linkerd multicluster link' command failed: %s\n%s", out, err)
	}

	out, err = TestHelper.KubectlApplyWithContext(out, contexts[testutil.SourceContextKey], "-f", "-")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// Link source cluster to target
	// * source cluster does not have a gateway, so the link will reflect that
	linkName = "source"
	lbIP, err = TestHelper.KubectlWithContext("", contexts[testutil.SourceContextKey], lbCmd...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl get' command failed",
			"'kubectl get' command failed\n%s", lbIP)
	}

	linkCmd = []string{
		"--context=" + contexts[testutil.SourceContextKey],
		"--cluster-name", linkName, "--gateway=false",
		"--api-server-address", fmt.Sprintf("https://%s:6443", lbIP),
		"multicluster", "link",
	}
	if TestHelper.GetMulticlusterManageControllers() {
		linkCmd = append(
			linkCmd,
			"--service-mirror=false",
		)
	} else {
		linkCmd = append(
			linkCmd,
			"--log-format", "json",
			"--log-level", "debug",
		)
	}

	out, err = TestHelper.LinkerdRun(linkCmd...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd multicluster link' command failed", "'linkerd multicluster link' command failed: %s\n%s", out, err)
	}

	out, err = TestHelper.KubectlApplyWithContext(out, contexts[testutil.TargetContextKey], "-f", "-")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

}

func TestCheckMulticluster(t *testing.T) {
	// Run `linkerd check` for both clusters, expect multicluster checks to be
	// run and pass successfully
	for _, ctx := range contexts {
		// First, switch context to make sure we check pods in the cluster we're
		// supposed to be checking them in. This will rebuild the clientset
		if err := TestHelper.SwitchContext(ctx); err != nil {
			testutil.AnnotatedFatalf(t, "failed to rebuild helper clientset with new context", "failed to rebuild helper clientset with new context [%s]: %v", ctx, err)
		}

		err := TestHelper.TestCheckWith([]healthcheck.CategoryID{mcHealthcheck.LinkerdMulticlusterExtensionCheck}, "--context", ctx)
		if err != nil {
			t.Fatalf("'linkerd check' command failed: %s", err)
		}
	}

	// Check resources after link were created successfully in source cluster (e.g.
	// secrets)
	t.Run("Outputs resources that allow service-mirror controllers to connect to target cluster", func(t *testing.T) {
		if err := TestHelper.SwitchContext(contexts[testutil.TargetContextKey]); err != nil {
			testutil.AnnotatedFatalf(t,
				"failed to rebuild helper clientset with new context",
				"failed to rebuild helper clientset with new context [%s]: %v",
				contexts[testutil.TargetContextKey], err)
		}
		name := "foo"
		out, err := TestHelper.LinkerdRun("mc", "allow", "--service-account-name", name)
		if err != nil {
			testutil.AnnotatedFatalf(t,
				"failed to execute 'mc allow' command",
				"failed to execute 'mc allow' command %s\n%s",
				err.Error(), out)
		}
		params := map[string]string{
			"Version":     TestHelper.GetVersion(),
			"AccountName": name,
		}
		if err = testDataDiffer.DiffTestYAMLTemplate("allow.golden", out, params); err != nil {
			testutil.AnnotatedFatalf(t,
				"received unexpected output",
				"received unexpected output\n%s",
				err.Error())
		}
	})
}

//////////////////////
///   CERT UTILS   ///
//////////////////////

func createMulticlusterCertificates() (multiclusterCerts, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return multiclusterCerts{}, err
	}

	var serialNumber int64 = 1
	caTemplate := createCertificateTemplate("root.linkerd.cluster.local", big.NewInt(serialNumber))
	caTemplate.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	// Create self-signed CA. Pass in its own pub key and its own private key
	caDerBytes, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return multiclusterCerts{}, err
	}

	// Increment serial number to generate next certificate (issuer)
	serialNumber++
	issuerDerBytes, issuerECKey, err := createIssuerCertificate(serialNumber, &caTemplate, caKey)
	if err != nil {
		return multiclusterCerts{}, err
	}

	// Convert keypairs to DER encoding. We don't care about the CA key so we
	// only encode (to export) the issuer keys
	issuerDerKey, err := x509.MarshalECPrivateKey(issuerECKey)
	if err != nil {
		return multiclusterCerts{}, err
	}

	// Finally, get strings from der blocks
	// we don't care about CA's private key, it won't be written to a file
	ca, _, err := tryDerToPem(caDerBytes, []byte{})
	if err != nil {
		return multiclusterCerts{}, err
	}

	issuer, issuerKey, err := tryDerToPem(issuerDerBytes, issuerDerKey)
	if err != nil {
		return multiclusterCerts{}, err
	}

	return multiclusterCerts{
		ca:         ca,
		issuerCert: issuer,
		issuerKey:  issuerKey,
	}, nil
}

// createCertificateTemplate will bootstrap a certificate based on the arguments
// passed in, with a validty of 24h
func createCertificateTemplate(subjectCommonName string, serialNumber *big.Int) x509.Certificate {
	return x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: subjectCommonName},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour * 24),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		IsCA:                  true,
	}
}

// createIssuerCertificate accepts a serial number, a CA template and the CA's
// key and it creates and signs an intermediate certificate. The function
// returns the certificate in DER encoding along with its keypair
func createIssuerCertificate(serialNumber int64, caTemplate *x509.Certificate, caKey *ecdsa.PrivateKey) ([]byte, *ecdsa.PrivateKey, error) {
	// Generate keypair first
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return []byte{}, nil, err
	}

	// Create issuer template
	template := createCertificateTemplate("identity.linkerd.cluster.local", big.NewInt(serialNumber))
	template.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign

	// Create issuer certificate signed by CA, we pass in parent template and
	// parent key
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, caTemplate, &key.PublicKey, caKey)
	if err != nil {
		return []byte{}, nil, err
	}

	return derBytes, key, nil
}

// tryDerToPem converts a DER encoded byte block and a DER encoded ECDSA keypair
// to a PEM encoded block
func tryDerToPem(derBlock []byte, key []byte) ([]byte, []byte, error) {
	certOut := &bytes.Buffer{}
	certPemBlock := pem.Block{Type: "CERTIFICATE", Bytes: derBlock}
	if err := pem.Encode(certOut, &certPemBlock); err != nil {
		return []byte{}, []byte{}, err
	}

	if len(key) == 0 {
		return certOut.Bytes(), []byte{}, nil
	}

	keyOut := &bytes.Buffer{}
	keyPemBlock := pem.Block{Type: "EC PRIVATE KEY", Bytes: key}
	if err := pem.Encode(keyOut, &keyPemBlock); err != nil {
		return []byte{}, []byte{}, err
	}

	return certOut.Bytes(), keyOut.Bytes(), nil
}
