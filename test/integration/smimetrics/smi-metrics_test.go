package smimetrics

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"testing"
	"text/template"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

type testCase struct {
	queryURL string
	filePath string
}

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(testutil.Run(m, TestHelper))
}

func TestSMIMetrics(t *testing.T) {

	// Install smi-metrics using Helm chart
	testNamespace := TestHelper.GetTestNamespace("smi-metrics-test")
	err := TestHelper.CreateDataPlaneNamespaceIfNotExists(testNamespace, nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create %s namespace: %s", testNamespace, err)
	}

	args := []string{
		"install",
		"smi-metrics",
		"smi-metrics.tgz",
		"--set",
		"adapter=linkerd",
		"--namespace",
		testNamespace,
	}

	if stdout, stderr, err := TestHelper.HelmRun(args...); err != nil {
		testutil.AnnotatedFatalf(t, "'helm install' command failed\n%s\n%s\n%v", stdout, stderr, err)
	}

	if err := TestHelper.CheckPods(testNamespace, "smi-metrics", 1); err != nil {
		if rce, ok := err.(*testutil.RestartCountError); ok {
			testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
		} else {
			testutil.AnnotatedError(t, "CheckPods timed-out", err)
		}
	}

	if err := TestHelper.CheckDeployment(testNamespace, "smi-metrics", 1); err != nil {
		testutil.AnnotatedErrorf(t, "CheckDeployment timed-out", "error validating deployment [%s]:\n%s", "terminus", err)
	}

	testCases := []testCase{
		{
			queryURL: fmt.Sprintf("/apis/metrics.smi-spec.io/v1alpha1/namespaces/%s/deployments/linkerd-controller", TestHelper.GetLinkerdNamespace()),
			filePath: "testdata/resources.golden",
		},
		{
			queryURL: fmt.Sprintf("/apis/metrics.smi-spec.io/v1alpha1/namespaces/%s/deployments/linkerd-controller/edges", TestHelper.GetLinkerdNamespace()),
			filePath: "testdata/edges.golden",
		},
	}

	for _, tc := range testCases {
		queryArgs := []string{
			"--raw",
			tc.queryURL,
		}

		out, err := TestHelper.Kubectl("get", queryArgs...)
		if err != nil {
			testutil.Fatalf(t, "failed to query smi-metrics API: %s", err)
		}

		// check resources output
		vars := struct{ Ns string }{TestHelper.GetLinkerdNamespace()}
		tpl := template.Must(template.ParseFiles(tc.filePath))
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, vars); err != nil {
			testutil.Fatalf(t, "failed to parse %s template: %s", tc.filePath, err)
		}

		r := regexp.MustCompile(buf.String())
		if !r.MatchString(out) {
			testutil.Fatalf(t, "Expected output:\n%s\nactual:\n%s", buf.String(), out)
		}
	}

}
