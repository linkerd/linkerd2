package tracing

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
	os.Exit(testutil.Run(m, TestHelper))
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestTracing(t *testing.T) {
	if os.Getenv("RUN_ARM_TEST") != "" {
		t.Skip("Skipped. Jaeger & Open Census images does not support ARM yet")
	}

	// Tracing Components
	out, stderr, err := TestHelper.LinkerdRun("inject", "testdata/tracing.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd inject' command failed",
			"'linkerd inject' command failed\n%s\n%s", out, stderr)
	}

	tracingNs := TestHelper.GetTestNamespace("tracing")
	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(tracingNs, nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", tracingNs),
			"failed to create %s namespace: %s", tracingNs, err)
	}
	out, err = TestHelper.KubectlApply(out, tracingNs)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// Emojivoto components
	emojivotoNs := TestHelper.GetTestNamespace("emojivoto")
	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(emojivotoNs, nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", emojivotoNs),
			"failed to create %s namespace: %s", emojivotoNs, err)
	}

	emojivotoYaml, err := testutil.ReadFile("testdata/emojivoto.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read emojivoto yaml",
			"failed to read emojivoto yaml\n%s\n", err)
	}
	emojivotoYaml = strings.ReplaceAll(emojivotoYaml, "___TRACING_NS___", tracingNs)
	out, stderr, err = TestHelper.PipeToLinkerdRun(emojivotoYaml, "inject", "-")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd inject' command failed",
			"'linkerd inject' command failed\n%s\n%s", out, stderr)
	}

	out, err = TestHelper.KubectlApply(out, emojivotoNs)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// Ingress components
	// Ingress must run in the same namespace as the service it routes to (web)
	ingressNs := emojivotoNs
	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(ingressNs, nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", ingressNs),
			"failed to create %s namespace: %s", ingressNs, err)
	}

	ingressYaml, err := testutil.ReadFile("testdata/ingress.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read ingress yaml",
			"failed to read ingress yaml\n%s\n", err)
	}
	ingressYaml = strings.ReplaceAll(ingressYaml, "___INGRESS_NAMESPACE___", ingressNs)
	ingressYaml = strings.ReplaceAll(ingressYaml, "___TRACING_NS___", tracingNs)
	out, stderr, err = TestHelper.PipeToLinkerdRun(ingressYaml, "inject", "-")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd inject' command failed",
			"'linkerd inject' command failed\n%s\n%s", out, stderr)
	}

	out, err = TestHelper.KubectlApply(out, ingressNs)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// wait for deployments to start
	for ns, deploy := range map[string]string{
		emojivotoNs: "vote-bot",
		emojivotoNs: "web",
		emojivotoNs: "emoji",
		emojivotoNs: "voting",
		ingressNs:   "nginx-ingress",
		tracingNs:   "oc-collector",
		tracingNs:   "jaeger",
	} {
		if err := TestHelper.CheckPods(ns, deploy, 1); err != nil {
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out", err)
			}
		}

		if err := TestHelper.CheckDeployment(ns, deploy, 1); err != nil {
			testutil.AnnotatedErrorf(t, "CheckDeployment timed-out", "Error validating deployment [%s]:\n%s", deploy, err)
		}
	}

	t.Run("expect full trace", func(t *testing.T) {

		url, err := TestHelper.URLFor(tracingNs, "jaeger", 16686)
		if err != nil {
			testutil.AnnotatedFatal(t, "error building URL", err)
		}
		timeout := 120 * time.Second
		err = TestHelper.RetryFor(timeout, func() error {
			tracesJSON, err := TestHelper.HTTPGetURL(url + "/api/traces?lookback=1h&service=nginx")
			if err != nil {
				return err
			}
			traces := traces{}

			err = json.Unmarshal([]byte(tracesJSON), &traces)
			if err != nil {
				return err
			}

			processes := []string{"nginx", "web", "emoji", "voting", "linkerd-proxy"}
			if !hasTraceWithProcesses(&traces, processes) {
				return fmt.Errorf("No trace found with processes: %s", processes)
			}
			return nil
		})
		if err != nil {
			testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out checking trace (%s)", timeout), err)
		}
	})
}

func hasTraceWithProcesses(traces *traces, ps []string) bool {
	for _, trace := range traces.Data {
		if containsProcesses(trace, ps) {
			return true
		}
	}
	return false
}

func containsProcesses(trace trace, ps []string) bool {
	toFind := make(map[string]struct{})
	for _, p := range ps {
		toFind[p] = struct{}{}
	}
	for _, p := range trace.Processes {
		delete(toFind, p.ServiceName)
	}
	return len(toFind) == 0
}
