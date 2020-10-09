package get

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(testutil.Run(m, TestHelper))
}

var (
	deployReplicas = map[string]int{
		"cli-get-test-d1":              2,
		"cli-get-test-d2":              1,
		"cli-get-test-not-injected-d1": 2,
		"cli-get-test-not-injected-d2": 1,
	}

	linkerdPods = map[string]int{
		"linkerd-controller":     1,
		"linkerd-destination":    1,
		"linkerd-grafana":        1,
		"linkerd-identity":       1,
		"linkerd-prometheus":     1,
		"linkerd-proxy-injector": 1,
		"linkerd-sp-validator":   1,
		"linkerd-tap":            1,
		"linkerd-web":            1,
	}
)

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestCliGet(t *testing.T) {
	out, err := TestHelper.LinkerdRunOk("inject", "testdata/to_be_injected_application.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "unexpected error", "%s", err)
	}

	ctx := context.Background()
	prefixedNs := TestHelper.GetTestNamespace("get-test")
	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(ctx, prefixedNs, nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create namespace", "failed to create %s namespace: %s", prefixedNs, err)
	}
	out, err = TestHelper.KubectlApply(out, prefixedNs)
	if err != nil {
		testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v output:\n%s", err, out)
	}

	bytes, err := ioutil.ReadFile("testdata/not_to_be_injected_application.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v", err)
	}

	out, err = TestHelper.KubectlApply(string(bytes), prefixedNs)
	if err != nil {
		testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v output:\n%s", err, out)
	}

	// wait for pods to start
	for deploy, replicas := range deployReplicas {
		if err := TestHelper.CheckPods(ctx, prefixedNs, deploy, replicas); err != nil {
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out", err)
			}
		}
	}

	t.Run("get pods from --all-namespaces", func(t *testing.T) {
		out, err = TestHelper.LinkerdRunOk("get", "pods", "--all-namespaces")
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "%s", err)
		}

		err := checkPodOutput(out, deployReplicas, "", prefixedNs)
		if err != nil {
			testutil.AnnotatedFatalf(t, "pod output check failed", "pod output check failed:\n%s\nCommand output:\n%s", err, out)
		}
	})

	t.Run("get pods from the linkerd namespace", func(t *testing.T) {
		out, err = TestHelper.LinkerdRunOk("get", "pods", "-n", TestHelper.GetLinkerdNamespace())
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "%s", err)
		}

		err := checkPodOutput(out, linkerdPods, "linkerd-heartbeat", TestHelper.GetLinkerdNamespace())
		if err != nil {
			testutil.AnnotatedFatalf(t, "pod output check failed", "pod output check failed:\n%s\nCommand output:\n%s", err, out)
		}
	})

	t.Run("get pods from the default namespace of current context", func(t *testing.T) {
		out, err := TestHelper.Kubectl("", "config", "set-context", "--namespace="+TestHelper.GetLinkerdNamespace(), "--current")
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v output:\n%s", err, out)
		}

		out, err = TestHelper.LinkerdRunOk("get", "pods")
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "%s", err)
		}

		err = checkPodOutput(out, linkerdPods, "linkerd-heartbeat", TestHelper.GetLinkerdNamespace())
		if err != nil {
			testutil.AnnotatedFatalf(t, "pod output check failed", "pod output check failed:\n%s\nCommand output:\n%s", err, out)
		}

		out, err = TestHelper.Kubectl("", "config", "set-context", "--namespace=default", "--current")
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v output:\n%s", err, out)
		}
	})
}

func checkPodOutput(cmdOutput string, expectedPodCounts map[string]int, optionalPod string, namespace string) error {
	expectedPods := []string{}
	for podName, replicas := range expectedPodCounts {
		for i := 0; i < replicas; i++ {
			expectedPods = append(expectedPods, podName)
		}
	}

	lines := strings.Split(cmdOutput, "\n")
	if len(lines) == 0 {
		return fmt.Errorf("Expecting linkerd get pods to return something, got nothing")
	}

	var actualPods []string
	for _, line := range lines {
		sanitizedLine := strings.TrimSpace(line)
		if sanitizedLine == "" {
			continue
		}

		ns, pod, err := TestHelper.ParseNamespacedResource(sanitizedLine)
		if err != nil {
			return fmt.Errorf("Unexpected error: %v", err)
		}

		if ns == namespace {
			podPrefix, err := parsePodPrefix(pod)

			if err != nil {
				return fmt.Errorf("Unexpected error: %v", err)
			}
			actualPods = append(actualPods, podPrefix)
		}
	}

	sort.Strings(expectedPods)
	sort.Strings(actualPods)
	if !reflect.DeepEqual(expectedPods, actualPods) {
		if optionalPod == "" {
			return fmt.Errorf("Expected linkerd get to return:\n%v\nBut got:\n%v", expectedPods, actualPods)
		}

		expectedPlusOptionalPods := append(expectedPods, optionalPod)
		sort.Strings(expectedPlusOptionalPods)
		if !reflect.DeepEqual(expectedPlusOptionalPods, actualPods) {
			return fmt.Errorf("Expected linkerd get to return:\n%v\nor:\n%v\nBut got:\n%v", expectedPods, expectedPlusOptionalPods, actualPods)
		}
	}

	return nil
}

func parsePodPrefix(pod string) (string, error) {
	r := regexp.MustCompile("^(.+)-.+-.+$")
	matches := r.FindAllStringSubmatch(pod, 1)
	if len(matches) == 0 {
		return "", fmt.Errorf("string [%s] didn't contain expected format for pod name, extracted: %v", pod, matches)
	}
	return matches[0][1], nil
}
