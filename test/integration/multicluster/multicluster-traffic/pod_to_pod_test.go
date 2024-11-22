package multiclustertraffic

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/testutil"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

// TestPodToPodTraffic inspects the target cluster's web-svc pod to see if the
// source cluster's vote-bot has been able to hit it with requests. If it has
// successfully issued requests, then we'll see log messages.
//
// We verify that the service has been mirrored in remote discovery mode by
// checking that it had no endpoints in the source cluster.
func TestPodToPodTraffic(t *testing.T) {
	if err := TestHelper.SwitchContext(contexts[testutil.TargetContextKey]); err != nil {
		testutil.AnnotatedFatalf(t,
			"failed to rebuild helper clientset with new context",
			"failed to rebuild helper clientset with new context [%s]: %v",
			contexts[testutil.TargetContextKey], err)
	}

	ctx := context.Background()
	// Create emojivoto in target cluster, to be deleted at the end of the test.
	annotations := map[string]string{
		// "config.linkerd.io/proxy-log-level": "linkerd=debug,info",
	}
	TestHelper.WithDataPlaneNamespace(ctx, "emojivoto-p2p", annotations, t, func(t *testing.T, ns string) {
		t.Run("Deploy resources in source and target clusters", func(t *testing.T) {
			// Deploy vote-bot client in source-cluster
			o, err := TestHelper.KubectlWithContext("", contexts[testutil.SourceContextKey], "create", "ns", ns)
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to create ns", "failed to create ns: %s\n%s", err, o)
			}
			o, err = TestHelper.KubectlApplyWithContext("", contexts[testutil.SourceContextKey], "--namespace", ns, "-f", "testdata/vote-bot.yml")
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to install vote-bot", "failed to install vote-bot: %s\n%s", err, o)
			}

			out, err := TestHelper.KubectlApplyWithContext("", contexts[testutil.TargetContextKey], "--namespace", ns, "-f", "testdata/emojivoto-no-bot.yml")
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to install emojivoto", "failed to install emojivoto: %s\n%s", err, out)
			}

			timeout := time.Minute
			err = testutil.RetryFor(timeout, func() error {
				out, err = TestHelper.KubectlWithContext("", contexts[testutil.TargetContextKey], "--namespace", ns, "label", "service/web-svc", "mirror.linkerd.io/exported=remote-discovery")
				return err
			})
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to label web-svc", "%s\n%s", err, out)
			}
		})

		t.Run("Wait until target workloads are ready", func(t *testing.T) {
			// Wait until client is up and running in source cluster
			voteBotDeployReplica := map[string]testutil.DeploySpec{"vote-bot": {Namespace: ns, Replicas: 1}}
			TestHelper.WaitRolloutWithContext(t, voteBotDeployReplica, contexts[testutil.SourceContextKey])

			// Wait until "target" services and replicas are up and running.
			emojiDeployReplicas := map[string]testutil.DeploySpec{
				"web":    {Namespace: ns, Replicas: 1},
				"emoji":  {Namespace: ns, Replicas: 1},
				"voting": {Namespace: ns, Replicas: 1},
			}
			TestHelper.WaitRolloutWithContext(t, emojiDeployReplicas, targetCtx)
		})

		timeout := time.Minute
		t.Run("Ensure mirror service exists and has no endpoints", func(t *testing.T) {
			err := TestHelper.SwitchContext(contexts[testutil.SourceContextKey])
			if err != nil {
				testutil.AnnotatedFatal(t, "failed to switch contexts", err)
			}
			err = testutil.RetryFor(timeout, func() error {
				svc, err := TestHelper.GetService(ctx, ns, "web-svc-target")
				if err != nil {
					return err
				}
				remoteDiscovery, found := svc.Labels[k8s.RemoteDiscoveryLabel]
				if !found {
					testutil.AnnotatedFatal(t, "mirror service missing label", "mirror service missing label: "+k8s.RemoteDiscoveryLabel)
				}
				if remoteDiscovery != "target" {
					testutil.AnnotatedFatal(t, "mirror service has incorrect remote discovery", fmt.Sprintf("mirror service remote discovery was %s, expected %s", remoteDiscovery, "target"))
				}
				remoteService, found := svc.Labels[k8s.RemoteServiceLabel]
				if !found {
					testutil.AnnotatedFatal(t, "mirror service missing label", "mirror service missing label: "+k8s.RemoteServiceLabel)
				}
				if remoteService != "web-svc" {
					testutil.AnnotatedFatal(t, "mirror service has incorrect remote service", fmt.Sprintf("mirror service remote service was %s, expected %s", remoteService, "web-svc"))
				}
				_, err = TestHelper.GetEndpoints(ctx, ns, "web-svc-target")
				if err == nil {
					testutil.AnnotatedFatal(t, "mirror service should not have endpoints", "mirror service should not have endpoints")
				}
				if !kerrors.IsNotFound(err) {
					testutil.AnnotatedFatalf(t, "failed to retrieve mirror service endpoints", err.Error())
				}
				return nil
			})
			if err != nil {
				testutil.AnnotatedFatal(t, "timed-out verifying mirror service", err)
			}
		})

		err := testutil.RetryFor(timeout, func() error {
			out, err := TestHelper.KubectlWithContext("",
				targetCtx,
				"--namespace", ns,
				"logs",
				"--selector", "app=web-svc",
				"--container", "web-svc",
			)
			if err != nil {
				return fmt.Errorf("%w\n%s", err, out)
			}
			// Check for expected error messages
			for _, row := range strings.Split(out, "\n") {
				if strings.Contains(row, " /api/vote?choice=:doughnut: ") {
					return nil
				}
			}
			return fmt.Errorf("web-svc logs in target cluster do not include voting errors\n%s", out)
		})
		if err != nil {
			testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out waiting for traffic (%s)", timeout), err)
		}
	})
}
