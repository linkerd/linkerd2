package cmd

import (
	"flag"
	"os"
	"testing"
)

var (
	// updateFixtures is set by the `-update` flag.
	updateFixtures bool

	// prettyDiff is set by the `-pretty-diff` flag.
	prettyDiff bool

	// write any rejected test data into this path
	rejectPath string
)

// TestMain parses flags before running tests
func TestMain(m *testing.M) {
	flag.BoolVar(&updateFixtures, "update", false, "update text fixtures in place")
	prettyDiff = os.Getenv("LINKERD_TEST_PRETTY_DIFF") != ""
	flag.BoolVar(&prettyDiff, "pretty-diff", prettyDiff, "display the full text when diffing")
	flag.StringVar(&rejectPath, "reject-path", "", "write results for failed tests to this path (path is relative to the test location)")
	flag.Parse()
	os.Exit(m.Run())
}
