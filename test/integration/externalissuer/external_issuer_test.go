package externalissuer

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/testutil"
	corev1 "k8s.io/api/core/v1"
)

var TestHelper *testutil.TestHelper

const (
	TestAppBackendDeploymentName = "backend"
	TestAppNamespaceSuffix       = "external-issuer-app-test"
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	if !TestHelper.ExternalIssuer() {
		fmt.Fprintln(os.Stdout, "Skipping as --external-issuer=false")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestExternalIssuer(t *testing.T) {
	ctx := context.Background()
	TestHelper.WithDataPlaneNamespace(ctx, TestAppNamespaceSuffix, map[string]string{}, t, func(t *testing.T, testNamespace string) {
		verifyInstallApp(ctx, t)
		verifyAppWorksBeforeCertRotation(t)
		verifyRotateExternalCerts(ctx, t)
		verifyIdentityServiceReloadsIssuerCert(t)
		ensureNewCSRSAreServed()
		verifyAppWorksAfterCertRotation(t)
	})
}

func verifyInstallApp(ctx context.Context, t *testing.T) {
	out, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/external_issuer_application.yaml")
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd inject' command failed", err)
	}

	prefixedNs := TestHelper.GetTestNamespace(TestAppNamespaceSuffix)
	out, err = TestHelper.KubectlApply(out, prefixedNs)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed", "'kubectl apply' command failed\n%s", out)
	}

	if err := TestHelper.CheckPods(ctx, prefixedNs, TestAppBackendDeploymentName, 1); err != nil {
		if rce, ok := err.(*testutil.RestartCountError); ok {
			testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
		} else {
			testutil.AnnotatedError(t, "CheckPods timed-out", err)
		}
	}

	if err := TestHelper.CheckPods(ctx, prefixedNs, "slow-cooker", 1); err != nil {
		if rce, ok := err.(*testutil.RestartCountError); ok {
			testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
		} else {
			testutil.AnnotatedError(t, "CheckPods timed-out", err)
		}
	}
}

func checkAppWoks(t *testing.T, timeout time.Duration) error {
	return TestHelper.RetryFor(timeout, func() error {
		args := []string{"viz", "stat", "deploy", "-n", TestHelper.GetTestNamespace(TestAppNamespaceSuffix), "--from", "deploy/slow-cooker", "-t", "1m"}
		out, err := TestHelper.LinkerdRun(args...)
		if err != nil {
			return err
		}
		rowStats, err := testutil.ParseRows(out, 1, 8)
		if err != nil {
			return err
		}

		stat := rowStats[TestAppBackendDeploymentName]
		if stat.Success != "100.00%" {
			t.Fatalf("Expected no errors in test app but got [%s] success rate", stat.Success)
		}
		return nil
	})

}

func verifyAppWorksBeforeCertRotation(t *testing.T) {
	timeout := 40 * time.Second
	err := checkAppWoks(t, timeout)
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out while ensuring test app works (before cert rotation) (%s)", timeout), err)
	}
}

func verifyRotateExternalCerts(ctx context.Context, t *testing.T) {
	// We rotate the certificates here by simply grabbing
	// the key and cert values from the temporary secret we have
	// created
	secretWithUpdatedData, err := TestHelper.KubernetesHelper.GetSecret(ctx, TestHelper.GetLinkerdNamespace(), k8s.IdentityIssuerSecretName+"-new")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to fetch new secret data resource", "failed to fetch new secret data resource: %s", err)
	}

	roots := secretWithUpdatedData.Data[k8s.IdentityIssuerTrustAnchorsNameExternal]
	crt := secretWithUpdatedData.Data[corev1.TLSCertKey]
	key := secretWithUpdatedData.Data[corev1.TLSPrivateKeyKey]

	if err = TestHelper.CreateTLSSecret(k8s.IdentityIssuerSecretName, string(roots), string(crt), string(key)); err != nil {
		testutil.AnnotatedFatalf(t, "failed to update linkerd-identity-issuer resource", "failed to update linkerd-identity-issuer resource: %s", err)
	}
}

func verifyIdentityServiceReloadsIssuerCert(t *testing.T) {
	// check that the identity service has received an IssuerUpdated event
	timeout := 90 * time.Second
	err := TestHelper.RetryFor(timeout, func() error {
		out, err := TestHelper.Kubectl("",
			"--namespace", TestHelper.GetLinkerdNamespace(),
			"get", "events", "--field-selector", "reason=IssuerUpdated", "-ojson",
		)
		if err != nil {
			testutil.AnnotatedErrorf(t, "'kubectl get events' command failed", "'kubectl get events' command failed with %s\n%s", err, out)
		}

		events, err := testutil.ParseEvents(out)
		if err != nil {
			return err
		}

		if len(events) != 1 {
			return fmt.Errorf("expected just one event but got %d", len(events))
		}

		expectedEventMessage := "Updated identity issuer"
		issuerUpdatedEvent := events[0]

		if issuerUpdatedEvent.Message != expectedEventMessage {
			return fmt.Errorf("expected event message [%s] but got [%s]", expectedEventMessage, issuerUpdatedEvent.Message)

		}
		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out verifying identity svc reloads issuer cert (%s)", timeout), err)
	}

}

func ensureNewCSRSAreServed() {
	// this is to ensure new certs have been issued by the identity service.
	// we know that this will happen because the issuance lifetime is set to 15s.
	// Possible improvement is to provide a more deterministic way of checking that.
	// Perhaps we can emit k8s events when the identity service processed a CSR.
	time.Sleep(20 * time.Second)
}

func verifyAppWorksAfterCertRotation(t *testing.T) {
	timeout := 40 * time.Second
	err := checkAppWoks(t, timeout)
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out ensuring test app works (after cert rotation) (%s)", timeout), err)
	}
}
