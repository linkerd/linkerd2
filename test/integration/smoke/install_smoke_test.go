package smoke

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var (
	TestHelper *testutil.TestHelper
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

var (
	injectionCases = []struct {
		ns          string
		annotations map[string]string
		injectArgs  []string
	}{
		{
			ns: "smoke-test",
			annotations: map[string]string{
				k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
			},
			injectArgs: nil,
		},
		{
			ns:         "smoke-test-manual",
			injectArgs: []string{"--manual"},
		},
		{
			ns:         "smoke-test-ann",
			injectArgs: []string{},
		},
	}

	multiclusterExtensionName = "multicluster"
	vizExtensionName          = "viz"
)

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestInject(t *testing.T) {
	resources, err := testutil.ReadFile("testdata/smoke_test.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read smoke test file",
			"failed to read smoke test file: %s", err)
	}

	ctx := context.Background()
	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			var out string

			prefixedNs := TestHelper.GetTestNamespace(tc.ns)

			err := TestHelper.CreateDataPlaneNamespaceIfNotExists(ctx, prefixedNs, tc.annotations)
			if err != nil {
				testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", prefixedNs),
					"failed to create %s namespace: %s", prefixedNs, err)
			}

			if tc.injectArgs != nil {
				cmd := []string{"inject"}
				cmd = append(cmd, tc.injectArgs...)
				cmd = append(cmd, "testdata/smoke_test.yaml")

				var injectReport string
				out, injectReport, err = TestHelper.PipeToLinkerdRun("", cmd...)
				if err != nil {
					testutil.AnnotatedFatalf(t, "'linkerd inject' command failed",
						"'linkerd inject' command failed: %s\n%s", err, out)
				}

				err = TestHelper.ValidateOutput(injectReport, "inject.report.golden")
				if err != nil {
					testutil.AnnotatedFatalf(t, "received unexpected output",
						"received unexpected output\n%s", err.Error())
				}
			} else {
				out = resources
			}

			out, err = TestHelper.KubectlApply(out, prefixedNs)
			if err != nil {
				testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
					"'kubectl apply' command failed\n%s", out)
			}

			for _, deploy := range []string{"smoke-test-terminus", "smoke-test-gateway"} {
				if err := TestHelper.CheckPods(ctx, prefixedNs, deploy, 1); err != nil {
					if rce, ok := err.(*testutil.RestartCountError); ok {
						testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
					} else {
						testutil.AnnotatedFatal(t, "CheckPods timed-out", err)
					}
				}
			}

			url, err := TestHelper.URLFor(ctx, prefixedNs, "smoke-test-gateway", 8080)
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to get URL",
					"failed to get URL: %s", err)
			}

			output, err := TestHelper.HTTPGetURL(url)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error",
					"unexpected error: %v %s", err, output)
			}

			expectedStringInPayload := "\"payload\":\"BANANA\""
			if !strings.Contains(output, expectedStringInPayload) {
				testutil.AnnotatedFatalf(t, "application response doesn't contain the expected response",
					"expected application response to contain string [%s], but it was [%s]",
					expectedStringInPayload, output)
			}
		})
	}
}

func TestServiceProfileDeploy(t *testing.T) {
	bbProto, err := TestHelper.HTTPGetURL("https://raw.githubusercontent.com/BuoyantIO/bb/v0.0.5/api.proto")
	if err != nil {
		testutil.AnnotatedFatalf(t, "unexpected error",
			"unexpected error: %v %s", err, bbProto)
	}

	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			prefixedNs := TestHelper.GetTestNamespace(tc.ns)

			cmd := []string{"profile", "-n", prefixedNs, "--proto", "-", "smoke-test-terminus-svc"}
			bbSP, stderr, err := TestHelper.PipeToLinkerdRun(bbProto, cmd...)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error",
					"unexpected error: %v %s", err, stderr)
			}

			out, err := TestHelper.KubectlApply(bbSP, prefixedNs)
			if err != nil {
				testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
					"'kubectl apply' command failed: %s\n%s", err, out)
			}
		})
	}
}

