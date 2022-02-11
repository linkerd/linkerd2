package upgradetest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/tree"
	"github.com/linkerd/linkerd2/pkg/version"
	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var (
	TestHelper *testutil.TestHelper
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

var (
	configMapUID string

	helmTLSCerts *tls.CA

	linkerdSvcEdge = []testutil.Service{
		{Namespace: "linkerd", Name: "linkerd-dst"},
		{Namespace: "linkerd", Name: "linkerd-identity"},

		{Namespace: "linkerd", Name: "linkerd-dst-headless"},
		{Namespace: "linkerd", Name: "linkerd-identity-headless"},
	}

	// Override in case edge starts to deviate from stable service-wise
	linkerdSvcStable = linkerdSvcEdge

	//skippedInboundPorts lists some ports to be marked as skipped, which will
	// be verified in test/integration/inject
	skippedInboundPorts       = "1234,5678"
	skippedOutboundPorts      = "1234,5678"
	multiclusterExtensionName = "multicluster"
	vizExtensionName          = "viz"
)

//////////////////////
/// TEST EXECUTION ///
//////////////////////
func TestInstallLinkerd(t *testing.T) {
	cmd := []string{
		"install",
		"--controller-log-level", "debug",
		"--set", "heartbeatSchedule=1 2 3 4 5",
		"--set", "proxyInit.ignoreInboundPorts=\"1234\\,5678\"",
		"--set", fmt.Sprintf("linkerdVersion=%s", TestHelper.GetVersion()),
	}

	// Pipe cmd & args to `linkerd`
	out, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
	}

	out, err = TestHelper.KubectlApplyWithArgs(out)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	TestHelper.WaitRollout(t, testutil.LinkerdDeployReplicasEdge)
}

// TestInstallViz will install the viz extension to be used by the rest of the
// tests in the viz suite
func TestInstallViz(t *testing.T) {
	cmd := []string{
		"viz",
		"install",
		"--set", fmt.Sprintf("namespace=%s", TestHelper.GetVizNamespace()),
	}

	out, err := TestHelper.LinkerdRun(cmd...)
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
}

func TestVersionPreInstall(t *testing.T) {
	version := TestHelper.UpgradeFromVersion()
	err := TestHelper.CheckVersion(version)
	if err != nil {
		testutil.AnnotatedFatalf(t, "Version command failed", "Version command failed\n%s", err.Error())
	}
}

