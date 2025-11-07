package edgeupgradetest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/go-test/deep"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tree"
	"github.com/linkerd/linkerd2/testutil"
)

var (
	TestHelper *testutil.TestHelper

	configMapUID string

	linkerdSvcEdge = []testutil.Service{
		{Namespace: "linkerd", Name: "linkerd-dst"},
		{Namespace: "linkerd", Name: "linkerd-identity"},

		{Namespace: "linkerd", Name: "linkerd-dst-headless"},
		{Namespace: "linkerd", Name: "linkerd-identity-headless"},
	}

	// skippedInboundPorts lists some ports to be marked as skipped, which will
	// be verified in test/integration/inject
	skippedInboundPorts    = "1234,5678"
	skippedOutboundPorts   = "1234,5678"
	linkerdBaseEdgeVersion string
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

func TestInstallResourcesPreUpgrade(t *testing.T) {
	versions, err := TestHelper.GetReleaseChannelVersions()
	if err != nil {
		testutil.AnnotatedFatal(t, "failed to get the latest release channels versions", err)
	}
	linkerdBaseEdgeVersion = versions["edge"]

	tmpDir, err := os.MkdirTemp("", "upgrade-cli")
	if err != nil {
		testutil.AnnotatedFatal(t, "failed to create temp dir", err)
	}
	defer os.RemoveAll(tmpDir)

	cliPath := fmt.Sprintf("%s/linkerd2-cli-%s-%s-%s", tmpDir, linkerdBaseEdgeVersion, runtime.GOOS, runtime.GOARCH)
	if err := TestHelper.DownloadCLIBinary(cliPath, linkerdBaseEdgeVersion); err != nil {
		testutil.AnnotatedFatal(t, "failed to fetch cli executable", err)
	}

	// Nest all pre-upgrade tests here so they can install and check resources
	// using the latest edge CLI
	t.Run(fmt.Sprintf("installing Linkerd %s control plane", linkerdBaseEdgeVersion), func(t *testing.T) {
		err := TestHelper.InstallGatewayAPI()
		if err != nil {
			testutil.AnnotatedFatal(t, "failed to install gateway-api", err)
		}

		out, err := TestHelper.CmdRun(cliPath, "install", "--crds")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd install --crds' command failed", "'linkerd install --crds' command failed:\n%v", err)
		}

		out, err = TestHelper.KubectlApply(out, "")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}

		waitArgs := []string{
			"wait", "--for", "condition=established", "--timeout=60s", "crd",
			"authorizationpolicies.policy.linkerd.io",
			"meshtlsauthentications.policy.linkerd.io",
			"networkauthentications.policy.linkerd.io",
			"servers.policy.linkerd.io",
			"serverauthorizations.policy.linkerd.io",
		}
		if _, err := TestHelper.Kubectl("", waitArgs...); err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl wait crd' command failed", "'kubectl wait crd' command failed:\n%v", err)
		}

		out, err = TestHelper.CmdRun(cliPath,
			"install",
			"--controller-log-level=debug",
			"--set=proxyInit.ignoreInboundPorts=1234\\,5678",
		)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd install' command failed", "'linkerd install' command failed:\n%v", err)
		}

		out, err = TestHelper.KubectlApply(out, "")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}

		TestHelper.WaitRollout(t, testutil.LinkerdDeployReplicasEdge)
	})

	// TestInstallViz will install the viz extension to be used by the rest of the
	// tests in the viz suite
	t.Run(fmt.Sprintf("installing Linkerd %s viz extension", linkerdBaseEdgeVersion), func(t *testing.T) {
		out, err := TestHelper.CmdRun(cliPath, "viz", "install", "--set", fmt.Sprintf("namespace=%s", TestHelper.GetVizNamespace()))
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd viz install' command failed", err)
		}

		out, err = TestHelper.KubectlApplyWithArgs(out)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}

		TestHelper.WaitRollout(t, testutil.LinkerdVizDeployReplicas)
		TestHelper.AddInstalledExtension("viz")

	})

	// Check client and server versions are what we expect them to be
	t.Run(fmt.Sprintf("check version is %s pre-upgrade", linkerdBaseEdgeVersion), func(t *testing.T) {
		out, err := TestHelper.CmdRun(cliPath, "version")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd version' command failed", "'linkerd version' command failed\n%s", err.Error())
		}

		if !strings.Contains(out, fmt.Sprintf("Client version: %s", linkerdBaseEdgeVersion)) {
			testutil.AnnotatedFatalf(t, "'linkerd version' command failed", "'linkerd version' command failed\nexpected client version: %s, got: %s", linkerdBaseEdgeVersion, out)
		}
		if !strings.Contains(out, fmt.Sprintf("Server version: %s", linkerdBaseEdgeVersion)) {
			testutil.AnnotatedFatalf(t, "'linkerd version' command failed", "'linkerd version' command failed\nexpected server version: %s, got: %s", linkerdBaseEdgeVersion, out)
		}
	})
}

