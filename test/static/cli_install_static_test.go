package test

import (
	"os"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

var (
	TestHelper *testutil.TestHelper
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewStaticCliTestHelper()
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