func TestUpgradeTestAppWorksBeforeUpgrade(t *testing.T) {
	ctx := context.Background()
	testAppNamespace := "upgrade-test"

	// create namespace and install test app
	TestHelper.CreateDataPlaneNamespaceIfNotExists(ctx, testAppNamespace, map[string]string{})
	TestHelper.Kubectl("-f", "./testdata/emoji.yaml", "-n", testAppNamespace)

	// make sure app is running
	for _, deploy := range []string{"emoji", "voting", "web"} {
		if err := TestHelper.CheckPods(ctx, testAppNamespace, deploy, 1); err != nil {
			if rce, ok := err.(*testutil.RestartCountError); ok {
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
		"--proxy-version", TestHelper.GetVersion(),
		"--skip-inbound-ports", skippedInboundPorts,
		"--set", "heartbeatSchedule=1 2 3 4 5",
	}
	vizCmd := []string{"viz", "install"}
	vizArgs := []string{
		"--set", fmt.Sprintf("namespace=%s", TestHelper.GetVizNamespace()),
	}

	// test 2-stage install during upgrade
	out, err := TestHelper.LinkerdRun(cmd, "config")
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd upgrade config' command failed", err)
	}

	// apply stage 1
	// Limit the pruning only to known resources
	// that we intend to be delete in this stage to prevent it
	// from deleting other resources that have the
	// label
	out, err = TestHelper.KubectlApplyWithArgs(out, []string{
		"--prune",
		"-l", "linkerd.io/control-plane-ns=linkerd",
		"--prune-whitelist", "rbac.authorization.k8s.io/v1/clusterrole",
		"--prune-whitelist", "rbac.authorization.k8s.io/v1/clusterrolebinding",
		"--prune-whitelist", "apiregistration.k8s.io/v1/apiservice",
	}...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"kubectl apply command failed\n%s", out)
	}

	// prepare for stage 2
	args = append([]string{"control-plane"}, args...)
	if err != nil {
		testutil.AnnotatedFatal(t, "could not match regex", err)
	}

	args = append(args, []string{"--set", fmt.Sprintf("proxyInit.ignoreOutboundPorts=%s", strings.Replace(skippedOutboundPorts, ",", "\\,", 1))}...)
	exec := append([]string{cmd}, args...)
	out, err = TestHelper.LinkerdRun(exec...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
	}

	// test `linkerd upgrade --from-manifests`
	kubeArgs := append([]string{"--namespace", TestHelper.GetLinkerdNamespace(), "get"}, "configmaps", "-oyaml")
	configManifests, err := TestHelper.Kubectl("", kubeArgs...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl get' command failed",
			"'kubectl get' command failed with %s\n%s\n%s", err, configManifests, kubeArgs)
	}

	kubeArgs = append([]string{"--namespace", TestHelper.GetLinkerdNamespace(), "get"}, "secrets", "-oyaml")
	secretManifests, err := TestHelper.Kubectl("", kubeArgs...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl get' command failed",
			"'kubectl get' command failed with %s\n%s\n%s", err, secretManifests, kubeArgs)
	}

	manifests := configManifests + "---\n" + secretManifests

	exec = append(exec, "--from-manifests", "-")
	upgradeFromManifests, stderr, err := TestHelper.PipeToLinkerdRun(manifests, exec...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd upgrade --from-manifests' command failed",
			"'linkerd upgrade --from-manifests' command failed with %s\n%s\n%s\n%s", err, stderr, upgradeFromManifests, manifests)
	}

	if out != upgradeFromManifests {
		// retry in case it's just a discrepancy in the heartbeat cron schedule
		exec := append([]string{cmd}, args...)
		out, err := TestHelper.LinkerdRun(exec...)
		if err != nil {
			testutil.AnnotatedFatalf(t, fmt.Sprintf("command failed: %v", exec),
				"command failed: %v\n%s\n%s", exec, out, stderr)
		}

		if out != upgradeFromManifests {
			testutil.AnnotatedFatalf(t, "manifest upgrade differs from k8s upgrade",
				"manifest upgrade differs from k8s upgrade.\nk8s upgrade:\n%s\nmanifest upgrade:\n%s", out, upgradeFromManifests)
		}
	}

	// Limit the pruning only to known resources
	// that we intend to be delete in this stage to prevent it
	// from deleting other resources that have the
	// label
	cmdOut, err := TestHelper.KubectlApplyWithArgs(out, []string{
		"--prune",
		"-l", "linkerd.io/control-plane-ns=linkerd",
		"--prune-whitelist", "apps/v1/deployment",
		"--prune-whitelist", "core/v1/service",
		"--prune-whitelist", "core/v1/configmap",
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
	exec = append(vizCmd, vizArgs...)
	out, err = TestHelper.LinkerdRun(exec...)
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

	// Install Linkerd Viz Extension
	exec = append(vizCmd, vizArgs...)
	out, err = TestHelper.LinkerdRun(exec...)
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
	expectedDeployments := testutil.LinkerdDeployReplicasStable
	expectedServices := linkerdSvcStable
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
				"issuer": tree.Tree{},
			},
			"identityTrustAnchorsPEM": extractValue(t, "identityTrustAnchorsPEM"),
			"proxyInit": tree.Tree{
				"ignoreInboundPorts": skippedInboundPorts,
			},
		}

		if reg := os.Getenv(flags.EnvOverrideDockerRegistry); reg != "" {
			knownKeys["controllerImage"] = reg + "/controller"
			knownKeys["debugContainer"] = tree.Tree{
				"image": tree.Tree{
					"name": reg + "/debug",
				},
			}
			knownKeys["policyController"] = tree.Tree{
				"image": tree.Tree{
					"name": reg + "/policy-controller",
				},
			}
			knownKeys["proxy"] = tree.Tree{
				"image": tree.Tree{
					"name": reg + "/proxy",
				},
			}
			knownKeys["proxyInit"].(tree.Tree)["image"] = tree.Tree{
				"name": reg + "/proxy-init",
			}

			knownKeys["proxyInit"].(tree.Tree)["ignoreOutboundPorts"] = skippedOutboundPorts
		}

		// Check if the keys in overridesTree match with knownKeys
		if !reflect.DeepEqual(overridesTree.String(), knownKeys.String()) {
			testutil.AnnotatedFatalf(t, "Overrides and knownKeys are different",
				"Expected overrides to be [%s] but found [%s]",
				knownKeys.String(), overridesTree.String(),
			)
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

func TestCheckCommand(t *testing.T, stage, expectedVersion, namespace, cliVersionOverride string) {
	var cmd []string
	var golden string
	cmd = []string{"check", "--proxy", "--expected-version", expectedVersion, "--namespace", namespace, "--wait=60m"}
	golden = "check.proxy.golden"

	expected := getCheckOutput(t, golden, TestHelper.GetLinkerdNamespace())
	timeout := time.Minute * 5
	err := TestHelper.RetryFor(timeout, func() error {
		if cliVersionOverride != "" {
			cliVOverride := []string{"--cli-version-override", cliVersionOverride}
			cmd = append(cmd, cliVOverride...)
		}
		out, err := TestHelper.LinkerdRun(cmd...)

		if err != nil {
			return fmt.Errorf("'linkerd check' command failed\n%s\n%s", err, out)
		}

		if !strings.Contains(out, expected) {
			return fmt.Errorf(
				"Expected:\n%s\nActual:\n%s", expected, out)
		}

		for _, ext := range TestHelper.GetInstalledExtensions() {
			if ext == vizExtensionName {
				expected = getCheckOutput(t, "check.viz.proxy.golden", TestHelper.GetVizNamespace())
				if !strings.Contains(out, expected) {
					return fmt.Errorf(
						"Expected:\n%s\nActual:\n%s", expected, out)
				}
			}
		}

		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd check' command timed-out (%s)", timeout), err)
	}
}

func getCheckOutput(t *testing.T, goldenFile string, namespace string) string {
	pods, err := TestHelper.KubernetesHelper.GetPods(context.Background(), namespace, nil)
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("failed to retrieve pods: %s", err), err)
	}

	proxyVersionErr := ""
	err = healthcheck.CheckProxyVersionsUpToDate(pods, version.Channels{})
	if err != nil {
		proxyVersionErr = err.Error()
	}

	tpl := template.Must(template.ParseFiles("testdata" + "/" + goldenFile))
	vars := struct {
		ProxyVersionErr string
		HintURL         string
	}{
		proxyVersionErr,
		healthcheck.HintBaseURL(TestHelper.GetVersion()),
	}

	var expected bytes.Buffer
	if err := tpl.Execute(&expected, vars); err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("failed to parse check.viz.golden template: %s", err), err)
	}

	return expected.String()
}

func TestUpgradeTestAppWorksAfterUpgrade(t *testing.T) {
	testAppNamespace := "upgrade-test"
	if err := testutil.ExerciseTestAppEndpoint("/api/vote?choice=:policeman:", testAppNamespace, TestHelper); err != nil {
		testutil.AnnotatedFatalf(t, "error exercising test app endpoint after upgrade",
			"error exercising test app endpoint after upgrade %s", err)
	}
}
