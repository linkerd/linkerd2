package test

import (
	"flag"
	"fmt"
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
	namespace := flag.String("linkerd-namespace", "l5d-integration", "the namespace where linkerd is installed")
	flag.Parse()

	if *linkerd == "" {
		exit(1, "-linkerd flag is required")
	}

	TestHelper = testutil.NewGenericTestHelper(*linkerd, *namespace, "", "", "", "", "", "", false, false)

	code := m.Run()
	os.Exit(code)
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
