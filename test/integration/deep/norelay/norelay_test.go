package norelay

import (
	"context"
	"fmt"
	"net"
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

		relayIPPort := getPodIPPort(t, ns, "app=server-relay", 4140)
		o, err := TestHelper.Kubectl(
			"", "-n", ns, "exec", "deploy/client",
			"--", "curl", "-f", "-H", "l5d-dst-override: server-hello."+ns+".svc.cluster.local:8080", "http://"+relayIPPort,
		)
		if err == nil || err.Error() != "exit status 22" {
			testutil.AnnotatedFatalf(t, "no error or unexpected error returned",
				"no error or unexpected error returned: %s\n%s", o, err)
		}
	})
}

// TestRelay validates the previous test by running the same scenario but
// forcing an open relay by changing the value of
// LINKERD2_PROXY_OUTBOUND_LISTEN_ADDRS from 127.0.0.1:4140,[::1]:4140 to
// 0.0.0.0:4140, which is not possible without manually changing the injected
// proxy yaml
//
// We don't care if this behavior breaks--it's not a supported configuration.
// However, this test is oddly useful in finding bugs in ingress-mode proxy
// configurations, so we keep it around. ¯\_(ツ)_/¯
func TestRelay(t *testing.T) {
	ctx := context.Background()
	deployments := getDeployments(t)
	deployments["server-relay"] = strings.ReplaceAll(
		deployments["server-relay"],
		"127.0.0.1:4140,[::1]:4140",
		"'[::]:4140'",
	)
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
		waitForPods(t, ctx, ns, deployments)
		relayIPPort := getPodIPPort(t, ns, "app=server-relay", 4140)

		// Send a request to the outbound proxy port with a header that should route internally.
		o, err := TestHelper.Kubectl(
			"", "-n", ns, "exec", "deploy/client",
			"--", "curl", "-fsv", "-H", "l5d-dst-override: server-hello."+ns+".svc.cluster.local:8080", "http://"+relayIPPort,
		)
		if err != nil {
			log, err := TestHelper.Kubectl(
				"", "logs",
				"-n", ns,
				"-l", "app=server-relay",
				"-c", "linkerd-proxy",
				"--tail=1000",
			)
			if err != nil {
				log = fmt.Sprintf("failed to retrieve server-relay logs: %s", err)
			}
			testutil.AnnotatedFatalf(t, "unexpected error returned",
				"unexpected error returned: %s\n%s\n---\n%s", o, err, log)
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
	deploys["server-relay"], err = TestHelper.LinkerdRun(
		"inject", "--manual", "--ingress",
		"--proxy-log-level=linkerd=debug,info",
		"testdata/server-relay.yml")
	if err != nil {
		testutil.AnnotatedFatal(t, "unexpected error", err)
	}

	// client is not injected
	deploys["client"], err = testutil.ReadFile("testdata/client.yml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read 'client.yml'",
			"failed to read 'client.yml': %s", err)
	}

	return deploys
}

func getPodIPPort(t *testing.T, ns, selector string, port int) string {
	t.Helper()
	ip, err := TestHelper.Kubectl(
		"", "-n", ns, "get", "po", "-l", selector,
		"-o", "jsonpath='{range .items[*]}{@.status.podIP}{end}'",
	)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to retrieve pod IP",
			"failed to retrieve pod IP: %s", err)
	}
	ip = strings.Trim(ip, "'")
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		testutil.AnnotatedFatalf(t, "invalid pod IP",
			"invalid pod IP: %s", err)
	}
	if parsedIP.To4() != nil {
		return fmt.Sprintf("%s:%d", ip, port)
	}
	return fmt.Sprintf("[%s]:%d", ip, port)
}

func waitForPods(t *testing.T, ctx context.Context, ns string, deployments map[string]string) {
	t.Helper()
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
}
