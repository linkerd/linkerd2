package multiclustertest

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"html/template"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var (
	TestHelper *testutil.TestHelper
	contexts   map[string]string
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
	// Write file with numberic mode 0444 -- ugo=r
	if err = os.WriteFile(rootPath, certs.ca, 0444); err != nil {
		testutil.AnnotatedFatal(t, "failed to create CA certificate", err)
	}

	// Second, write issuer key and cert to files
	issuerCertPath := fmt.Sprintf("%s/%s", tmpDir, "issuer.crt")
	issuerKeyPath := fmt.Sprintf("%s/%s", tmpDir, "issuer.key")

	if err = os.WriteFile(issuerCertPath, certs.issuerCert, 0444); err != nil {
		testutil.AnnotatedFatal(t, "failed to create issuer certificate", err)
	}

	if err = os.WriteFile(issuerKeyPath, certs.issuerKey, 0444); err != nil {
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
	for _, ctx := range contexts {
		out, err := TestHelper.LinkerdRun("--context="+ctx, "multicluster", "install")
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd multicluster install' command failed", err)
		}

		out, err = TestHelper.KubectlApplyWithContext(out, ctx, "-f", "-")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}
	}

	// Wait for gateways to come up
	for _, ctx := range contexts {
		TestHelper.WaitRolloutWithContext(t, testutil.MulticlusterDeployReplicas, ctx)
	}

	TestHelper.AddInstalledExtension("multicluster")
}

func TestMulticlusterResourcesPostInstall(t *testing.T) {
	multiclusterSvcs := []testutil.Service{
		{Namespace: "linkerd-multicluster", Name: "linkerd-gateway"},
	}

	testutil.TestResourcesPostInstall(TestHelper.GetMulticlusterNamespace(), multiclusterSvcs, testutil.MulticlusterDeployReplicas, TestHelper, t)
}

func TestLinkClusters(t *testing.T) {
	linkName := "target"
	// Get gateway IP from target cluster
	lbCmd := []string{
		"get", "svc",
		"-n", "kube-system", "traefik",
		"-o", "go-template={{ (index .status.loadBalancer.ingress 0).ip }}",
	}
	lbIP, err := TestHelper.KubectlWithContext("", contexts[testutil.TargetContextKey], lbCmd...)

	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl get' command failed",
			"'kubectl get' command failed\n%s", lbIP)
	}
	linkCmd := []string{
		"--context=" + contexts[testutil.TargetContextKey],
		"multicluster", "link",
		"--log-level", "debug",
		"--api-server-address", fmt.Sprintf("https://%s:6443", lbIP),
		"--cluster-name", linkName, "--set", "enableHeadlessServices=true",
	}

	// Create link in target context
	out, err := TestHelper.LinkerdRun(linkCmd...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd multicluster link' command failed", "'linkerd multicluster link' command failed: %s\n%s", out, err)
	}

	// Apply Link in source
	out, err = TestHelper.KubectlApplyWithContext(out, contexts[testutil.SourceContextKey], "-f", "-")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

}

// TestInstallViz will install the viz extension, needed to verify whether the
// gateway probe succeeded.
// TODO (matei): can the dependency on viz be removed?
func TestInstallViz(t *testing.T) {
	for _, ctx := range contexts {
		cmd := []string{
			"--context=" + ctx,
			"viz",
			"install",
			"--set", fmt.Sprintf("namespace=%s", TestHelper.GetVizNamespace()),
		}

		out, err := TestHelper.LinkerdRun(cmd...)
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd viz install' command failed", err)
		}

		out, err = TestHelper.KubectlApplyWithContext(out, ctx, "-f", "-")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}

	}

	// Allow viz to be installed in parallel and then block until viz is ready.
	for _, ctx := range contexts {
		TestHelper.WaitRolloutWithContext(t, testutil.LinkerdVizDeployReplicas, ctx)
	}
}

func TestCheckMulticluster(t *testing.T) {
	golden := "check.multicluster.golden"
	// Check resources after link were created successfully in source cluster
	ctx := contexts[testutil.SourceContextKey]
	checkCmd := []string{"--context=" + ctx, "multicluster", "check", "--wait=40s"}

	// First, switch context to make sure we check pods in the cluster we're
	// supposed to be checking them in. This will rebuild the clientset
	if err := TestHelper.SwitchContext(ctx); err != nil {
		testutil.AnnotatedFatalf(t, "failed to rebuild helper clientset with new context", "failed to rebuild helper clientset with new context [%s]: %v", ctx, err)
	}
	pods, err := TestHelper.KubernetesHelper.GetPods(context.Background(), TestHelper.GetMulticlusterNamespace(), nil)
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("failed to retrieve pods: %s", err), err)
	}

	linkName := "target"
	tpl := template.Must(template.ParseFiles("testdata" + "/" + golden))
	vars := struct {
		ProxyVersionErr string
		HintURL         string
		LinkName        string
	}{
		healthcheck.CheckProxyVersionsUpToDate(pods, version.Channels{}).Error(),
		healthcheck.HintBaseURL(TestHelper.GetVersion()),
		linkName,
	}

	var expected bytes.Buffer
	if err := tpl.Execute(&expected, vars); err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("failed to parse %s template: %s", golden, err), err)
	}

	timeout := 5 * time.Minute
	err = TestHelper.RetryFor(timeout, func() error {
		out, err := TestHelper.LinkerdRun(checkCmd...)
		if err != nil {
			return fmt.Errorf("'linkerd multicluster check' command failed\n%w", err)
		}

		if out != expected.String() {
			return fmt.Errorf(
				"Expected:\n%s\nActual:\n%s", expected.String(), out)
		}
		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd multicluster check' command timed-out (%s)", timeout), err)
	}
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
