package norelay

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until control plane is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdDeployReplicasEdge)
	os.Exit(m.Run())
}

// TestNoRelay verifies that hitting the proxy's outbound port doesn't result
// in an open relay, by trying to leverage the l5d-dst-override header in an
// ingress proxy.
func TestNoRelay(t *testing.T) {
	ctx := context.Background()
	deployments := getDeployments(t)
	TestHelper.WithDataPlaneNamespace(ctx, "norelay-test", map[string]string{}, t, func(t *testing.T, ns string) {
		for name, res := range deployments {
			out, err := TestHelper.KubectlApply(res, ns)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error",
					"unexpected error with deployment %s: %v output:\n%s",
					name, err, out,
				)
			}
		}

		for name := range deployments {
			err := TestHelper.CheckPods(ctx, ns, name, 1)
			if err != nil {
				//nolint:errorlint
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedError(t, "CheckPods timed-out", err)
				}
			}
		}

		ip, err := TestHelper.Kubectl(
			"", "-n", ns, "get", "po", "-l", "app=server-relay",
			"-o", "jsonpath='{range .items[*]}{@.status.podIP}{end}'",
		)
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to retrieve server-relay IP",
				"failed to retrieve server-relay IP: %s\n%s", err, ip)
		}
		relayIP := strings.Trim(ip, "'")
		o, err := TestHelper.Kubectl(
			"", "-n", ns, "exec", "deploy/client",
			"--", "curl", "-f", "-H", "l5d-dst-override: server-hello."+ns+".svc.cluster.local:8080", "http://"+relayIP+":4140",
		)
		if err == nil || err.Error() != "exit status 22" {
			testutil.AnnotatedFatalf(t, "no error or unexpected error returned",
				"no error or unexpected error returned: %s\n%s", o, err)
		}
	})
}

// TestRelay validates the previous test by running the same scenario but
// forcing an open relay by changing the value of
// LINKERD2_PROXY_OUTBOUND_LISTEN_ADDR from 127.0.0.1:4140 to 0.0.0.0:4140,
// which is not possible without manually changing the injected proxy yaml
func TestRelay(t *testing.T) {
	ctx := context.Background()
	deployments := getDeployments(t)
	deployments["server-relay"] = strings.ReplaceAll(deployments["server-relay"], "127.0.0.1:4140", "0.0.0.0:4140")
	TestHelper.WithDataPlaneNamespace(ctx, "relay-test", map[string]string{}, t, func(t *testing.T, ns string) {
		for name, res := range deployments {
			out, err := TestHelper.KubectlApply(res, ns)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error",
					"unexpected error with deployment %s: %v output:\n%s",
					name, err, out,
				)
			}
		}

		for name := range deployments {
			err := TestHelper.CheckPods(ctx, ns, name, 1)
			if err != nil {
				//nolint:errorlint
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedError(t, "CheckPods timed-out", err)
				}
			}
		}

		ip, err := TestHelper.Kubectl(
			"", "-n", ns, "get", "po", "-l", "app=server-relay",
			"-o", "jsonpath='{range .items[*]}{@.status.podIP}{end}'",
		)
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to retrieve server-relay IP",
				"failed to retrieve server-relay IP: %s\n%s", err, ip)
		}
		relayIP := strings.Trim(ip, "'")
		o, err := TestHelper.Kubectl(
			"", "-n", ns, "exec", "deploy/client",
			"--", "curl", "-f", "-H", "l5d-dst-override: server-hello."+ns+".svc.cluster.local:8080", "http://"+relayIP+":4140",
		)
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error returned",
				"unexpected error returned: %s\n%s", o, err)
		}
		if !strings.Contains(o, "HELLO-FROM-SERVER") {
			testutil.AnnotatedFatalf(t, "unexpected response returned",
				"unexpected response returned: %s", o)
		}
	})
}

func getDeployments(t *testing.T) map[string]string {
	deploys := make(map[string]string)
	var err error

	// server-hello is injected normally
	deploys["server-hello"], err = TestHelper.LinkerdRun("inject", "testdata/server-hello.yml")
	if err != nil {
		testutil.AnnotatedFatal(t, "unexpected error", err)
	}

	// server-relay is injected in ingress mode, manually
	deploys["server-relay"], err = TestHelper.LinkerdRun("inject", "--manual", "--ingress", "testdata/server-relay.yml")
	if err != nil {
		testutil.AnnotatedFatal(t, "unexpected error", err)
	}

	// client is not injected
	deploys["client"], err = testutil.ReadFile("testdata/client.yml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read 'client.yml'", "failed to read 'client.yml': %s", err)
	}

	return deploys
}
