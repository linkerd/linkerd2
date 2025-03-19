package multiclustertraffic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/testutil"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

// TestFederatedService deploys emojivoto to two clusters and has the web-svc
// in both clusters join a federated service. It creates a vote-bot in the
// source cluster which sends traffic to the federated service and then checks
// the logs of the web-svc in both clusters. If it has successfully issued
// requests, then we'll see log messages.
//
// We verify that the federated service exists and has no endpoints in the
// source cluster.
func TestFederatedService(t *testing.T) {
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
	TestHelper.WithDataPlaneNamespace(ctx, "emojivoto-federated", annotations, t, func(t *testing.T, ns string) {
		t.Run("Deploy resources in source and target clusters", func(t *testing.T) {
			// Deploy federated-client in source-cluster
			o, err := TestHelper.KubectlWithContext("", contexts[testutil.SourceContextKey], "create", "ns", ns)
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to create ns", "failed to create ns: %s\n%s", err, o)
			}
			o, err = TestHelper.KubectlApplyWithContext("", contexts[testutil.SourceContextKey], "--namespace", ns, "-f", "testdata/federated-client.yml")
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to install federated-client", "failed to installfederated-client: %s\n%s", err, o)
			}

			// Deploy emojivoto in both clusters
			for _, ctx := range contexts {
				out, err := TestHelper.KubectlApplyWithContext("", ctx, "--namespace", ns, "-f", "testdata/emojivoto-no-bot.yml")
				if err != nil {
					testutil.AnnotatedFatalf(t, "failed to install emojivoto", "failed to install emojivoto: %s\n%s", err, out)
				}

				// Label the service to join the federated service and add
				// labels to the service so we can ensure they are copied
				// correctly to the federated service.
				timeout := time.Minute
				err = testutil.RetryFor(timeout, func() error {
					for _, label := range []string{
						"mirror.linkerd.io/federated=member",
						"evil.linkerd/a=b",
						"evil=yes",
						"good.linkerd/c=d",
						"good=yes",
					} {
						out, err = TestHelper.KubectlWithContext("", ctx, "--namespace", ns, "label", "service/web-svc", label)
						if err != nil {
							return err
						}
					}
					out, err = TestHelper.KubectlWithContext("", ctx, "--namespace", ns, "label", "service/web-svc", "test-context="+ctx)
					if err != nil {
						return err
					}
					return nil
				})
				if err != nil {
					testutil.AnnotatedFatalf(t, "failed to label web-svc", "%s\n%s", err, out)
				}

				// Add annotations to the service so we can ensure they are
				// copied correctly to the federated service.
				err = testutil.RetryFor(timeout, func() error {
					for _, annotation := range []string{
						"evil.linkerd/a=b",
						"evil=yes",
						"good.linkerd/c=d",
						"good=yes",
					} {
						out, err = TestHelper.KubectlWithContext("", ctx, "--namespace", ns, "annotate", "service/web-svc", annotation)
						if err != nil {
							return err
						}
					}
					out, err = TestHelper.KubectlWithContext("", ctx, "--namespace", ns, "annotate", "service/web-svc", "test-context="+ctx)
					if err != nil {
						return err
					}
					return nil
				})
				if err != nil {
					testutil.AnnotatedFatalf(t, "failed to annotate web-svc", "%s\n%s", err, out)
				}
			}
		})

		t.Run("Wait until target workloads are ready", func(t *testing.T) {
			// Wait until client is up and running in source cluster
			voteBotDeployReplica := map[string]testutil.DeploySpec{"vote-bot": {Namespace: ns, Replicas: 1}}
			TestHelper.WaitRolloutWithContext(t, voteBotDeployReplica, contexts[testutil.SourceContextKey])

			// Wait until services and replicas are up and running.
			emojiDeployReplicas := map[string]testutil.DeploySpec{
				"web":    {Namespace: ns, Replicas: 1},
				"emoji":  {Namespace: ns, Replicas: 1},
				"voting": {Namespace: ns, Replicas: 1},
			}
			for _, ctx := range contexts {
				TestHelper.WaitRolloutWithContext(t, emojiDeployReplicas, ctx)
			}

		})

		timeout := 3 * time.Minute
		t.Run("Ensure federated service exists and has no endpoints", func(t *testing.T) {
			err := TestHelper.SwitchContext(contexts[testutil.SourceContextKey])
			if err != nil {
				testutil.AnnotatedFatal(t, "failed to switch contexts", err)
			}
			err = testutil.RetryFor(timeout, func() error {
				svc, err := TestHelper.GetService(ctx, ns, "web-svc-federated")
				if err != nil {
					return err
				}
				remoteDiscovery, found := svc.Annotations[k8s.RemoteDiscoveryAnnotation]
				if !found {
					return fmt.Errorf("federated service missing annotation: %s", k8s.RemoteDiscoveryLabel)
				}
				if remoteDiscovery != "web-svc@target" {
					return fmt.Errorf("federated service remote discovery was %s, expected %s", remoteDiscovery, "web-svc@target")
				}
				localDiscovery, found := svc.Annotations[k8s.LocalDiscoveryAnnotation]
				if !found {
					return fmt.Errorf("federated service missing annotation: %s", k8s.LocalDiscoveryAnnotation)
				}
				if localDiscovery != "web-svc" {
					return fmt.Errorf("federated service local discovery was %s, expected %s", localDiscovery, "web-svc")
				}

				// Metadata should be copied from the source cluster's service
				// because that Link is older.
				testAnnotation, found := svc.Annotations["test-context"]
				if !found {
					return errors.New("federated service missing annotation: test-context")
				}
				if testAnnotation != contexts[testutil.SourceContextKey] {
					return fmt.Errorf("federated service test-context was %s, expected %s", testAnnotation, contexts[testutil.SourceContextKey])
				}
				testLabel, found := svc.Labels["test-context"]
				if !found {
					return errors.New("federated service missing label: test-context")
				}
				if testLabel != contexts[testutil.SourceContextKey] {
					return fmt.Errorf("federated service test-context was %s, expected %s", testLabel, contexts[testutil.SourceContextKey])
				}

				_, err = TestHelper.GetEndpoints(ctx, ns, "web-svc-federated")
				if err == nil {
					return errors.New("federated service should not have endpoints")
				}
				if !kerrors.IsNotFound(err) {
					return fmt.Errorf("failed to retrieve federated service endpoints: %w", err)
				}
				return nil
			})
			if err != nil {
				testutil.AnnotatedFatal(t, "timed-out verifying federated service", err)
			}
		})

		t.Run("Check if federated service has correct metadata", func(t *testing.T) {
			timeout := time.Minute
			err := testutil.RetryFor(timeout, func() error {
				CheckAnnotation(t, contexts[testutil.SourceContextKey], ns, "web-svc-federated", "evil", "")              // Should be excluded.
				CheckAnnotation(t, contexts[testutil.SourceContextKey], ns, "web-svc-federated", "evil\\.linkerd/a", "")  // Should be excluded.
				CheckAnnotation(t, contexts[testutil.SourceContextKey], ns, "web-svc-federated", "good", "yes")           // Should be included.
				CheckAnnotation(t, contexts[testutil.SourceContextKey], ns, "web-svc-federated", "good\\.linkerd/c", "d") // Should be included.

				CheckLabel(t, contexts[testutil.SourceContextKey], ns, "web-svc-federated", "evil", "")              // Should be excluded.
				CheckLabel(t, contexts[testutil.SourceContextKey], ns, "web-svc-federated", "evil\\.linkerd/a", "")  // Should be excluded.
				CheckLabel(t, contexts[testutil.SourceContextKey], ns, "web-svc-federated", "good", "yes")           // Should be included.
				CheckLabel(t, contexts[testutil.SourceContextKey], ns, "web-svc-federated", "good\\.linkerd/c", "d") // Should be included.
				return nil
			})
			if err != nil {
				testutil.AnnotatedFatalf(t, "incorrect service metadata", "incorrect service metadata: %s", err)
			}
		})

		for _, ctx := range contexts {
			err := testutil.RetryFor(timeout, func() error {
				out, err := TestHelper.KubectlWithContext("",
					ctx,
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
				return fmt.Errorf("web-svc logs in %s cluster do not include voting errors\n%s", ctx, out)
			})
			if err != nil {
				testutil.AnnotatedFatal(t, fmt.Sprintf("timed-out waiting for traffic in %s cluster (%s)", ctx, timeout), err)
			}
		}

		// We update the ports on both members services and assert that the ports
		// are copied from the older Link.
		t.Run("Update federated service ports", func(t *testing.T) {
			for _, ctx := range contexts {
				out, err := TestHelper.KubectlWithContext("",
					ctx,
					"--namespace", ns,
					"patch",
					"service",
					"web-svc",
					"--type", "merge",
					"--patch", fmt.Sprintf(`{"spec": {"ports": [{"name": "http-%s", "port": 80, "targetPort": 80}]}}`, ctx),
				)
				if err != nil {
					testutil.AnnotatedFatal(t, "failed to update ports", out)
				}
			}

			err := TestHelper.SwitchContext(contexts[testutil.SourceContextKey])
			if err != nil {
				testutil.AnnotatedFatal(t, "failed to switch contexts", err)
			}
			err = testutil.RetryFor(timeout, func() error {
				svc, err := TestHelper.GetService(ctx, ns, "web-svc-federated")
				if err != nil {
					return err
				}
				if svc.Spec.Ports[0].Name != "http-"+contexts[testutil.SourceContextKey] {
					return fmt.Errorf("federated service port name was %s, expected %s", svc.Spec.Ports[0].Name, "http-"+contexts[testutil.SourceContextKey])
				}
				return nil
			})
			if err != nil {
				testutil.AnnotatedFatal(t, "timed-out verifying federated service ports", err)
			}
		})
	})
}
