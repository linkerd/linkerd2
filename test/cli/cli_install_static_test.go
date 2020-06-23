package test

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

var (
	TestHelper *testutil.TestHelper
)

func TestMain(m *testing.M) {

	// Read the flags and create a new test helper
	exit := func(code int, msg string) {
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(code)
	}

	linkerd := flag.String("linkerd", "", "path to the linkerd binary to test")
	runTests := flag.Bool("cli-tests", false, "must be provided to run the cli tests")
	flag.Parse()

	if !*runTests {
		exit(0, "cli tests not enabled: enable with -cli-tests")
	}

	if *linkerd == "" {
		exit(1, "-linkerd flag is required")
	}

	TestHelper = testutil.NewGenericTestHelper(*linkerd, "", "l5d", "", "", "", "", "", "", false, false, *http.DefaultClient, testutil.KubernetesHelper{})
	os.Exit(testutil.Run(m, TestHelper))
}

func TestCliInstall(t *testing.T) {

	var (
		cmd  = "install"
		args = []string{
			"--ignore-cluster",
		}
	)

	exec := append([]string{cmd}, args...)
	out, stderr, err := TestHelper.LinkerdRun(exec...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd install' command failed",
			"'linkerd install' command failed: \n%s\n%s", out, stderr)
	}

}
