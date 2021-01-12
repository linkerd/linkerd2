package cmd

import (
	"flag"
	"os"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

var (
	testDataDiffer testutil.TestDataDiffer
)

// TestMain parses flags before running tests
func TestMain(m *testing.M) {
	flag.BoolVar(&testDataDiffer.UpdateFixtures, "update", false, "update text fixtures in place")
	prettyDiff := os.Getenv("LINKERD_TEST_PRETTY_DIFF") != ""
	flag.BoolVar(&testDataDiffer.PrettyDiff, "pretty-diff", prettyDiff, "display the full text when diffing")
	flag.StringVar(&testDataDiffer.RejectPath, "reject-path", "", "write results for failed tests to this path (path is relative to the test location)")
	flag.Parse()
	os.Exit(m.Run())
}
