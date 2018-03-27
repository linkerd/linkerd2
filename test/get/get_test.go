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

	"github.com/runconduit/conduit/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestCliGet(t *testing.T) {
	out, err := TestHelper.ConduitRun("inject", "testdata/to_be_injected_application.yaml")
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

	out, err = TestHelper.ConduitRun("get", "pods")
	if err != nil {
		t.Fatalf("Unexpected error: %v output:\n%s", err, out)
	}

	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		t.Fatal("Expecting conduit get pods to return something, got nothing")
	}

	expectedPods := []string{
		"cli-get-test-d1", "cli-get-test-not-injected-d1", "cli-get-test-not-injected-d1", "cli-get-test-not-injected-d2", "cli-get-test-d1", "cli-get-test-d2",
	}

	var actualPods []string
	for _, line := range lines {
		sanitizedLine := strings.TrimSpace(line)
		if sanitizedLine == "" {
			continue
		}

		ns, pod, err := TestHelper.ParseNamespacedResource(sanitizedLine)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if ns == prefixedNs {
			podPrefix, err := parsePodPrefix(pod)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			actualPods = append(actualPods, podPrefix)
		}
	}

	sort.Strings(expectedPods)
	sort.Strings(actualPods)
	if !reflect.DeepEqual(expectedPods, actualPods) {
		t.Fatalf("Expected conduit get to return:\n%v\nBut got:\n%v\nRaw output:\n%s", expectedPods, actualPods, out)
	}
}

func parsePodPrefix(pod string) (string, error) {
	r := regexp.MustCompile("^(.+)-.+-.+$")
	matches := r.FindAllStringSubmatch(pod, 1)
	if len(matches) == 0 {
		return "", fmt.Errorf("string [%s] didn't contain expected format for pod name, extracted: %v", pod, matches)
	}
	return matches[0][1], nil
}
