package get

import (
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
	os.Exit(m.Run())
}

var (
	deployReplicas = map[string]int{
		"cli-get-test-d1":              2,
		"cli-get-test-d2":              1,
		"cli-get-test-not-injected-d1": 2,
		"cli-get-test-not-injected-d2": 1,
	}

	linkerdPods = map[string]int{
		"grafana":    1,
		"web":        1,
		"prometheus": 1,
		"controller": 1,
	}
)

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestCliGet(t *testing.T) {
	out, _, err := TestHelper.LinkerdRun("inject", "testdata/to_be_injected_application.yaml")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	prefixedNs := TestHelper.GetTestNamespace("get-test")
	out, err = TestHelper.KubectlApply(out, prefixedNs)
	if err != nil {
		t.Fatalf("Unexpected error: %v output:\n%s", err, out)
	}

	bytes, err := ioutil.ReadFile("testdata/not_to_be_injected_application.yaml")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	out, err = TestHelper.KubectlApply(string(bytes), prefixedNs)
	if err != nil {
		t.Fatalf("Unexpected error: %v output:\n%s", err, out)
	}

	// wait for pods to start
	for deploy, replicas := range deployReplicas {
		if err := TestHelper.CheckPods(prefixedNs, deploy, replicas); err != nil {
			t.Error(fmt.Errorf("Error validating pods for deploy [%s]:\n%s", deploy, err))
		}
	}

	t.Run("get pods from --all-namespaces", func(t *testing.T) {
		out, _, err = TestHelper.LinkerdRun("get", "pods", "--all-namespaces")

		if err != nil {
			t.Fatalf("Unexpected error: %v output:\n%s", err, out)
		}

		err := checkPodOutput(out, deployReplicas, prefixedNs)
		if err != nil {
			t.Fatalf("Pod output check failed:\n%s\nCommand output:\n%s", err, out)
		}
	})

	t.Run("get pods from the linkerd namespace", func(t *testing.T) {
		out, _, err = TestHelper.LinkerdRun("get", "pods", "-n", TestHelper.GetLinkerdNamespace())

		if err != nil {
			t.Fatalf("Unexpected error: %v output:\n%s", err, out)
		}

		err := checkPodOutput(out, linkerdPods, TestHelper.GetLinkerdNamespace())
		if err != nil {
			t.Fatalf("Pod output check failed:\n%s\nCommand output:\n%s", err, out)
		}
	})
}

func checkPodOutput(cmdOutput string, expectedPodCounts map[string]int, namespace string) error {
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
		return fmt.Errorf("Expected linkerd get to return:\n%v\nBut got:\n%v", expectedPods, actualPods)
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
