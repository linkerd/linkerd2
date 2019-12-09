package externalissuer

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

const (
	TestAppBackendDeploymentName = "backend"
	TestAppNamespaceSuffix       = "external-issuer-app-test"
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	if !TestHelper.ExternalIssuer() {
		fmt.Fprintln(os.Stdout, "Skiping as --external-issuer=false")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestExternalIssuer(t *testing.T) {
	verifyInstallApp(t)
	verifyAppWorksBeforeCertRotation(t)
	verifyRotateExternalCerts(t)
	verifyIdentityServiceReloadsIssuerCert(t)
	ensureNewCSRSAreServed()
	verifyAppWorksAfterCertRotation(t)
}

func verifyInstallApp(t *testing.T) {
	out, _, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/external_issuer_application.yaml")
	if err != nil {
		t.Fatalf("linkerd inject command failed\n%s", out)
	}

	prefixedNs := TestHelper.GetTestNamespace(TestAppNamespaceSuffix)
	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(prefixedNs, nil)
	if err != nil {
		t.Fatalf("failed to create %s namespace: %s", prefixedNs, err)
	}
	out, err = TestHelper.KubectlApply(out, prefixedNs)
	if err != nil {
		t.Fatalf("kubectl apply command failed\n%s", out)
	}

	// wait for deployment to start
	if err := TestHelper.CheckPods(prefixedNs, TestAppBackendDeploymentName, 1); err != nil {
		t.Error(err)
	}

	if err := TestHelper.CheckDeployment(prefixedNs, TestAppBackendDeploymentName, 1); err != nil {
		t.Error(fmt.Errorf("Error validating deployment [%s]:\n%s", TestAppBackendDeploymentName, err))
	}
}

func checkAppWoks(t *testing.T) error {
	return TestHelper.RetryFor(20*time.Second, func() error {
		args := []string{"stat", "deploy", "-n", TestHelper.GetTestNamespace(TestAppNamespaceSuffix), "--from", "deploy/slow-cooker", "-t", "1m"}
		out, stderr, err := TestHelper.LinkerdRun(args...)
		if err != nil {
			return fmt.Errorf("Unexpected stat error: %s\n%s\n%s", err, out, stderr)
		}
		rowStats, err := testutil.ParseRows(out, 1, 8)
		if err != nil {
			return err
		}

		stat := rowStats[TestAppBackendDeploymentName]
		if stat.Success != "100.00%" {
			t.Fatalf("Expected no errors in test app but got [%s] succes rate", stat.Success)
		}
		return nil
	})

}

func verifyAppWorksBeforeCertRotation(t *testing.T) {
	err := checkAppWoks(t)
	if err != nil {
		t.Fatalf("Received error while ensuring test app works (before cert rotation): %s", err)
	}
}

func verifyRotateExternalCerts(t *testing.T) {
	// change issuer secret
	secretResource, err := testutil.ReadFile("testdata/issuer_secret_2.yaml")
	if err != nil {
		t.Fatalf("failed to load linkerd-identity-issuer resource: %s", err)
	}

	out, err := TestHelper.KubectlApply(secretResource, TestHelper.GetLinkerdNamespace())

	if err != nil {
		t.Fatalf("failed to update linkerd-identity-issuer resource: %s", out)
	}
}

func verifyIdentityServiceReloadsIssuerCert(t *testing.T) {
	// check that the identity service has received an IssuerUpdated event
	err := TestHelper.RetryFor(90*time.Second, func() error {
		out, err := TestHelper.Kubectl("",
			"--namespace", TestHelper.GetLinkerdNamespace(),
			"get", "events", "--field-selector", "reason=IssuerUpdated", "-ojson",
		)
		if err != nil {
			t.Errorf("kubectl get events command failed with %s\n%s", err, out)
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
		t.Fatal(err.Error())
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
	err := checkAppWoks(t)
	if err != nil {
		t.Fatalf("Received error while ensuring test app works (after cert rotation): %s", err)
	}
}
