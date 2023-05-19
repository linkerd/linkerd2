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
	// Block test execution until control plane is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdDeployReplicasEdge)
	os.Exit(m.Run())
}

// namesMatch checks if all the expectedServiceAccountNames are present in the given list,
// The passed argument list is allowed to contain extra members.
func namesMatch(names []string) bool {
	for _, expectedname := range healthcheck.ExpectedServiceAccountNames {
		found := false
		for _, name := range names {
			if expectedname == name {
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
		testutil.AnnotatedFatalf(t, "Error retrieving list of service accounts",
			"error retrieving list of service accounts: %s", err)
	}
	names := strings.Split(strings.TrimSpace(res), "\n")
	var saNames []string
	for _, name := range names {
		saNames = append(saNames, strings.TrimPrefix(name, "serviceaccount/"))
	}
	// disregard `default` and `linkerd-heartbeat`
	if len(saNames) < len(expectedNames) || !namesMatch(saNames) {
		testutil.Fatalf(t, "the service account list doesn't match the expected list: %s", expectedNames)
	}
}