func TestUpgradeTestAppWorksBeforeUpgrade(t *testing.T) {
	ctx := context.Background()
	testAppNamespace := "upgrade-test"

	// create namespace, and install test app
	if err := TestHelper.CreateDataPlaneNamespaceIfNotExists(ctx, testAppNamespace, map[string]string{k8s.ProxyInjectAnnotation: "enabled"}); err != nil {
		testutil.AnnotatedFatalf(t, "failed to create namespace", "failed to create namespace %s: %s", testAppNamespace, err)
	}

	if _, err := TestHelper.Kubectl("", "apply", "-f", "./testdata/emoji.yaml", "-n", testAppNamespace); err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' failed", "'kubectl apply' failed: %s", err)
	}

	// make sure app is running
	for _, deploy := range []string{"emoji", "voting", "web"} {
		if err := TestHelper.CheckPods(ctx, testAppNamespace, deploy, 1); err != nil {
			var rce *testutil.RestartCountError
			if errors.As(err, &rce) {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out", err)
			}
		}
	}

	if err := testutil.ExerciseTestAppEndpoint("/api/list", testAppNamespace, TestHelper); err != nil {
		testutil.AnnotatedFatalf(t, "error exercising test app endpoint before upgrade",
			"error exercising test app endpoint before upgrade %s", err)
	}
}

func TestRetrieveUidPreUpgrade(t *testing.T) {
	var err error
	configMapUID, err = TestHelper.KubernetesHelper.GetConfigUID(context.Background(), TestHelper.GetLinkerdNamespace())
	if err != nil || configMapUID == "" {
		testutil.AnnotatedFatalf(t, "error retrieving linkerd-config's uid",
			"error retrieving linkerd-config's uid: %s", err)
	}
}

