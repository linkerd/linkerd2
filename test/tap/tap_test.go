package tap

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

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

type tapEvent struct {
	method     string
	authority  string
	path       string
	httpStatus string
	grpcStatus string
	tls        string
	lineCount  int
}

var (
	expectedT1 = tapEvent{
		method:     "POST",
		authority:  "t1-svc:9090",
		path:       "/buoyantio.bb.TheService/theFunction",
		httpStatus: "200",
		grpcStatus: "OK",
		tls:        "true",
		lineCount:  3,
	}

	expectedT2 = tapEvent{
		method:     "POST",
		authority:  "t2-svc:9090",
		path:       "/buoyantio.bb.TheService/theFunction",
		httpStatus: "200",
		grpcStatus: "Unknown",
		tls:        "true",
		lineCount:  3,
	}

	expectedT3 = tapEvent{
		method:     "POST",
		authority:  "t3-svc:8080",
		path:       "/",
		httpStatus: "200",
		grpcStatus: "",
		tls:        "true",
		lineCount:  3,
	}

	expectedGateway = tapEvent{
		method:     "GET",
		authority:  "gateway-svc:8080",
		path:       "/",
		httpStatus: "500",
		grpcStatus: "",
		tls:        "true",
		lineCount:  3,
	}
)

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestCliTap(t *testing.T) {
	out, stderr, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/tap_application.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd inject' command failed",
			"'linkerd inject' command failed\n%s\n%s", out, stderr)
	}

	prefixedNs := TestHelper.GetTestNamespace("tap-test")
	err = TestHelper.CreateDataPlaneNamespaceIfNotExists(prefixedNs, nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", prefixedNs),
			"failed to create %s namespace: %s", prefixedNs, err)
	}
	out, err = TestHelper.KubectlApply(out, prefixedNs)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// wait for deployments to start
	for _, deploy := range []string{"t1", "t2", "t3", "gateway"} {
		if err := TestHelper.CheckPods(prefixedNs, deploy, 1); err != nil {
			testutil.AnnotatedError(t, "CheckPods timed-out", err)
		}

		if err := TestHelper.CheckDeployment(prefixedNs, deploy, 1); err != nil {
			testutil.AnnotatedErrorf(t, "CheckDeployment timed-out", "Error validating deployment [%s]:\n%s", deploy, err)
		}
	}

	t.Run("tap a deployment", func(t *testing.T) {
		events, err := tap("deploy/t1", "--namespace", prefixedNs)
		if err != nil {
			testutil.AnnotatedFatal(t, "tap failed", err)
		}

		err = validateExpected(events, expectedT1)
		if err != nil {
			testutil.AnnotatedFatal(t, "validating tap failed", err)
		}
	})

	t.Run("tap a deployment using context namespace", func(t *testing.T) {
		out, err := TestHelper.Kubectl("", "config", "set-context", "--namespace="+prefixedNs, "--current")
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error",
				"unexpected error: %v output:\n%s", err, out)
		}

		events, err := tap("deploy/t1")
		if err != nil {
			testutil.AnnotatedFatal(t, "tap failed using context namespace", err)
		}

		err = validateExpected(events, expectedT1)
		if err != nil {
			testutil.AnnotatedFatal(t, "validating tap failed using context namespace", err)
		}

		out, err = TestHelper.Kubectl("", "config", "set-context", "--namespace=default", "--current")
		if err != nil {
			testutil.AnnotatedFatalf(t, "unexpected error",
				"unexpected error: %v output:\n%s", err, out)
		}
	})

	t.Run("tap a disabled deployment", func(t *testing.T) {
		out, stderr, err := TestHelper.LinkerdRun("tap", "deploy/t4", "--namespace", prefixedNs)
		if out != "" {
			testutil.AnnotatedFatalf(t, "unexpected output",
				"unexpected output: %s", out)
		}
		if err == nil {
			testutil.Fatal(t, "expected an error, got none")
		}
		if stderr == "" {
			testutil.Fatal(t, "expected an error, got none")
		}
		expectedErr := "Error: all pods found for deployment/t4 have tapping disabled"
		if errs := strings.Split(stderr, "\n"); errs[0] != expectedErr {
			testutil.AnnotatedFatalf(t, "unexpected error",
				"expected [%s], got: %s", expectedErr, errs[0])
		}
	})

	t.Run("tap a service call", func(t *testing.T) {
		events, err := tap("deploy/gateway", "--to", "svc/t2-svc", "--namespace", prefixedNs)
		if err != nil {
			testutil.AnnotatedFatal(t, "failed tapping a service call", err)
		}

		err = validateExpected(events, expectedT2)
		if err != nil {
			testutil.AnnotatedFatal(t, "failed validating tapping a service call", err)
		}
	})

	t.Run("tap a pod", func(t *testing.T) {
		deploy := "t3"
		pods, err := TestHelper.GetPodNamesForDeployment(prefixedNs, deploy)
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to get pods for deployment t3",
				"failed to get pods for deployment [%s]\n%s", deploy, err)
		}

		if len(pods) != 1 {
			testutil.Fatalf(t, "expected exactly one pod for deployment [%s], got:\n%v", deploy, pods)
		}

		events, err := tap("pod/"+pods[0], "--namespace", prefixedNs)
		if err != nil {
			testutil.AnnotatedFatal(t, "error tapping pod", err)
		}

		err = validateExpected(events, expectedT3)
		if err != nil {
			testutil.AnnotatedFatal(t, "error validating pod tap", err)
		}
	})

	t.Run("filter tap events by method", func(t *testing.T) {
		events, err := tap("deploy/gateway", "--namespace", prefixedNs, "--method", "GET")
		if err != nil {
			testutil.AnnotatedFatal(t, "error filtering tap events by method", err)
		}

		err = validateExpected(events, expectedGateway)
		if err != nil {
			testutil.AnnotatedFatal(t, "error validating filtered tap events by method", err)
		}
	})

	t.Run("filter tap events by authority", func(t *testing.T) {
		events, err := tap("deploy/gateway", "--namespace", prefixedNs, "--authority", "t1-svc:9090")
		if err != nil {
			testutil.AnnotatedFatal(t, "error filtering tap events by authority", err)
		}

		err = validateExpected(events, expectedT1)
		if err != nil {
			testutil.AnnotatedFatal(t, "error validating filtered tap events by authority", err)
		}
	})

}

