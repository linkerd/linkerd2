package serviceaccounts

import (
	"os"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

func namesMatch(names []string) bool {
	for _, name := range names {
		if name == "default" || name == "linkerd-heartbeat" {
			continue
		}
		found := false
		for _, expectedName := range healthcheck.ExpectedServiceAccountNames {
			if name == expectedName {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestServiceAccountsMatch(t *testing.T) {
	expectedNames := healthcheck.ExpectedServiceAccountNames

	res, err := TestHelper.Kubectl("",
		"-n", TestHelper.GetLinkerdNamespace(),
		"get", "serviceaccounts",
		"--output", "name",
	)
	if err != nil {
		t.Fatalf("Error retrieving list of service accounts: %s", err)
	}
	names := strings.Split(strings.TrimSpace(res), "\n")
	var saNames []string
	for _, name := range names {
		saNames = append(saNames, strings.TrimPrefix(name, "serviceaccount/"))
	}
	// disregard `default` and `linkerd-heartbeat`
	if len(saNames)-2 != len(expectedNames) || !namesMatch(saNames) {
		t.Fatalf("The service account list doesn't match the expected list: %s", expectedNames)
	}

	res, err = TestHelper.Kubectl("",
		"-n", TestHelper.GetLinkerdNamespace(),
		"get", "rolebindings", "linkerd-psp",
		"--output", "jsonpath={.subjects[*].name}",
	)
	if err != nil {
		t.Fatalf("Error retrieving list of linkerd-psp rolebindings: %s", err)
	}
	saNamesPSP := strings.Split(res, " ")
	// disregard `linkerd-heartbeat`
	if len(saNamesPSP)-1 != len(expectedNames) || !namesMatch(saNamesPSP) {
		t.Fatalf(
			"The service accounts in the linkerd-psp rolebindings don't match the expected list: %s",
			expectedNames)
	}
}