func TestUpgradeCli(t *testing.T) {
	cmd := "upgrade"
	args := []string{
		"--controller-log-level", "debug",
		"--set", "proxyInit.ignoreInboundPorts=1234\\,5678",
		"--set", "heartbeatSchedule=1 2 3 4 5",
		"--set", "proxyInit.ignoreOutboundPorts=1234\\,5678",
	}

	// Upgrade CRDs.
	exec := append([]string{cmd}, append(args, "--crds")...)
	out, err := TestHelper.LinkerdRun(exec...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd upgrade --crds' command failed", err)
	}
	cmdOut, err := TestHelper.KubectlApply(out, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", cmdOut)
	}

	// Upgrade control plane.
	exec = append([]string{cmd}, args...)
	out, err = TestHelper.LinkerdRun(exec...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd upgrade' command failed", err)
	}

	// Limit the pruning only to known resources
	// that we intend to be delete in this stage to prevent it
	// from deleting other resources that have the
	// label
	cmdOut, err = TestHelper.KubectlApplyWithArgs(out, []string{
		"--prune",
		"-l", "linkerd.io/control-plane-ns=linkerd",
		"--prune-allowlist", "apps/v1/deployment",
		"--prune-allowlist", "core/v1/service",
		"--prune-allowlist", "core/v1/configmap",
	}...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", cmdOut)
	}

	TestHelper.WaitRollout(t, testutil.LinkerdDeployReplicasEdge)

	// It is necessary to clone LinkerdVizDeployReplicas so that we do not
	// mutate its original value.
	expectedDeployments := make(map[string]testutil.DeploySpec)
	for k, v := range testutil.LinkerdVizDeployReplicas {
		expectedDeployments[k] = v
	}

	// Install Linkerd Viz Extension
	vizCmd := []string{
		"viz",
		"install",
		"--set", fmt.Sprintf("namespace=%s", TestHelper.GetVizNamespace()),
	}
	out, err = TestHelper.LinkerdRun(vizCmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd viz install' command failed", err)
	}

	out, err = TestHelper.KubectlApplyWithArgs(out, []string{
		"--prune",
		"-l", "linkerd.io/extension=viz",
	}...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	TestHelper.WaitRollout(t, expectedDeployments)

}

func TestControlPlaneResourcesPostInstall(t *testing.T) {
	expectedDeployments := testutil.LinkerdDeployReplicasEdge
	expectedServices := linkerdSvcEdge
	vizServices := []testutil.Service{
		{Namespace: "linkerd-viz", Name: "web"},
		{Namespace: "linkerd-viz", Name: "tap"},
		{Namespace: "linkerd-viz", Name: "prometheus"},
	}
	expectedServices = append(expectedServices, vizServices...)
	expectedDeployments["prometheus"] = testutil.DeploySpec{Namespace: "linkerd-viz", Replicas: 1}

	testutil.TestResourcesPostInstall(TestHelper.GetLinkerdNamespace(), expectedServices, expectedDeployments, TestHelper, t)
}

func TestRetrieveUidPostUpgrade(t *testing.T) {
	newConfigMapUID, err := TestHelper.KubernetesHelper.GetConfigUID(context.Background(), TestHelper.GetLinkerdNamespace())
	if err != nil || newConfigMapUID == "" {
		testutil.AnnotatedFatalf(t, "error retrieving linkerd-config's uid",
			"error retrieving linkerd-config's uid: %s", err)
	}
	if configMapUID != newConfigMapUID {
		testutil.AnnotatedFatalf(t, "linkerd-config's uid after upgrade doesn't match its value before the upgrade",
			"linkerd-config's uid after upgrade [%s] doesn't match its value before the upgrade [%s]",
			newConfigMapUID, configMapUID,
		)
	}
}

func TestOverridesSecret(t *testing.T) {
	configOverridesSecret, err := TestHelper.KubernetesHelper.GetSecret(context.Background(), TestHelper.GetLinkerdNamespace(), "linkerd-config-overrides")
	if err != nil {
		testutil.AnnotatedFatalf(t, "could not retrieve linkerd-config-overrides",
			"could not retrieve linkerd-config-overrides\n%s", err)
	}

	overrides := configOverridesSecret.Data["linkerd-config-overrides"]
	overridesTree, err := tree.BytesToTree(overrides)
	if err != nil {
		testutil.AnnotatedFatalf(t, "could not retrieve linkerd-config-overrides",
			"could not retrieve linkerd-config-overrides\n%s", err)
	}

	// Check for fields that were added during install
	testCases := []struct {
		path  []string
		value string
	}{
		{
			[]string{"controllerLogLevel"},
			"debug",
		},
		{
			[]string{"proxyInit", "ignoreInboundPorts"},
			skippedInboundPorts,
		},
		{
			[]string{"proxyInit", "ignoreOutboundPorts"},
			skippedOutboundPorts,
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%s: %s", strings.Join(tc.path, "/"), tc.value), func(t *testing.T) {
			finalValue, err := overridesTree.GetString(tc.path...)
			if err != nil {
				testutil.AnnotatedFatalf(t, "could not perform tree.GetString",
					"could not perform tree.GetString\n%s", err)
			}

			if tc.value != finalValue {
				testutil.AnnotatedFatalf(t, fmt.Sprintf("Values at path %s do not match", strings.Join(tc.path, "/")),
					"Expected value at [%s] to be [%s] but received [%s]",
					strings.Join(tc.path, "/"), tc.value, finalValue)
			}
		})
	}

	extractValue := func(t *testing.T, path ...string) string {
		val, err := overridesTree.GetString(path...)
		if err != nil {
			testutil.AnnotatedFatalf(t, "error calling overridesTree.GetString()",
				"error calling overridesTree.GetString(): %s", err)
			return ""

		}
		return val
	}

	t.Run("Check if any unknown fields snuck in", func(t *testing.T) {
		knownKeys := tree.Tree{
			"controllerLogLevel": "debug",
			"heartbeatSchedule":  "1 2 3 4 5",
			"identity": tree.Tree{
				"issuer": tree.Tree{
					"tls": tree.Tree{
						"crtPEM": extractValue(t, "identity", "issuer", "tls", "crtPEM"),
						"keyPEM": extractValue(t, "identity", "issuer", "tls", "keyPEM"),
					},
				},
			},
			"identityTrustAnchorsPEM": extractValue(t, "identityTrustAnchorsPEM"),
			"proxyInit": tree.Tree{
				"ignoreInboundPorts":  skippedInboundPorts,
				"ignoreOutboundPorts": skippedOutboundPorts,
			},
		}

		if reg := os.Getenv(flags.EnvOverrideDockerRegistry); reg != "" {
			knownKeys["controllerImage"] = reg + "/controller"
			knownKeys["debugContainer"] = tree.Tree{
				"image": tree.Tree{
					"name": reg + "/debug",
				},
			}
			knownKeys["proxy"] = tree.Tree{
				"image": tree.Tree{
					"name": reg + "/proxy",
				},
			}
		}

		// Check if the keys in overridesTree match with knownKeys
		if diff := deep.Equal(overridesTree.String(), knownKeys.String()); diff != nil {
			testutil.AnnotatedFatalf(t, "Overrides and knownKeys are different", "%+v", diff)
		}
	})
}