// executes a tap command and converts the command's streaming output into tap
// events using each line's "id" field
func tap(target string, arg ...string) ([]*tapEvent, error) {
	cmd := append([]string{"tap", target}, arg...)
	outputStream, err := TestHelper.LinkerdRunStream(cmd...)
	if err != nil {
		return nil, err
	}
	defer outputStream.Stop()

	outputLines, err := outputStream.ReadUntil(10, 1*time.Minute)
	if err != nil {
		return nil, err
	}

	tapEventByID := make(map[string]*tapEvent)
	for _, line := range outputLines {
		fields := toFieldMap(line)
		obj, ok := tapEventByID[fields["id"]]
		if !ok {
			obj = &tapEvent{}
			tapEventByID[fields["id"]] = obj
		}
		obj.lineCount++
		obj.tls = fields["tls"]

		switch fields["type"] {
		case "req":
			obj.method = fields[":method"]
			obj.authority = fields[":authority"]
			obj.path = fields[":path"]
		case "rsp":
			obj.httpStatus = fields[":status"]
		case "end":
			obj.grpcStatus = fields["grpc-status"]
		}
	}

	output := make([]*tapEvent, 0)
	for _, obj := range tapEventByID {
		if obj.lineCount == 3 { // filter out incomplete events
			output = append(output, obj)
		}
	}

	return output, nil
}

func toFieldMap(line string) map[string]string {
	fields := strings.Fields(line)
	fieldMap := map[string]string{"type": fields[0]}
	for _, field := range fields[1:] {
		parts := strings.SplitN(field, "=", 2)
		fieldMap[parts[0]] = parts[1]
	}
	return fieldMap
}

func validateExpected(events []*tapEvent, expectedEvent tapEvent) error {
	if len(events) == 0 {
		return fmt.Errorf("Expected tap events, got nothing")
	}
	for _, event := range events {
		if *event != expectedEvent {
			return fmt.Errorf("Unexpected tap event [%+v]; expected=[%+v]", *event, expectedEvent)
		}
	}
	return nil
}
