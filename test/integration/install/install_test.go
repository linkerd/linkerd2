package test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-test/deep"
	"github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/flags"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/pkg/tree"
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

	// skippedInboundPorts lists some ports to be marked as skipped, which will
	// be verified in test/integration/inject
	skippedInboundPorts  = "1234,5678"
	skippedOutboundPorts = "1234,5678"
	vizExtensionName     = "viz"
)

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// Tests are executed in serial in the order defined
// Later tests depend on the success of earlier tests

func TestVersionPreInstall(t *testing.T) {
	version := "unavailable"
	if TestHelper.UpgradeFromVersion() != "" {
		version = TestHelper.UpgradeFromVersion()
	}

	err := TestHelper.CheckVersion(version)
	if err != nil {
		testutil.AnnotatedFatalf(t, "Version command failed", "Version command failed\n%s", err.Error())
	}
}

func TestCheckPreInstall(t *testing.T) {
	if TestHelper.ExternalIssuer() {
		t.Skip("Skipping pre-install check for external issuer test")
	}

	if TestHelper.UpgradeFromVersion() != "" {
		t.Skip("Skipping pre-install check for upgrade test")
	}

	if err := TestHelper.TestCheckPre(); err != nil {
		t.Fatalf("'linkerd check --pre' command failed: %s", err)
	}
}

func TestUpgradeTestAppWorksBeforeUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		ctx := context.Background()
		// make sure app is running
		testAppNamespace := "upgrade-test"
		for _, deploy := range []string{"emoji", "voting", "web"} {
			if err := TestHelper.CheckPods(ctx, testAppNamespace, deploy, 1); err != nil {
				//nolint:errorlint
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
	} else {
		t.Skip("Skipping for non upgrade test")
	}
}

func TestRetrieveUidPreUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		var err error
		configMapUID, err = TestHelper.KubernetesHelper.GetConfigUID(context.Background(), TestHelper.GetLinkerdNamespace())
		if err != nil || configMapUID == "" {
			testutil.AnnotatedFatalf(t, "error retrieving linkerd-config's uid",
				"error retrieving linkerd-config's uid: %s", err)
		}
	}
}

