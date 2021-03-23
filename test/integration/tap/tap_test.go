package tap

import (
	"context"
	"os"
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
	expectedT1 = testutil.TapEvent{
		Method:     "POST",
		Authority:  "t1-svc:9090",
		Path:       "/buoyantio.bb.TheService/theFunction",
		HTTPStatus: "200",
		GrpcStatus: "OK",
		TLS:        "true",
		LineCount:  3,
	}

	expectedT2 = testutil.TapEvent{
		Method:     "POST",
		Authority:  "t2-svc:9090",
		Path:       "/buoyantio.bb.TheService/theFunction",
		HTTPStatus: "200",
		GrpcStatus: "Unknown",
		TLS:        "true",
		LineCount:  3,
	}

	expectedT3 = testutil.TapEvent{
		Method:     "POST",
		Authority:  "t3-svc:8080",
		Path:       "/",
		HTTPStatus: "200",
		GrpcStatus: "",
		TLS:        "true",
		LineCount:  3,
	}

	expectedGateway = testutil.TapEvent{
		Method:     "GET",
		Authority:  "gateway-svc:8080",
		Path:       "/",
		HTTPStatus: "500",
		GrpcStatus: "",
		TLS:        "true",
		LineCount:  3,
	}
)

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestCliTap(t *testing.T) {
	out, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/tap_application.yaml")
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd inject' command failed", err)
	}

	ctx := context.Background()
	TestHelper.WithDataPlaneNamespace(ctx, "tap-test", map[string]string{}, t, func(t *testing.T, prefixedNs string) {

		out, err = TestHelper.KubectlApply(out, prefixedNs)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}

		// wait for deployments to start
		for _, deploy := range []string{"t1", "t2", "t3", "gateway"} {
			if err := TestHelper.CheckPods(ctx, prefixedNs, deploy, 1); err != nil {
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedError(t, "CheckPods timed-out", err)
				}
			}
		}

		t.Run("tap a deployment", func(t *testing.T) {
			events, err := testutil.Tap("deploy/t1", TestHelper, "--namespace", prefixedNs)
			if err != nil {
				testutil.AnnotatedFatal(t, "tap failed", err)
			}

			err = testutil.ValidateExpected(events, expectedT1)
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

			events, err := testutil.Tap("deploy/t1", TestHelper)
			if err != nil {
				testutil.AnnotatedFatal(t, "tap failed using context namespace", err)
			}

			err = testutil.ValidateExpected(events, expectedT1)
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
			out, stderr, err := TestHelper.PipeToLinkerdRun("", "viz", "tap", "deploy/t4", "--namespace", prefixedNs)
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
			expectedErr := `no pods to tap for deployment/t4
1 pods found with tap disabled via the viz.linkerd.io/disable-tap annotation:`
			split := strings.Split(stderr, "\n")
			actualErr := strings.Join(split[:2], "\n")
			if actualErr != expectedErr {
				testutil.AnnotatedFatalf(t, "unexpected error",
					"expected [%s], got: %s", expectedErr, actualErr)
			}
		})

		t.Run("tap a service call", func(t *testing.T) {
			events, err := testutil.Tap("deploy/gateway", TestHelper, "--to", "svc/t2-svc", "--namespace", prefixedNs)
			if err != nil {
				testutil.AnnotatedFatal(t, "failed tapping a service call", err)
			}

			err = testutil.ValidateExpected(events, expectedT2)
			if err != nil {
				testutil.AnnotatedFatal(t, "failed validating tapping a service call", err)
			}
		})

		t.Run("tap a pod", func(t *testing.T) {
			deploy := "t3"
			pods, err := TestHelper.GetPodNamesForDeployment(ctx, prefixedNs, deploy)
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to get pods for deployment t3",
					"failed to get pods for deployment [%s]\n%s", deploy, err)
			}

			if len(pods) != 1 {
				testutil.Fatalf(t, "expected exactly one pod for deployment [%s], got:\n%v", deploy, pods)
			}

			events, err := testutil.Tap("pod/"+pods[0], TestHelper, "--namespace", prefixedNs)
			if err != nil {
				testutil.AnnotatedFatal(t, "error tapping pod", err)
			}

			err = testutil.ValidateExpected(events, expectedT3)
			if err != nil {
				testutil.AnnotatedFatal(t, "error validating pod tap", err)
			}
		})

		t.Run("filter tap events by method", func(t *testing.T) {
			events, err := testutil.Tap("deploy/gateway", TestHelper, "--namespace", prefixedNs, "--method", "GET")
			if err != nil {
				testutil.AnnotatedFatal(t, "error filtering tap events by method", err)
			}

			err = testutil.ValidateExpected(events, expectedGateway)
			if err != nil {
				testutil.AnnotatedFatal(t, "error validating filtered tap events by method", err)
			}
		})

		t.Run("filter tap events by authority", func(t *testing.T) {
			events, err := testutil.Tap("deploy/gateway", TestHelper, "--namespace", prefixedNs, "--authority", "t1-svc:9090")
			if err != nil {
				testutil.AnnotatedFatal(t, "error filtering tap events by authority", err)
			}

			err = testutil.ValidateExpected(events, expectedT1)
			if err != nil {
				testutil.AnnotatedFatal(t, "error validating filtered tap events by authority", err)
			}
		})
	})

}
