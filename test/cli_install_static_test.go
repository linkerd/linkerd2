package test

import (
	"fmt"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

func TestCliInstall(t *testing.T) {
	if TestHelper.GetHelmReleaseName() != "" {
		return
	}

	var (
		cmd  = "install"
		args = []string{
			"--controller-log-level", "debug",
			"--proxy-log-level", "warn,linkerd2_proxy=debug",
			"--proxy-version", "test-version",
		}
	)

	err := TestHelper.CreateControlPlaneNamespaceIfNotExists(TestHelper.GetLinkerdNamespace())
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", TestHelper.GetLinkerdNamespace()),
			"failed to create %s namespace: %s", TestHelper.GetLinkerdNamespace(), err)
	}

	exec := append([]string{cmd}, args...)
	out, stderr, err := TestHelper.LinkerdRun(exec...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd install' command failed",
			"'linkerd install' command failed: \n%s\n%s", out, stderr)
	}

}
