package externalresources

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestRabbitMQDeploy(t *testing.T) {
	ctx := context.Background()
	TestHelper.WithDataPlaneNamespace(ctx, "rabbitmq-test", map[string]string{}, t, func(t *testing.T, testNamespace string) {
		out, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/rabbitmq-server.yaml")
		// inject rabbitmq server
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd inject' command failed", "'linkerd inject' command failed: %s", err)
		}
		// deploy rabbitmq server
		_, err = TestHelper.KubectlApply(out, testNamespace)
		if err != nil {
			testutil.AnnotatedFatalf(t, "kubectl apply command failed", "'kubectl apply' command failed: %s", err)
		}
		if err := TestHelper.CheckPods(ctx, testNamespace, "rabbitmq", 1); err != nil {
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out %s", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out %s", err)
			}
		}
		// inject rabbitmq-client
		stdout, err := TestHelper.LinkerdRun("inject", "--manual", "testdata/rabbitmq-client.yaml")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd inject' command failed", "'linkerd inject' command failed: %s", err)
		}
		// deploy rabbitmq client
		_, err = TestHelper.KubectlApply(stdout, testNamespace)
		if err != nil {
			testutil.AnnotatedFatalf(t, "kubectl apply command failed", "'kubectl apply' command failed: %s", err)
		}
		if err := TestHelper.CheckPods(ctx, testNamespace, "rabbitmq-client", 1); err != nil {
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out %s", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out %s", err)
			}
		}
		// Verify client output
		golden := "check.rabbitmq.golden"
		timeout := 50 * time.Second
		err = TestHelper.RetryFor(timeout, func() error {
			out, err := TestHelper.Kubectl("", "-n", testNamespace, "logs", "-lapp=rabbitmq-client", "-crabbitmq-client")
			if err != nil {
				return fmt.Errorf("'kubectl logs -l app=rabbitmq-client -c rabbitmq-client' command failed\n%s", err)
			}
			err = TestHelper.ValidateOutput(out, golden)
			if err != nil {
				return fmt.Errorf("received unexpected output\n%s", err.Error())
			}
			return nil
		})
		if err != nil {
			testutil.AnnotatedFatal(t, fmt.Sprintf("'kubectl logs' command timed-out (%s)", timeout), err)
		}

	})

}
