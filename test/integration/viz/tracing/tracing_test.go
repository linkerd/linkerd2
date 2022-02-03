package tracing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/linkerd/linkerd2/testutil"
)

type (
	traces struct {
		Data []trace `json:"data"`
	}

	trace struct {
		Processes map[string]process `json:"processes"`
	}

	process struct {
		ServiceName string `json:"serviceName"`
	}
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	if err := TestHelper.WaitUntilDeployReady(testutil.LinkerdVizDeployReplicas); err != nil {
		panic(fmt.Sprintf("error running test: %v", err))
	}
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestTracing(t *testing.T) {
	ctx := context.Background()
	if os.Getenv("RUN_ARM_TEST") != "" {
		t.Skip("Skipped. Jaeger & Open Census images does not support ARM yet")
	}

	// Require an environment variable to be set for this test to be run.
	if os.Getenv("RUN_FLAKEY_TEST") == "" {
		t.Skip("Skipping due to flakiness. See linkerd/linkerd2#7538")
	}

	// linkerd-jaeger extension
	tracingNs := "linkerd-jaeger"
	out, err := TestHelper.LinkerdRun("jaeger", "install")
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd jaeger install' command failed", err)
	}

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// wait for the jaeger extension
	checkCmd := []string{"jaeger", "check", "--wait=0"}
	golden := "check.jaeger.golden"
	timeout := time.Minute
	err = TestHelper.RetryFor(timeout, func() error {
		out, err := TestHelper.LinkerdRun(checkCmd...)
		if err != nil {
			return fmt.Errorf("'linkerd jaeger check' command failed\n%s\n%s", err, out)
		}

		pods, err := TestHelper.KubernetesHelper.GetPods(context.Background(), tracingNs, nil)
		if err != nil {
			testutil.AnnotatedFatal(t, fmt.Sprintf("failed to retrieve pods: %s", err), err)
		}

		tpl := template.Must(template.ParseFiles("testdata" + "/" + golden))
		versionErr := healthcheck.CheckProxyVersionsUpToDate(pods, version.Channels{})
		versionErrMsg := ""
		if versionErr != nil {
			versionErrMsg = versionErr.Error()
		}
		vars := struct {
			ProxyVersionErr string
			HintURL         string
		}{
			versionErrMsg,
			healthcheck.HintBaseURL(TestHelper.GetVersion()),
		}

		var expected bytes.Buffer
		if err := tpl.Execute(&expected, vars); err != nil {
			testutil.AnnotatedFatal(t, fmt.Sprintf("failed to parse check.viz.golden template: %s", err), err)
		}

		if out != expected.String() {
			return fmt.Errorf(
				"Expected:\n%s\nActual:\n%s", expected.String(), out)
		}
		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd jaeger check' command timed-out (%s)", timeout), err)
	}

	TestHelper.WithDataPlaneNamespace(ctx, "tracing-test", map[string]string{}, t, func(t *testing.T, namespace string) {
		emojivotoYaml, err := testutil.ReadFile("testdata/emojivoto.yaml")
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to read emojivoto yaml",
				"failed to read emojivoto yaml\n%s\n", err)
		}

		out, err = TestHelper.KubectlApply(emojivotoYaml, namespace)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}

		// wait for deployments to start
		for _, deploy := range []struct {
			ns   string
			name string
		}{
			{ns: namespace, name: "vote-bot"},
			{ns: namespace, name: "web"},
			{ns: namespace, name: "emoji"},
			{ns: namespace, name: "voting"},
			{ns: tracingNs, name: "collector"},
			{ns: tracingNs, name: "jaeger"},
		} {
			if err := TestHelper.CheckPods(ctx, deploy.ns, deploy.name, 1); err != nil {
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedError(t, "CheckPods timed-out", err)
				}
			}
		}

		t.Run("expect full trace", func(t *testing.T) {
			timeout := 3 * time.Minute
			err = TestHelper.RetryFor(timeout, func() error {
				url, err := TestHelper.URLFor(ctx, tracingNs, "jaeger", 16686)
				if err != nil {
					return err
				}

				tracesJSON, err := TestHelper.HTTPGetURL(url + "/jaeger/api/traces?lookback=1h&service=linkerd-proxy")
				if err != nil {
					return err
				}
				traces := traces{}

				err = json.Unmarshal([]byte(tracesJSON), &traces)
				if err != nil {
					return err
				}

				if !hasTraceWithProcess(&traces, "linkerd-proxy") {
					return fmt.Errorf("No trace found with processes: linkerd-proxy")
				}
				return nil
			})
			if err != nil {
				testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out checking trace (%s)", timeout), err)
			}
		})
	})
}

func hasTraceWithProcess(traces *traces, ps string) bool {
	for _, trace := range traces.Data {
		for _, process := range trace.Processes {
			if process.ServiceName == ps {
				return true
			}
		}
	}
	return false
}
