package edgeupgradetest

import (
	"fmt"
	"os"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var (
	TestHelper             *testutil.TestHelper
	linkerdBaseEdgeVersion string
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////
func TestInstallLinkerd(t *testing.T) {
	versions, err := TestHelper.GetReleaseChannelVersions()
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
	}
	linkerdBaseEdgeVersion = versions["edge"]
	fmt.Printf("%v", linkerdBaseEdgeVersion)
}