func TestInstallOrUpgradeCli(t *testing.T) {
	if TestHelper.GetHelmReleaseName() != "" {
		return
	}

	var (
		cmd  = "install"
		args = []string{
			"--controller-log-level", "debug",
			"--set", fmt.Sprintf("proxy.image.version=%s", TestHelper.GetVersion()),
			"--skip-inbound-ports", skippedInboundPorts,
			"--set", "heartbeatSchedule=1 2 3 4 5",
		}
		vizCmd  = []string{"viz", "install"}
		vizArgs = []string{
			"--set", fmt.Sprintf("namespace=%s", TestHelper.GetVizNamespace()),
		}
	)

	if TestHelper.GetClusterDomain() != "cluster.local" {
		args = append(args, "--cluster-domain", TestHelper.GetClusterDomain())
		vizArgs = append(vizArgs, "--set", fmt.Sprintf("clusterDomain=%s", TestHelper.GetClusterDomain()))
	}

	if policy := TestHelper.DefaultInboundPolicy(); policy != "" {
		args = append(args, "--set", "proxy.defaultInboundPolicy="+policy)
	}

	if TestHelper.UpgradeFromVersion() != "" {

		cmd = "upgrade"
		// upgrade CRDs and then control-plane
		out, err := TestHelper.LinkerdRun(cmd, "--crds")
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd upgrade config' command failed", err)
		}

		// apply CRDs
		// Limit the pruning only to known resources
		// that we intend to be delete in this stage to prevent it
		// from deleting other resources that have the
		// label
		out, err = TestHelper.KubectlApplyWithArgs(out, []string{
			"--prune",
			"-l", "linkerd.io/control-plane-ns=linkerd",
			"--prune-allowlist", "rbac.authorization.k8s.io/v1/clusterrole",
			"--prune-allowlist", "rbac.authorization.k8s.io/v1/clusterrolebinding",
			"--prune-allowlist", "apiregistration.k8s.io/v1/apiservice",
		}...)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"kubectl apply command failed\n%s", out)
		}

		// prepare for upgrade of control-plane
		edge, err := regexp.Match(`(edge)-([0-9]+\.[0-9]+\.[0-9]+)`, []byte(TestHelper.UpgradeFromVersion()))
		if err != nil {
			testutil.AnnotatedFatal(t, "could not match regex", err)
		}

		if edge {
			args = append(args, []string{"--set", fmt.Sprintf("proxyInit.ignoreOutboundPorts=%s", strings.Replace(skippedOutboundPorts, ",", "\\,", 1))}...)
		} else {
			args = append(args, []string{"--skip-outbound-ports", skippedOutboundPorts}...)
		}
	} else {
		err := TestHelper.InstallGatewayAPI()
		if err != nil {
			testutil.AnnotatedFatal(t, "failed to install gateway-api", err)
		}

		// install CRDs first
		exec := append([]string{cmd}, append(args, "--crds")...)
		out, err := TestHelper.LinkerdRun(exec...)
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
		}
		out, err = TestHelper.KubectlApply(out, "")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"kubectl apply command failed\n%s", out)
		}
	}

	exec := append([]string{cmd}, args...)
	out, err := TestHelper.LinkerdRun(exec...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
	}

	// test `linkerd upgrade --from-manifests`
	if TestHelper.UpgradeFromVersion() != "" {
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
	}

	// Limit the pruning only to known resources
	// that we intend to be delete in this stage to prevent it
	// from deleting other resources that have the
	// label
	cmdOut, err := TestHelper.KubectlApplyWithArgs(out, []string{
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
	if TestHelper.UpgradeFromVersion() != "" {
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

// helmInstallFlags returns the flags required for the `helm install` command,
// both for the linkerd-control-plane and the linkerd-viz charts
func helmInstallFlags(root *tls.CA) ([]string, []string) {
	coreArgs := []string{
		"--set", "controllerLogLevel=debug",
		"--set", "linkerdVersion=" + TestHelper.UpgradeHelmFromVersion(),
		"--set", "proxy.image.version=" + TestHelper.UpgradeHelmFromVersion(),
		"--set", "identityTrustDomain=cluster.local",
		"--set", "identityTrustAnchorsPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.crtPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.keyPEM=" + root.Cred.EncodePrivateKeyPEM(),
		"--set", "identity.issuer.crtExpiry=" + root.Cred.Crt.Certificate.NotAfter.Format(time.RFC3339),
	}
	vizArgs := []string{
		"--namespace", TestHelper.GetVizNamespace(),
		"--create-namespace",
		"--set", "linkerdVersion=" + TestHelper.UpgradeHelmFromVersion(),
	}
	return coreArgs, vizArgs
}

// helmUpgradeFlags returns the flags required for the `helm upgrade` command,
// both for the linkerd-control-plane and the linkerd-viz charts
func helmUpgradeFlags(root *tls.CA) ([]string, []string) {
	skippedInboundPortsEscaped := strings.Replace(skippedInboundPorts, ",", "\\,", 1)
	coreArgs := []string{
		"--set", "controllerLogLevel=debug",
		"--set", "linkerdVersion=" + TestHelper.GetVersion(),
		// these ports will get verified in test/integration/inject
		"--set", "proxyInit.ignoreInboundPorts=" + skippedInboundPortsEscaped,
		"--set", "identityTrustAnchorsPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.crtPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.keyPEM=" + root.Cred.EncodePrivateKeyPEM(),
	}
	vizArgs := []string{
		"--namespace", TestHelper.GetVizNamespace(),
		"--set", "linkerdVersion=" + TestHelper.GetVersion(),
	}

	if override := os.Getenv(flags.EnvOverrideDockerRegistry); override != "" {
		coreArgs = append(coreArgs,
			"--set", "proxy.image.name="+cmd.RegistryOverride("cr.l5d.io/linkerd/proxy", override),
			"--set", "proxyInit.image.name="+cmd.RegistryOverride("cr.l5d.io/linkerd/proxy-init", override),
			"--set", "controllerImage="+cmd.RegistryOverride("cr.l5d.io/linkerd/controller", override),
			"--set", "debugContainer.image.name="+cmd.RegistryOverride("cr.l5d.io/linkerd/debug", override),
		)
		vizArgs = append(vizArgs,
			"--set", "metricsAPI.image.registry="+override,
			"--set", "tap.image.registry="+override,
			"--set", "tapInjector.image.registry="+override,
			"--set", "dashboard.image.registry="+override,
		)
	}

	return coreArgs, vizArgs
}

func TestInstallHelm(t *testing.T) {
	if TestHelper.GetHelmReleaseName() == "" {
		return
	}

	cn := fmt.Sprintf("identity.%s.cluster.local", TestHelper.GetLinkerdNamespace())
	var err error
	helmTLSCerts, err = tls.GenerateRootCAWithDefaults(cn)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to generate root certificate for identity",
			"failed to generate root certificate for identity: %s", err)
	}

	args, vizArgs := helmInstallFlags(helmTLSCerts)

	releaseName := TestHelper.GetHelmReleaseName() + "-crds"
	if stdout, stderr, err := TestHelper.HelmInstall("linkerd/linkerd-crds", releaseName, args...); err != nil {
		testutil.AnnotatedFatalf(t, "'helm install' command failed",
			"'helm install' command failed\n%s\n%s", stdout, stderr)
	}

	releaseName = TestHelper.GetHelmReleaseName() + "-control-plane"
	if stdout, stderr, err := TestHelper.HelmInstall("linkerd/linkerd-control-plane", releaseName, args...); err != nil {
		testutil.AnnotatedFatalf(t, "'helm install' command failed",
			"'helm install' command failed\n%s\n%s", stdout, stderr)
	}
	TestHelper.WaitRollout(t, testutil.LinkerdDeployReplicasEdge)

	releaseName = TestHelper.GetHelmReleaseName() + "-l5d-viz"
	if stdout, stderr, err := TestHelper.HelmCmdPlain("install", "linkerd/linkerd-viz", releaseName, vizArgs...); err != nil {
		testutil.AnnotatedFatalf(t, "'helm install' command failed",
			"'helm install' command failed\n%s\n%s", stdout, stderr)
	}

	TestHelper.WaitRollout(t, testutil.LinkerdVizDeployReplicas)
}

func TestControlPlaneResourcesPostInstall(t *testing.T) {
	expectedServices := linkerdSvcEdge
	expectedDeployments := testutil.LinkerdDeployReplicasEdge
	if !TestHelper.ExternalPrometheus() {
		vizServices := []testutil.Service{
			{Namespace: "linkerd-viz", Name: "web"},
			{Namespace: "linkerd-viz", Name: "tap"},
			{Namespace: "linkerd-viz", Name: "prometheus"},
		}
		expectedServices = append(expectedServices, vizServices...)
		expectedDeployments["prometheus"] = testutil.DeploySpec{Namespace: "linkerd-viz", Replicas: 1}
	}

	// Upgrade Case
	if TestHelper.UpgradeHelmFromVersion() != "" {
		expectedServices = linkerdSvcStable
		expectedDeployments = testutil.LinkerdDeployReplicasStable
	}
	testutil.TestResourcesPostInstall(TestHelper.GetLinkerdNamespace(), expectedServices, expectedDeployments, TestHelper, t)
}

func TestUpgradeHelm(t *testing.T) {
	if TestHelper.UpgradeHelmFromVersion() == "" {
		t.Skip("Skipping as this is not a helm upgrade test")
	}

	args := []string{
		// implicit as at least one value is set manually: "--reset-values",
		// (see https://medium.com/@kcatstack/understand-helm-upgrade-flags-reset-values-reuse-values-6e58ac8f127e )

		// Also ensure that the CPU requests are fairly small (<100m) in order
		// to avoid squeeze-out of other pods in CI tests.

		"--set", "proxy.resources.cpu.limit=200m",
		"--set", "proxy.resources.cpu.request=20m",
		"--set", "proxy.resources.memory.limit=200Mi",
		"--set", "proxy.resources.memory.request=100Mi",
		// actually sets the value for the controller pod
		"--set", "destinationProxyResources.cpu.limit=1020m",
		"--set", "destinationProxyResources.memory.request=102Mi",
		"--set", "identityProxyResources.cpu.limit=1040m",
		"--set", "identityProxyResources.memory.request=104Mi",
		"--set", "proxyInjectorProxyResources.cpu.limit=1060m",
		"--set", "proxyInjectorProxyResources.memory.request=106Mi",
		"--atomic",
		"--timeout", "60m",
		"--wait",
	}
	extraArgs, vizArgs := helmUpgradeFlags(helmTLSCerts)
	args = append(args, extraArgs...)
	releaseName := TestHelper.GetHelmReleaseName() + "-crds"
	if stdout, stderr, err := TestHelper.HelmUpgrade(TestHelper.GetHelmCharts()+"/linkerd-crds", releaseName, args...); err != nil {
		testutil.AnnotatedFatalf(t, "'helm upgrade' command failed",
			"'helm upgrade' command failed\n%s\n%s", stdout, stderr)
	}

	releaseName = TestHelper.GetHelmReleaseName() + "-control-plane"
	if stdout, stderr, err := TestHelper.HelmUpgrade(TestHelper.GetHelmCharts()+"/linkerd-control-plane", releaseName, args...); err != nil {
		TestHelper.WaitRollout(t, testutil.LinkerdDeployReplicasEdge)
		testutil.AnnotatedFatalf(t, "'helm upgrade' command failed",
			"'helm upgrade' command failed\n%s\n%s", stdout, stderr)
	}
	TestHelper.WaitRollout(t, testutil.LinkerdVizDeployReplicas)

	vizChart := TestHelper.GetLinkerdVizHelmChart()
	releaseName = TestHelper.GetHelmReleaseName() + "-l5d-viz"
	if stdout, stderr, err := TestHelper.HelmCmdPlain("upgrade", vizChart, releaseName, vizArgs...); err != nil {
		testutil.AnnotatedFatalf(t, "'helm upgrade' command failed",
			"'helm upgrade' command failed\n%s\n%s", stdout, stderr)
	}

	TestHelper.WaitRollout(t, testutil.LinkerdVizDeployReplicas)

	TestHelper.AddInstalledExtension(vizExtensionName)
}

func TestRetrieveUidPostUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
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
}

