package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
)

var TestHelper *testutil.TestHelper

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

func TestGateways(t *testing.T) {
	timeout := time.Minute
	err := TestHelper.RetryFor(timeout, func() error {
		out, err := TestHelper.LinkerdRun("multicluster", "gateways")
		if err != nil {
			return err
		}
		rows := strings.Split(out, "\n")
		if len(rows) < 2 {
			return errors.New("response is empty")
		}
		fields := strings.Fields(rows[1])
		if len(fields) < 6 {
			return fmt.Errorf("unexpected number of columns: %d", len(fields))
		}
		if fields[0] != "target" {
			return fmt.Errorf("unexpected target cluster name: %s", fields[0])
		}
		if fields[1] != "True" {
			return errors.New("target cluster is not alive")
		}
		if fields[2] != "1" {
			return fmt.Errorf("invalid NUM_SVC: %s", fields[2])
		}

		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd multicluster gateways' command timed-out (%s)", timeout), err)
	}
}

func TestInstallVoteBot(t *testing.T) {
	if err := TestHelper.CreateDataPlaneNamespaceIfNotExists(context.Background(), "emojivoto", nil); err != nil {
		testutil.AnnotatedFatalf(t, "failed to create emojivoto namespace",
			"failed to create emojivoto namespace: %s", err)
	}
	yaml, err := testutil.ReadFile("testdata/vote-bot.yml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read 'vote_bot.yml'", "failed to read 'vote_bot.yml': %s", err)
	}
	o, err := TestHelper.KubectlApply(yaml, "emojivoto")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to install vote-bot", "failed to install vote-bot: %s\n%s", err, o)
	}
}

func TestInstallSlowCooker(t *testing.T) {
	err := TestHelper.CreateDataPlaneNamespaceIfNotExists(context.Background(), "multicluster-statefulset", nil)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to create multicluster-statefulset namespace", "failed to create multicluster-statefulset namespace: %s", err)
	}

	slowcooker, err := TestHelper.LinkerdRun("inject", "testdata/slow-cooker.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to inject slow-cooker manifest", "failed to inject slow-cooker manifest: %s", err)
	}

	out, err := TestHelper.KubectlApply(slowcooker, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to install slow-cooker", "failed to install slow-cooker: %s\ngot: %s", err, out)
	}

}