func TestVersionPostInstall(t *testing.T) {
	err := TestHelper.CheckVersion(TestHelper.GetVersion())
	if err != nil {
		testutil.AnnotatedFatalf(t, "Version command failed",
			"Version command failed\n%s", err.Error())
	}
}

func TestCheckProxyPostUpgrade(t *testing.T) {
	if err := TestHelper.TestCheckProxy(TestHelper.GetVersion(), TestHelper.GetLinkerdNamespace()); err != nil {
		t.Fatalf("'linkerd check --proxy' command failed: %s", err)
	}
}

func TestUpgradeTestAppWorksAfterUpgrade(t *testing.T) {
	testAppNamespace := "upgrade-test"

	// Restart pods after upgrade to make sure they're re-injected with the
	// latest proxy
	if _, err := TestHelper.Kubectl("", "rollout", "restart", "deploy", "-n", testAppNamespace); err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl rollout' failed", "'kubectl rollout' failed: %s", err)
	}

	// make sure app is running before proceeding
	ctx := context.Background()
	for _, deploy := range []string{"emoji", "voting", "web"} {
		if err := TestHelper.CheckPods(ctx, testAppNamespace, deploy, 1); err != nil {
			var rce *testutil.RestartCountError
			if errors.As(err, &rce) {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedError(t, "CheckPods timed-out", err)
			}
		}
	}

	if err := testutil.ExerciseTestAppEndpoint("/api/vote?choice=:policeman:", testAppNamespace, TestHelper); err != nil {
		testutil.AnnotatedFatalf(t, "error exercising test app endpoint after upgrade",
			"error exercising test app endpoint after upgrade %s", err)
	}
}
