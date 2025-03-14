package multiclustertraffic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	mcHealthcheck "github.com/linkerd/linkerd2/multicluster/cmd"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/testutil"
	"github.com/linkerd/linkerd2/testutil/prommatch"
)

var (
	TestHelper *testutil.TestHelper
	targetCtx  string
	sourceCtx  string
	contexts   map[string]string
)

var (
	nginxTargetLabels = prommatch.Labels{
		"direction":            prommatch.Equals("outbound"),
		"tls":                  prommatch.Equals("true"),
		"server_id":            prommatch.Equals("default.linkerd-multicluster-statefulset.serviceaccount.identity.linkerd.cluster.local"),
		"dst_control_plane_ns": prommatch.Equals("linkerd"),
		"dst_namespace":        prommatch.Equals("linkerd-multicluster-statefulset"),
		"dst_pod":              prommatch.Equals("nginx-statefulset-0"),
		"dst_serviceaccount":   prommatch.Equals("default"),
		"dst_statefulset":      prommatch.Equals("nginx-statefulset"),
	}

	tcpConnMatcher = prommatch.NewMatcher("tcp_open_total",
		prommatch.Labels{
			"peer": prommatch.Equals("dst"),
		},
		prommatch.TargetAddrLabels(),
		nginxTargetLabels,
		prommatch.HasPositiveValue(),
	)
	httpReqMatcher = prommatch.NewMatcher("request_total",
		prommatch.TargetAddrLabels(),
		nginxTargetLabels,
		prommatch.HasPositiveValue(),
	)
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Before starting, initialize contexts
	contexts = TestHelper.GetMulticlusterContexts()
	sourceCtx = contexts[testutil.SourceContextKey]
	targetCtx = contexts[testutil.TargetContextKey]
	// Then, re-build clientset with source cluster context instead of context
	// inferred from environment.
	if err := TestHelper.SwitchContext(sourceCtx); err != nil {
		out := fmt.Sprintf("Error running test: failed to switch Kubernetes client to context [%s]: %s\n", sourceCtx, err)
		os.Stderr.Write([]byte(out))
		os.Exit(1)
	}
	// Block until gateway & service mirror deploys are running successfully in
	// source cluster.
	if TestHelper.GetMulticlusterManageControllers() {
		TestHelper.WaitUntilDeployReady(map[string]testutil.DeploySpec{
			"controller-target": {Namespace: "linkerd-multicluster", Replicas: 1},
		})
	} else {
		TestHelper.WaitUntilDeployReady(map[string]testutil.DeploySpec{
			"linkerd-service-mirror-target": {Namespace: "linkerd-multicluster", Replicas: 1},
		})
	}
	os.Exit(m.Run())
}

