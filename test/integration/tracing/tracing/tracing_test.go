package tracing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

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
	// Block test execution until viz extension is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdDeployReplicasEdge)
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestTracing(t *testing.T) {
	ctx := context.Background()

	tracingNs := "tracing"
	installTracing(t, tracingNs)

	TestHelper.WithDataPlaneNamespace(ctx, "tracing-test", map[string]string{}, t, func(t *testing.T, namespace string) {
		emojivotoYaml, err := testutil.ReadFile("testdata/emojivoto.yaml")
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to read emojivoto yaml",
				"failed to read emojivoto yaml\n%s\n", err)
		}

		out, err := TestHelper.KubectlApply(emojivotoYaml, namespace)
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
			{ns: tracingNs, name: "otel-collector-opentelemetry-collector"},
			{ns: tracingNs, name: "jaeger"},
		} {
			if err := TestHelper.CheckPods(ctx, deploy.ns, deploy.name, 1); err != nil {
				var rce *testutil.RestartCountError
				if errors.As(err, &rce) {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				}
			}
		}

		t.Run("expect full trace", func(t *testing.T) {
			timeout := 1 * time.Minute
			err = testutil.RetryFor(timeout, func() error {
				url, err := TestHelper.URLFor(ctx, tracingNs, "jaeger", 16686)
				if err != nil {
					return err
				}

				tracesJSON, err := TestHelper.HTTPGetURL(url + "/api/traces?lookback=1h&service=linkerd-proxy")
				if err != nil {
					return err
				}
				traces := traces{}

				err = json.Unmarshal([]byte(tracesJSON), &traces)
				if err != nil {
					return err
				}

				if !hasTraceWithProcess(&traces, "linkerd-proxy") {
					return noProxyTraceFound{}
				}
				return nil
			})
			if err != nil {
				testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out checking trace (%s)", timeout), err)
			}
		})
	})
}

func installTracing(t *testing.T, namespace string) {
	tracingYaml, err := testutil.ReadFile("testdata/tracing.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read tracing yaml",
			"failed to read emojivoto yaml\n%s\n", err)
	}

	out, err := TestHelper.KubectlApply(tracingYaml, namespace)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	stdout, stderr, err := TestHelper.HelmRun("repo", "add", "jaegertracing", "https://jaegertracing.github.io/helm-charts")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to add jaeger repository", "failed to add jaeger repository\n%s\n------\n%s\n", stdout, stderr)
	}
	stdout, stderr, err = TestHelper.HelmRun("repo", "add", "open-telemetry", "https://open-telemetry.github.io/opentelemetry-helm-charts")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to add OpenTelemetry repository", "failed to add OpenTelemetry repository\n%s\n------\n%s\n", stdout, stderr)
	}
	stdout, stderr, err = TestHelper.HelmRun("install", "jaeger", "jaegertracing/jaeger", "--namespace=tracing", "--values=testdata/jaeger-aio-values.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to install jaeger", "failed to install jaeger\n%s\n------\n%s\n", stdout, stderr)
	}
	stdout, stderr, err = TestHelper.HelmRun("install", "otel-collector", "open-telemetry/opentelemetry-collector", "--namespace=tracing", "--values=testdata/otel-values.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to install OpenTelemetry", "failed to install OpenTelemetry\n%s\n------\n%s\n", stdout, stderr)
	}
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

type noProxyTraceFound struct{}

func (e noProxyTraceFound) Error() string {
	return "no trace found with processes: linkerd-proxy"
}
