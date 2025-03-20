package externaltest

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var (
	TestHelper *testutil.TestHelper
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// TestInstallLinkerd will install the linkerd control plane to be used in the rest of
// the deep suite tests
func TestInstallLinkerd(t *testing.T) {
	err := TestHelper.InstallGatewayAPI()
	if err != nil {
		testutil.AnnotatedFatal(t, "failed to install gateway-api", err)
	}

	err = TestHelper.CreateControlPlaneNamespaceIfNotExists(context.Background(), TestHelper.GetLinkerdNamespace())
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", TestHelper.GetLinkerdNamespace()),
			"failed to create %s namespace: %s", TestHelper.GetLinkerdNamespace(), err)
	}

	identity := fmt.Sprintf("identity.%s.%s", TestHelper.GetLinkerdNamespace(), TestHelper.GetClusterDomain())

	root, err := tls.GenerateRootCAWithDefaults(identity)
	if err != nil {
		testutil.AnnotatedFatal(t, "error generating root CA", err)
	}

	// instead of passing the roots and key around we generate
	// two secrets here. The second one will be used in the
	// external_issuer_test to update the first one and trigger
	// cert rotation in the identity service. That allows us
	// to generated the certs on the fly and use custom domain.
	if err = TestHelper.CreateTLSSecret(
		k8s.IdentityIssuerSecretName,
		root.Cred.Crt.EncodeCertificatePEM(),
		root.Cred.Crt.EncodeCertificatePEM(),
		root.Cred.EncodePrivateKeyPEM()); err != nil {
		testutil.AnnotatedFatal(t, "error creating TLS secret", err)
	}

	crt2, err := root.GenerateCA(identity, -1)
	if err != nil {
		testutil.AnnotatedFatal(t, "error generating CA", err)
	}

	if err = TestHelper.CreateTLSSecret(
		k8s.IdentityIssuerSecretName+"-new",
		root.Cred.Crt.EncodeCertificatePEM(),
		crt2.Cred.EncodeCertificatePEM(),
		crt2.Cred.EncodePrivateKeyPEM()); err != nil {
		testutil.AnnotatedFatal(t, "error creating TLS secret (-new)", err)
	}

	// Install CRDs
	cmd := []string{
		"install",
		"--crds",
		"--controller-log-level", "debug",
		"--set", fmt.Sprintf("proxy.image.version=%s", TestHelper.GetVersion()),
		"--set", "heartbeatSchedule=1 2 3 4 5",
		"--identity-issuance-lifetime=15s",
		"--identity-external-issuer=true",
	}

	// Pipe cmd & args to `linkerd`
	out, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install --crds' command failed", err)
	}

	out, err = TestHelper.KubectlApplyWithArgs(out)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// Install control-plane with a short cert lifetime to put some pressure on
	// the CSR request, response code path.
	cmd = []string{
		"install",
		"--controller-log-level", "debug",
		"--set", fmt.Sprintf("proxy.image.version=%s", TestHelper.GetVersion()),
		"--set", "heartbeatSchedule=1 2 3 4 5",
		"--identity-issuance-lifetime=15s",
		"--identity-external-issuer=true",
	}

	// Pipe cmd & args to `linkerd`
	out, err = TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
	}

	out, err = TestHelper.KubectlApplyWithArgs(out)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	TestHelper.WaitRollout(t, testutil.LinkerdDeployReplicasEdge)
}

// TestInstallViz will install the viz extension to be used by the rest of the
// tests in the viz suite
func TestInstallViz(t *testing.T) {
	// Install external prometheus
	out, err := TestHelper.LinkerdRun("inject", "testdata/external_prometheus.yaml", "--manual")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd inject' command failed", "'linkerd inject' command failed: %s", err)
	}

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"kubectl apply command failed\n%s", out)
	}

	cmd := []string{
		"viz",
		"install",
		"--set", fmt.Sprintf("namespace=%s", TestHelper.GetVizNamespace()),
		"--set", "prometheusUrl=http://prometheus.external-prometheus.svc.cluster.local:9090",
		"--set", "prometheus.enabled=false",
	}

	out, err = TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd viz install' command failed", err)
	}

	out, err = TestHelper.KubectlApplyWithArgs(out)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	expectedDeployments := make(map[string]testutil.DeploySpec, len(testutil.LinkerdVizDeployReplicas))
	for k, v := range testutil.LinkerdVizDeployReplicas {
		if k == "prometheus" {
			v = testutil.DeploySpec{Namespace: "external-prometheus", Replicas: 1}
		}

		expectedDeployments[k] = v
	}
	TestHelper.WaitRollout(t, expectedDeployments)

}

func TestCheckVizWithExternalPrometheus(t *testing.T) {
	if err := TestHelper.TestCheck(); err != nil {
		t.Fatalf("'linkerd check' command failed: %s", err)
	}
}