// TestGateways tests the `linkerd multicluster gateways` command by installing
// three emojivoto services in target cluster and asserting the output against
// the source cluster.
func TestGateways(t *testing.T) {
	t.Run("install resources in target cluster", func(t *testing.T) {
		// Create namespace in source cluster
		out, err := TestHelper.KubectlWithContext("", contexts[testutil.SourceContextKey], "create", "namespace", "linkerd-nginx-gateway-deploy")
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to create namespace", "failed to create namespace 'linkerd-nginx-gateway-deploy': %s\n%s", err, out)
		}

		out, err = TestHelper.KubectlApplyWithContext("", contexts[testutil.TargetContextKey], "-n", "linkerd-nginx-gateway-deploy", "-f", "testdata/nginx-gateway-deploy.yaml")
		if err != nil {
			testutil.AnnotatedFatalf(t, "failed to install nginx deploy", "failed to install nginx deploy: %s\n%s", err, out)
		}

		// Wait for workloads to spin up in target cluster. These workloads will have
		// their services mirrored in the source cluster. The test will check whether
		// the gateway keeps track of the mirror services.
		tgtWorkloadRollouts := map[string]testutil.DeploySpec{
			"nginx-deploy": {Namespace: "linkerd-nginx-gateway-deploy", Replicas: 1},
		}
		TestHelper.WaitRolloutWithContext(t, tgtWorkloadRollouts, contexts[testutil.TargetContextKey])
	})

	timeout := time.Minute
	err := testutil.RetryFor(timeout, func() error {
		out, err := TestHelper.LinkerdRun("--context="+contexts[testutil.SourceContextKey], "multicluster", "gateways")
		if err != nil {
			return err
		}
		rows := strings.Split(out, "\n")
		if len(rows) < 2 {
			return errors.New("response is empty")
		}
		fields := strings.Fields(rows[1])
		if len(fields) < 4 {
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

// TestCheckGatewayAfterRepairEndpoints calls `linkerd mc check` again after 1 minute,
// so that the RepairEndpoints event has already been processed, making sure
// that resyncing didn't break things.
func TestCheckGatewayAfterRepairEndpoints(t *testing.T) {
	// Re-build the clientset with the source context
	if err := TestHelper.SwitchContext(contexts[testutil.SourceContextKey]); err != nil {
		testutil.AnnotatedFatalf(t,
			"failed to rebuild helper clientset with new context",
			"failed to rebuild helper clientset with new context [%s]: %v",
			contexts[testutil.SourceContextKey], err)
	}
	time.Sleep(time.Minute + 5*time.Second)
	err := TestHelper.TestCheckWith([]healthcheck.CategoryID{mcHealthcheck.LinkerdMulticlusterExtensionCheck}, "--context", contexts[testutil.SourceContextKey])
	if err != nil {
		t.Fatalf("'linkerd check' command failed: %s", err)
	}
}

// TestTargetTraffic inspects the target cluster's web-svc pod to see if the
// source cluster's vote-bot has been able to hit it with requests. If it has
// successfully issued requests, then we'll see log messages.
//
// TODO it may be clearer to invoke `linkerd diagnostics proxy-metrics` to check whether we see
// connections from the gateway pod to the web-svc?
func TestTargetTraffic(t *testing.T) {
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
	TestHelper.WithDataPlaneNamespace(ctx, "emojivoto", annotations, t, func(t *testing.T, ns string) {
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
				out, err = TestHelper.KubectlWithContext("", contexts[testutil.TargetContextKey], "--namespace", ns, "label", "service/web-svc", "mirror.linkerd.io/exported=true")
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
			testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd multicluster gateways' command timed-out (%s)", timeout), err)
		}
	})
}

// TestMulticlusterStatefulSetTargetTraffic will test that a statefulset can be
// mirrored from a target cluster to a source cluster. The test deploys two
// workloads: a slow cooker (as a client) in the src, and an nginx statefulset in
// (as a server) in the tgt. The slow-cooker is configured to send traffic to an
// nginx endpoint mirror (nginx-statefulset-0). The traffic should be received
// by the nginx pod in the tgt. To assert this, we get proxy metrics from the
// gateway to make sure our connections from the source cluster were routed
// correctly.
func TestMulticlusterStatefulSetTargetTraffic(t *testing.T) {
	if err := TestHelper.SwitchContext(contexts[testutil.TargetContextKey]); err != nil {
		testutil.AnnotatedFatalf(t, "failed to rebuild helper clientset with new context", "failed to rebuild helper clientset with new context [%s]: %v", contexts[testutil.TargetContextKey], err)
	}

	ctx := context.Background()
	// Create 'multicluster-statefulset' namespace in target cluster, to be deleted at the end of the test.
	TestHelper.WithDataPlaneNamespace(ctx, "multicluster-statefulset", map[string]string{}, t, func(t *testing.T, ns string) {
		t.Run("Deploy resources in source and target clusters", func(t *testing.T) {
			// Create slow-cooker client in source cluster
			out, err := TestHelper.KubectlApplyWithContext("", contexts[testutil.SourceContextKey], "-f", "testdata/slow-cooker.yml")
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to install slow-cooker", "failed to install slow-cooker: %s\ngot: %s", err, out)
			}

			// Create statefulset deployment in target cluster
			out, err = TestHelper.KubectlApplyWithContext("", contexts[testutil.TargetContextKey], "-f", "testdata/nginx-ss.yml")
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to install nginx-ss", "failed to install nginx-ss: %s\n%s", err, out)
			}
		})

		t.Run("Wait until workloads are ready", func(t *testing.T) {
			// Wait until client is up and running in source cluster
			scDeployReplica := map[string]testutil.DeploySpec{"slow-cooker": {Namespace: ns, Replicas: 1}}
			TestHelper.WaitRolloutWithContext(t, scDeployReplica, contexts[testutil.SourceContextKey])

			// Wait until "target" statefulset is up and running.
			nginxSpec := testutil.DeploySpec{Namespace: ns, Replicas: 1}
			o, err := TestHelper.KubectlWithContext("", contexts[testutil.TargetContextKey], "--namespace="+nginxSpec.Namespace, "rollout", "status", "--timeout=60m", "statefulset/nginx-statefulset")
			if err != nil {
				oEvt, _ := TestHelper.KubectlWithContext("", contexts[testutil.TargetContextKey], "--namespace="+nginxSpec.Namespace, "get", "event", "--field-selector", "involvedObject.name=nginx-statefulset")
				testutil.AnnotatedFatalf(t,
					fmt.Sprintf("failed to wait rollout of deploy/%s", "nginx-statefulset"),
					"failed to wait for rollout of deploy/%s: %s: %s\nEvents:\n%s", "nginx-statefulset", err, o, oEvt)
			}
		})

		_, err := TestHelper.KubectlWithContext("", contexts[testutil.TargetContextKey], "--namespace="+ns, "label", "svc", "nginx-statefulset-svc", k8s.DefaultExportedServiceSelector+"=true")
		if err != nil {
			testutil.AnnotatedFatal(t, "failed to label nginx-statefulset-svc service", err)
		}

		dgCmd := []string{"--context=" + targetCtx, "diagnostics", "proxy-metrics", "--namespace",
			"linkerd-multicluster", "deploy/linkerd-gateway"}
		t.Run("expect open outbound TCP connection from gateway to nginx", func(t *testing.T) {
			// Use a short time window so that slow-cooker can warm-up and send
			// requests.
			err := testutil.RetryFor(1*time.Minute, func() error {
				// Check gateway metrics
				metrics, err := TestHelper.LinkerdRun(dgCmd...)
				if err != nil {
					return fmt.Errorf("failed to get metrics for gateway deployment: %w", err)
				}

				s := prommatch.Suite{}.
					MustContain("TCP connection from gateway to nginx", tcpConnMatcher).
					MustContain("HTTP requests from gateway to nginx", httpReqMatcher)

				if err := s.CheckString(metrics); err != nil {
					return fmt.Errorf("invalid metrics for gateway deployment: %w", err)
				}

				return nil
			})

			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error", "unexpected error: %v", err)
			}
		})
	})
}

