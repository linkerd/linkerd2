package endpoints

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

type testCase struct {
	name       string
	authority  string
	expectedRE string
	ns         string
}

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until control plane is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdDeployReplicasEdge)
	os.Exit(m.Run())
}

func TestGoodEndpoints(t *testing.T) {
	ctx := context.Background()
	controlNs := TestHelper.GetLinkerdNamespace()

	TestHelper.WithDataPlaneNamespace(ctx, "endpoints-test", map[string]string{}, t, func(t *testing.T, ns string) {
		out, err := TestHelper.Kubectl("", "apply", "-f", "testdata/nginx.yaml", "-n", ns)
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v output:\n%s", err, out)
		}

		err = TestHelper.CheckPods(ctx, ns, "nginx", 1)
		if err != nil {
			//nolint:errorlint
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out", err)
			}
		}

		endpointCases := createTestCaseTable(controlNs, ns)
		for _, endpointCase := range endpointCases {
			testName := fmt.Sprintf("expect endpoints created for %s", endpointCase.name)

			t.Run(testName, func(t *testing.T) {
				err = testutil.RetryFor(5*time.Second, func() error {
					out, err = TestHelper.LinkerdRun("diagnostics", "endpoints", endpointCase.authority, "-ojson")
					if err != nil {
						return fmt.Errorf("failed to get endpoints for %s: %w", endpointCase.authority, err)
					}

					re := regexp.MustCompile(endpointCase.expectedRE)
					if !re.MatchString(out) {
						return fmt.Errorf("endpoint data does not match pattern\nexpected output:\n%s\nactual:\n%s", endpointCase.expectedRE, out)
					}

					matches := re.FindStringSubmatch(out)
					if len(matches) < 2 {
						return fmt.Errorf("invalid endpoint data\nexpected: \n%s\nactual: \n%s", endpointCase.expectedRE, out)
					}

					namespaceMatch := matches[1]
					if namespaceMatch != endpointCase.ns {
						return fmt.Errorf("endpoint namespace does not match\nexpected: %s, actual: %s", endpointCase.ns, namespaceMatch)
					}

					return nil
				})
				if err != nil {
					testutil.AnnotatedErrorf(t, "unexpected error", "unexpected error: %v", err)
				}
			})
		}

	})
}

// TODO: when #3004 gets fixed, add a negative test for mismatched ports
func TestBadEndpoints(t *testing.T) {
	_, stderr, err := TestHelper.PipeToLinkerdRun("", "diagnostics", "endpoints", "foo")
	if err == nil {
		testutil.AnnotatedFatalf(t, "was expecting an error", "was expecting an error: %v", err)
	}
	stderrOut := strings.Split(stderr, "\n")
	if len(stderrOut) == 0 {
		testutil.AnnotatedFatalf(t, "unexpected output", "unexpected output: %s", stderr)
	}
	if stderrOut[0] != "Destination API error: Invalid authority: foo" {
		testutil.AnnotatedErrorf(t, "unexpected error string", "unexpected error string: %s", stderrOut[0])
	}
}

func createTestCaseTable(controlNs, endpointNs string) []testCase {
	return []testCase{
		{
			name:      "linkerd-dst",
			authority: fmt.Sprintf("linkerd-dst.%s.svc.cluster.local:8086", controlNs),
			expectedRE: `\[
  \{
    "namespace": "(\S*)",
    "ip": "[a-f0-9.:]+",
    "port": 8086,
    "pod": "linkerd-destination\-[a-f0-9]+\-[a-z0-9]+",
    "service": "linkerd-dst\.\S*",
    "weight": \d+,
    "http2": \{(?s).*\},
    "labels": \{(?s).*\}
  \}
\]`,
			ns: controlNs,
		},
		{
			name:      "linkerd-identity",
			authority: fmt.Sprintf("linkerd-identity.%s.svc.cluster.local:8080", controlNs),
			expectedRE: `\[
  \{
    "namespace": "(\S*)",
    "ip": "[a-f0-9.:]+",
    "port": 8080,
    "pod": "linkerd-identity\-[a-f0-9]+\-[a-z0-9]+",
    "service": "linkerd-identity\.\S*",
    "weight": \d+,
    "http2": \{(?s).*\},
    "labels": \{(?s).*\}
  \}
\]`,
			ns: controlNs,
		},
		{
			name:      "linkerd-proxy-injector",
			authority: fmt.Sprintf("linkerd-proxy-injector.%s.svc.cluster.local:443", controlNs),
			expectedRE: `\[
  \{
    "namespace": "(\S*)",
    "ip": "[a-f0-9.:]+",
    "port": 8443,
    "pod": "linkerd-proxy-injector-[a-f0-9]+\-[a-z0-9]+",
    "service": "linkerd-proxy-injector\.\S*",
    "weight": \d+,
    "http2": \{(?s).*\},
    "labels": \{(?s).*\}
  \}
\]`,
			ns: controlNs,
		},
		{
			name:      "nginx",
			authority: fmt.Sprintf("nginx.%s.svc.cluster.local:8080", endpointNs),
			expectedRE: `\[
  \{
    "namespace": "(\S*)",
    "ip": "[a-f0-9.:]+",
    "port": 8080,
    "pod": "nginx-[a-f0-9]+\-[a-z0-9]+",
    "service": "nginx\.\S*",
    "weight": \d+,
    "http2": \{(?s).*\},
    "labels": \{(?s).*\}
  \}
\]`,
			ns: endpointNs,
		},
	}
}