func TestOverridesSecret(t *testing.T) {

	if TestHelper.GetHelmReleaseName() != "" {
		t.Skip("Skipping as this is a helm test where linkerd-config-overrides is absent")
	}

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
	}

	// Check for fields that were added during upgrade
	if TestHelper.UpgradeFromVersion() != "" {
		testCases = append(testCases, []struct {
			path  []string
			value string
		}{
			{
				[]string{"proxyInit", "ignoreOutboundPorts"},
				skippedOutboundPorts,
			},
		}...)
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

	t.Run("Check if any unknown fields sneaked in", func(t *testing.T) {
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
			"proxy": tree.Tree{
				"image": tree.Tree{
					"version": TestHelper.GetVersion(),
				},
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
					"name":    reg + "/proxy",
					"version": TestHelper.GetVersion(),
				},
			}
			knownKeys["proxyInit"].(tree.Tree)["image"] = tree.Tree{
				"name": reg + "/proxy-init",
			}
		}

		// Check for fields that were added during upgrade
		if TestHelper.UpgradeFromVersion() != "" {
			knownKeys["proxyInit"].(tree.Tree)["ignoreOutboundPorts"] = skippedOutboundPorts
		}

		if TestHelper.GetClusterDomain() != "cluster.local" {
			knownKeys["clusterDomain"] = TestHelper.GetClusterDomain()
		}

		if TestHelper.ExternalIssuer() {
			knownKeys["identity"].(tree.Tree)["issuer"].(tree.Tree)["issuanceLifetime"] = "15s"
			knownKeys["identity"].(tree.Tree)["issuer"].(tree.Tree)["scheme"] = "kubernetes.io/tls"
		} else {
			knownKeys["identity"].(tree.Tree)["issuer"].(tree.Tree)["tls"] = tree.Tree{
				"crtPEM": extractValue(t, "identity", "issuer", "tls", "crtPEM"),
				"keyPEM": extractValue(t, "identity", "issuer", "tls", "keyPEM"),
			}
		}

		if TestHelper.CNI() {
			knownKeys["cniEnabled"] = true
		}

		if policy := TestHelper.DefaultInboundPolicy(); policy != "" {
			knownKeys["proxy"].(tree.Tree)["defaultInboundPolicy"] = policy
		}

		// Check if the keys in overridesTree match with knownKeys
		if diff := deep.Equal(overridesTree.String(), knownKeys.String()); diff != nil {
			testutil.AnnotatedFatalf(t, "Overrides and knownKeys are different", "%+v", diff)
		}
	})
}