func TestSourceResourcesAreCleaned(t *testing.T) {
	if err := TestHelper.SwitchContext(contexts[testutil.SourceContextKey]); err != nil {
		testutil.AnnotatedFatalf(t, "failed to rebuild helper clientset with new context", "failed to rebuild helper clientset with new context [%s]: %v", contexts[testutil.SourceContextKey], err)
	}

	ctx := context.Background()
	if err := TestHelper.DeleteNamespaceIfExists(ctx, "linkerd-multicluster-statefulset"); err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to delete %s namespace", "linkerd-multicluster-statefulset"),
			"failed to delete %s namespace: %s", "linkerd-multicluster-statefulset", err)
	}

	if err := TestHelper.DeleteNamespaceIfExists(ctx, "linkerd-emojivoto"); err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to delete %s namespace", "linkerd-emojivoto"),
			"failed to delete %s namespace: %s", "linkerd-emojivoto", err)
	}

	if err := TestHelper.DeleteNamespaceIfExists(ctx, "linkerd-nginx-gateway-deploy"); err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to delete %s namespace", "linkerd-nginx-gateway-deploy"),
			"failed to delete %s namespace: %s", "linkerd-nginx-gateway-deploy", err)
	}
}

// At the end of the test, we have one resource left to clean 'linkerd-nginx-gateway-deploy',
// so we just switch the context again and delete its corresponding namespace.
func TestTargetResourcesAreCleaned(t *testing.T) {
	if err := TestHelper.SwitchContext(contexts[testutil.TargetContextKey]); err != nil {
		testutil.AnnotatedFatalf(t, "failed to rebuild helper clientset with new context", "failed to rebuild helper clientset with new context [%s]: %v", contexts[testutil.TargetContextKey], err)
	}

	ctx := context.Background()
	if err := TestHelper.DeleteNamespaceIfExists(ctx, "linkerd-nginx-gateway-deploy"); err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to delete %s namespace", "linkerd-nginx-gateway-deploy"),
			"failed to delete %s namespace: %s", "linkerd-nginx-gateway-deploy", err)
	}
}
