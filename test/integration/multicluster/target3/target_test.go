package target3

import (
	// "context"
	"fmt"
	"os"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	if !TestHelper.Multicluster() {
		fmt.Fprintln(os.Stderr, "Multicluster test disabled")
		os.Exit(0)
	}
	os.Exit(m.Run())
}