type expectedData struct {
	pod        string
	cpuLimit   string
	cpuRequest string
	memLimit   string
	memRequest string
}

var expectedResources = []expectedData{
	{
		pod:        "linkerd-destination",
		cpuLimit:   "1020m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "102Mi",
	},
	{
		pod:        "linkerd-identity",
		cpuLimit:   "1040m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "104Mi",
	},
	{
		pod:        "linkerd-proxy-injector",
		cpuLimit:   "1060m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "106Mi",
	},
}

func TestComponentProxyResources(t *testing.T) {
	if TestHelper.UpgradeHelmFromVersion() == "" {
		t.Skip("Skipping as this is not a helm upgrade test")
	}

	for _, expected := range expectedResources {
		resourceReqs, err := TestHelper.GetResources(context.Background(), "linkerd-proxy", expected.pod, TestHelper.GetLinkerdNamespace())
		if err != nil {
			testutil.AnnotatedFatalf(t, "setting proxy resources failed", "Error retrieving resource requirements for %s: %s", expected.pod, err)
		}

		cpuLimitStr := resourceReqs.Limits.Cpu().String()
		if cpuLimitStr != expected.cpuLimit {
			testutil.AnnotatedFatalf(t, "setting proxy resources failed", "unexpected %s CPU limit: expected %s, was %s", expected.pod, expected.cpuLimit, cpuLimitStr)
		}
		cpuRequestStr := resourceReqs.Requests.Cpu().String()
		if cpuRequestStr != expected.cpuRequest {
			testutil.AnnotatedFatalf(t, "setting proxy resources failed", "unexpected %s CPU request: expected %s, was %s", expected.pod, expected.cpuRequest, cpuRequestStr)
		}
		memLimitStr := resourceReqs.Limits.Memory().String()
		if memLimitStr != expected.memLimit {
			testutil.AnnotatedFatalf(t, "setting proxy resources failed", "unexpected %s memory limit: expected %s, was %s", expected.pod, expected.memLimit, memLimitStr)
		}
		memRequestStr := resourceReqs.Requests.Memory().String()
		if memRequestStr != expected.memRequest {
			testutil.AnnotatedFatalf(t, "setting proxy resources failed", "unexpected %s memory request: expected %s, was %s", expected.pod, expected.memRequest, memRequestStr)
		}
	}
}

