package tracing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
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
		Spans     []span             `json:"spans"`
	}

	process struct {
		ServiceName string `json:"serviceName"`
		Tags        []tag  `json:"tags"`
	}

	span struct {
		OperationName string `json:"operationName"`
		Tags          []tag  `json:"tags"`
		ProcessId     string `json:"processID"`
	}

	tag struct {
		Key   string      `json:"key"`
		Type  string      `json:"type"`
		Value interface{} `json:"value"`
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

				expected := []expectedTrace{
					{
						serviceName: "linkerd-proxy",
						app:         "web-svc",
						processTags: map[string]tagMatcher{
							"host.name":                   anyStringMatcher{},
							"k8s.container.name":          stringMatcher("linkerd-proxy"),
							"k8s.pod.ip":                  anyStringMatcher{},
							"k8s.pod.uid":                 anyStringMatcher{},
							"linkerd.io/control-plane-ns": stringMatcher("linkerd"),
							"linkerd.io/proxy-deployment": stringMatcher("web"),
							"linkerd.io/workload-ns":      stringMatcher(namespace),
							"pod-template-hash":           anyStringMatcher{},
							"process.pid":                 anyMatcher{},
							"process.start_timestamp":     anyMatcher{},
						},
						operation: "/api/vote",
						spanKind:  "server",
						spanTags: map[string]tagMatcher{
							"http.request.method":                stringMatcher("GET"),
							"url.scheme":                         stringMatcher("http"),
							"url.path":                           stringMatcher("/api/vote"),
							"url.query":                          anyStringMatcher{},
							"url.full":                           anyStringMatcher{},
							"network.transport":                  stringMatcher("tcp"),
							"server.address":                     stringMatcher("web-svc"),
							"server.port":                        stringMatcher("80"),
							"user_agent.original":                anyStringMatcher{},
							"http.request.header.l5d-orig-proto": stringMatcher("HTTP/1.1"),
							"direction":                          stringMatcher("inbound"),
							"http.response.status_code":          stringMatcher("200"),
						},
					},
				}

				for _, e := range expected {
					err := inspectTraces(t, &traces, e)
					if err != nil {
						return err
					}
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

type expectedTrace struct {
	serviceName string
	app         string
	processTags map[string]tagMatcher
	operation   string
	spanKind    string
	spanTags    map[string]tagMatcher
}

type tagMatcher interface {
	assertMatches(t *testing.T, key string, value interface{})
}

type stringMatcher string

func (expected stringMatcher) assertMatches(t *testing.T, key string, actual interface{}) {
	anyStringMatcher{}.assertMatches(t, key, actual)
	if string(expected) != actual.(string) {
		t.Fatalf("Tag %s found with incorrect value\nExpected %s, found %s", key, string(expected), actual.(string))
	}
}

type anyStringMatcher struct{}

func (_ anyStringMatcher) assertMatches(t *testing.T, key string, value interface{}) {
	_, ok := value.(string)
	if !ok {
		t.Fatalf("Tag %s has incorrect type\nexpected string, found %s", key, reflect.TypeOf(value).Name())
	}
}

type anyMatcher struct{}

func (_ anyMatcher) assertMatches(_ *testing.T, _ string, _ interface{}) {}

func inspectTraces(t *testing.T, traces *traces, expected expectedTrace) error {
	for _, trace := range traces.Data {
		var matchedProcess = ""
		for id, process := range trace.Processes {
			if process.ServiceName != expected.serviceName {
				continue
			}

			var appMatches = false
			for _, tag := range process.Tags {
				if tag.Key != "app" {
					continue
				}
				value, ok := tag.Value.(string)
				if !ok {
					continue
				}
				if value != expected.app {
					break
				}
				appMatches = true
				break
			}
			if !appMatches {
				continue
			}

			assertContainsTags(t, process.Tags, expected.processTags)

			matchedProcess = id
			break
		}
		if matchedProcess == "" {
			continue
		}

		for _, span := range trace.Spans {
			if span.ProcessId != matchedProcess {
				continue
			}

			if span.OperationName != expected.operation {
				continue
			}

			var kindMatches = false
			for _, tag := range span.Tags {
				value, ok := tag.Value.(string)
				if tag.Key != "span.kind" {
					continue
				}
				if !ok {
					continue
				}
				if value != expected.spanKind {
					break
				}
				kindMatches = true
				break
			}
			if !kindMatches {
				continue
			}

			assertContainsTags(t, span.Tags, expected.spanTags)

			return nil
		}
	}

	return noProxyTraceFound{traces}
}

func assertContainsTags(t *testing.T, rawTags []tag, expected map[string]tagMatcher) {
	tags := map[string]interface{}{}
	for _, tag := range rawTags {
		tags[tag.Key] = tag.Value
	}

	for key, expectedValue := range expected {
		actual, ok := tags[key]
		if !ok {
			t.Fatalf("Tag %s not found in tags\nTags: %v", key, tags)
		}
		expectedValue.assertMatches(t, key, actual)
	}
}

type noProxyTraceFound struct {
	traces *traces
}

func (e noProxyTraceFound) Error() string {
	return fmt.Sprintf("no trace found with processes: linkerd-proxy\n%v", e.traces.Data)
}