func TestCheckProxy(t *testing.T) {
	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			prefixedNs := TestHelper.GetTestNamespace(tc.ns)
			testCheckCommand(t, "proxy", TestHelper.GetVersion(), prefixedNs, "")
		})
	}
}

// TestCleanUp deletes the resources used in the above tests
func TestCleanUp(t *testing.T) {
	ctx := context.Background()
	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			prefixedNs := TestHelper.GetTestNamespace(tc.ns)
			if err := TestHelper.DeleteNamespaceIfExists(ctx, prefixedNs); err != nil {
				testutil.AnnotatedFatal(t, "Deleting namespace failed", err)
			}
		})
	}
}

func testCheckCommand(t *testing.T, stage, expectedVersion, namespace, cliVersionOverride string) {
	var cmd []string
	var golden string
	proxyStage := "proxy"
	if stage == proxyStage {
		cmd = []string{"check", "--proxy", "--expected-version", expectedVersion, "--namespace", namespace, "--wait=60m"}
		if TestHelper.CNI() {
			golden = "check.cni.proxy.golden"
		} else {
			golden = "check.proxy.golden"
		}
	} else if stage == "config" {
		cmd = []string{"check", "config", "--expected-version", expectedVersion, "--wait=60m"}
		golden = "check.config.golden"
	} else {
		cmd = []string{"check", "--expected-version", expectedVersion, "--wait=60m"}
		if TestHelper.CNI() {
			golden = "check.cni.golden"
		} else {
			golden = "check.golden"
		}
	}

	expected := getCheckOutput(t, golden, TestHelper.GetLinkerdNamespace())
	timeout := time.Minute * 5
	err := TestHelper.RetryFor(timeout, func() error {
		if cliVersionOverride != "" {
			cliVOverride := []string{"--cli-version-override", cliVersionOverride}
			cmd = append(cmd, cliVOverride...)
		}
		out, err := TestHelper.LinkerdRun(cmd...)

		if err != nil {
			return fmt.Errorf("'linkerd check' command failed\n%s\n%s", err, out)
		}

		if !strings.Contains(out, expected) {
			return fmt.Errorf(
				"Expected:\n%s\nActual:\n%s", expected, out)
		}

		for _, ext := range TestHelper.GetInstalledExtensions() {
			if ext == multiclusterExtensionName {
				// multicluster check --proxy and multicluster check have the same output
				// so use the same golden file.
				expected = getCheckOutput(t, "check.multicluster.golden", TestHelper.GetMulticlusterNamespace())
				if !strings.Contains(out, expected) {
					return fmt.Errorf(
						"Expected:\n%s\nActual:\n%s", expected, out)
				}
			} else if ext == vizExtensionName {
				if stage == proxyStage {
					expected = getCheckOutput(t, "check.viz.proxy.golden", TestHelper.GetVizNamespace())
					if !strings.Contains(out, expected) {
						return fmt.Errorf(
							"Expected:\n%s\nActual:\n%s", expected, out)
					}
				} else {
					expected = getCheckOutput(t, "check.viz.golden", TestHelper.GetVizNamespace())
					if !strings.Contains(out, expected) {
						return fmt.Errorf(
							"Expected:\n%s\nActual:\n%s", expected, out)
					}
				}
			}
		}

		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd check' command timed-out (%s)", timeout), err)
	}
}

func getCheckOutput(t *testing.T, goldenFile string, namespace string) string {
	pods, err := TestHelper.KubernetesHelper.GetPods(context.Background(), namespace, nil)
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("failed to retrieve pods: %s", err), err)
	}

	proxyVersionErr := ""
	err = healthcheck.CheckProxyVersionsUpToDate(pods, version.Channels{})
	if err != nil {
		proxyVersionErr = err.Error()
	}

	tpl := template.Must(template.ParseFiles("testdata" + "/" + goldenFile))
	vars := struct {
		ProxyVersionErr string
		HintURL         string
	}{
		proxyVersionErr,
		healthcheck.HintBaseURL(TestHelper.GetVersion()),
	}

	var expected bytes.Buffer
	if err := tpl.Execute(&expected, vars); err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("failed to parse check.viz.golden template: %s", err), err)
	}

	return expected.String()
}