func TestVersionPostInstall(t *testing.T) {
	err := TestHelper.CheckVersion(TestHelper.GetVersion())
	if err != nil {
		testutil.AnnotatedFatalf(t, "Version command failed",
			"Version command failed\n%s", err.Error())
	}
}

func TestCheckPostInstall(t *testing.T) {
	if err := TestHelper.TestCheckProxy(TestHelper.GetVersion(), TestHelper.GetLinkerdNamespace()); err != nil {
		t.Fatalf("'linkerd check --proxy' command failed: %s", err)
	}
}

func TestUpgradeTestAppWorksAfterUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		testAppNamespace := "upgrade-test"
		if err := testutil.ExerciseTestAppEndpoint("/api/vote?choice=:policeman:", testAppNamespace, TestHelper); err != nil {
			testutil.AnnotatedFatalf(t, "error exercising test app endpoint after upgrade",
				"error exercising test app endpoint after upgrade %s", err)
		}
	} else {
		t.Skip("Skipping for non upgrade test")
	}
}

func TestRestarts(t *testing.T) {
	expectedDeployments := testutil.LinkerdDeployReplicasEdge
	if !TestHelper.ExternalPrometheus() {
		expectedDeployments["prometheus"] = testutil.DeploySpec{Namespace: "linkerd-viz", Replicas: 1}
	}
	for deploy, spec := range expectedDeployments {
		if err := TestHelper.CheckPods(context.Background(), spec.Namespace, deploy, spec.Replicas); err != nil {
			//nolint:errorlint
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedFatal(t, "CheckPods timed-out", err)
			}
		}
	}
}
