package test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
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
	os.Exit(testutil.Run(m, TestHelper))
}

var (
	configMapUID string

	helmTLSCerts *tls.CA

	linkerdSvcs = []string{
		"linkerd-controller-api",
		"linkerd-dst",
		"linkerd-grafana",
		"linkerd-identity",
		"linkerd-prometheus",
		"linkerd-web",
		"linkerd-tap",
	}

	multiclusterSvcs = []string{
		"linkerd-gateway",
	}

	injectionCases = []struct {
		ns          string
		annotations map[string]string
		injectArgs  []string
	}{
		{
			ns: "smoke-test",
			annotations: map[string]string{
				k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
			},
			injectArgs: nil,
		},
		{
			ns:         "smoke-test-manual",
			injectArgs: []string{"--manual"},
		},
		{
			ns:         "smoke-test-ann",
			injectArgs: []string{},
		},
	}

	//skippedInboundPorts lists some ports to be marked as skipped, which will
	// be verified in test/integration/inject
	skippedInboundPorts = "1234,5678"
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

	cmd := []string{"check", "--pre", "--expected-version", TestHelper.GetVersion()}
	golden := "check.pre.golden"
	out, stderr, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd check' command failed", "'linkerd check' command failed\n%s\n%s", out, stderr)
	}

	err = TestHelper.ValidateOutput(out, golden)
	if err != nil {
		testutil.AnnotatedFatalf(t, "received unexpected output", "received unexpected output\n%s", err.Error())
	}
}

func TestUpgradeTestAppWorksBeforeUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		// make sure app is running
		testAppNamespace := "upgrade-test"
		for _, deploy := range []string{"emoji", "voting", "web"} {
			if err := TestHelper.CheckPods(testAppNamespace, deploy, 1); err != nil {
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedError(t, "CheckPods timed-out", err)
				}
			}

			if err := TestHelper.CheckDeployment(testAppNamespace, deploy, 1); err != nil {
				testutil.AnnotatedErrorf(t, "CheckDeployment timed-out", "Error validating deployment [%s]:\n%s", deploy, err)
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
		configMapUID, err = TestHelper.KubernetesHelper.GetConfigUID(TestHelper.GetLinkerdNamespace())
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
			"--proxy-log-level", "warn,linkerd2_proxy=debug",
			"--proxy-version", TestHelper.GetVersion(),
			"--skip-inbound-ports", skippedInboundPorts,
		}
	)

	if TestHelper.GetClusterDomain() != "cluster.local" {
		args = append(args, "--cluster-domain", TestHelper.GetClusterDomain())
	}

	if TestHelper.ExternalIssuer() {

		// short cert lifetime to put some pressure on the CSR request, response code path
		args = append(args, "--identity-issuance-lifetime=15s", "--identity-external-issuer=true")

		err := TestHelper.CreateControlPlaneNamespaceIfNotExists(TestHelper.GetLinkerdNamespace())
		if err != nil {
			testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", TestHelper.GetLinkerdNamespace()),
				"failed to create %s namespace: %s", TestHelper.GetLinkerdNamespace(), err)
		}

		identity := fmt.Sprintf("identity.%s.%s", TestHelper.GetLinkerdNamespace(), TestHelper.GetClusterDomain())

		root, err := tls.GenerateRootCAWithDefaults(identity)
		if err != nil {
			testutil.AnnotatedFatal(t, "error generating root CA", err)
		}

		// instead of passing the roots and key around we generate
		// two secrets here. The second one will be used in the
		// external_issuer_test to update the first one and trigger
		// cert rotation in the identity service. That allows us
		// to generated the certs on the fly and use custom domain.

		if err = TestHelper.CreateTLSSecret(
			k8s.IdentityIssuerSecretName,
			root.Cred.Crt.EncodeCertificatePEM(),
			root.Cred.Crt.EncodeCertificatePEM(),
			root.Cred.EncodePrivateKeyPEM()); err != nil {
			testutil.AnnotatedFatal(t, "error creating TLS secret", err)
		}

		crt2, err := root.GenerateCA(identity, -1)
		if err != nil {
			testutil.AnnotatedFatal(t, "error generating CA", err)
		}

		if err = TestHelper.CreateTLSSecret(
			k8s.IdentityIssuerSecretName+"-new",
			root.Cred.Crt.EncodeCertificatePEM(),
			crt2.Cred.EncodeCertificatePEM(),
			crt2.Cred.EncodePrivateKeyPEM()); err != nil {
			testutil.AnnotatedFatal(t, "error creating TLS secret (-new)", err)
		}
	}

	if TestHelper.UpgradeFromVersion() != "" {

		cmd = "upgrade"
		// test 2-stage install during upgrade
		out, stderr, err := TestHelper.LinkerdRun(cmd, "config")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd upgrade config' command failed",
				"'linkerd upgrade config' command failed\n%s\n%s", out, stderr)
		}

		// apply stage 1
		out, err = TestHelper.KubectlApply(out, "")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"kubectl apply command failed\n%s", out)
		}

		// prepare for stage 2
		args = append([]string{"control-plane"}, args...)
	}

	exec := append([]string{cmd}, args...)
	out, stderr, err := TestHelper.LinkerdRun(exec...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd install' command failed",
			"'linkerd install' command failed: \n%s\n%s", out, stderr)
	}

	// test `linkerd upgrade --from-manifests`
	if TestHelper.UpgradeFromVersion() != "" {
		resources := []string{"configmaps/" + k8s.ConfigConfigMapName, "configmaps/" + k8s.AddOnsConfigMapName, "secrets/" + k8s.IdentityIssuerSecretName}
		kubeArgs := append([]string{"--namespace", TestHelper.GetLinkerdNamespace(), "get"}, resources...)
		kubeArgs = append(kubeArgs, "-oyaml")

		manifests, err := TestHelper.Kubectl("", kubeArgs...)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl get' command failed",
				"'kubectl get' command failed with %s\n%s\n%s", err, manifests, kubeArgs)
		}

		exec = append(exec, "--from-manifests", "-")
		upgradeFromManifests, stderr, err := TestHelper.PipeToLinkerdRun(manifests, exec...)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd upgrade --from-manifests' command failed",
				"'linkerd upgrade --from-manifests' command failed with %s\n%s\n%s\n%s", err, stderr, upgradeFromManifests, manifests)
		}

		if out != upgradeFromManifests {
			// retry in case it's just a discrepancy in the heartbeat cron schedule
			exec := append([]string{cmd}, args...)
			out, stderr, err := TestHelper.LinkerdRun(exec...)
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

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

}

// These need to be updated (if there are changes) once a new stable is released
func helmOverridesStable(root *tls.CA) []string {
	return []string{
		"--set", "global.controllerLogLevel=debug",
		"--set", "global.linkerdVersion=" + TestHelper.UpgradeHelmFromVersion(),
		"--set", "global.proxy.image.version=" + TestHelper.UpgradeHelmFromVersion(),
		"--set", "global.identityTrustDomain=cluster.local",
		"--set", "global.identityTrustAnchorsPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.crtPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.keyPEM=" + root.Cred.EncodePrivateKeyPEM(),
		"--set", "identity.issuer.crtExpiry=" + root.Cred.Crt.Certificate.NotAfter.Format(time.RFC3339),
	}
}

// These need to correspond to the flags in the current edge
func helmOverridesEdge(root *tls.CA) []string {
	skippedInboundPortsEscaped := strings.Replace(skippedInboundPorts, ",", "\\,", 1)
	return []string{
		"--set", "global.controllerLogLevel=debug",
		"--set", "global.linkerdVersion=" + TestHelper.GetVersion(),
		"--set", "global.proxy.image.version=" + TestHelper.GetVersion(),
		// these ports will get verified in test/integration/inject
		"--set", "global.proxyInit.ignoreInboundPorts=" + skippedInboundPortsEscaped,
		"--set", "global.identityTrustDomain=cluster.local",
		"--set", "global.identityTrustAnchorsPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.crtPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.keyPEM=" + root.Cred.EncodePrivateKeyPEM(),
		"--set", "identity.issuer.crtExpiry=" + root.Cred.Crt.Certificate.NotAfter.Format(time.RFC3339),
		"--set", "grafana.image.version=" + TestHelper.GetVersion(),
	}
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

	var chartToInstall string
	var args []string

	if TestHelper.UpgradeHelmFromVersion() != "" {
		chartToInstall = TestHelper.GetHelmStableChart()
		args = helmOverridesStable(helmTLSCerts)
	} else {
		chartToInstall = TestHelper.GetHelmChart()
		args = helmOverridesEdge(helmTLSCerts)
	}

	if stdout, stderr, err := TestHelper.HelmInstall(chartToInstall, args...); err != nil {
		testutil.AnnotatedFatalf(t, "'helm install' command failed",
			"'helm install' command failed\n%s\n%s", stdout, stderr)
	}
}

func TestControlPlaneResourcesPostInstall(t *testing.T) {
	testutil.TestResourcesPostInstall(TestHelper.GetLinkerdNamespace(), linkerdSvcs, testutil.LinkerdDeployReplicas, TestHelper, t)
}

func TestInstallMulticluster(t *testing.T) {
	if !TestHelper.Multicluster() {
		return
	}
	if TestHelper.GetMulticlusterHelmReleaseName() != "" {
		flags := []string{
			"--set", "linkerdVersion=" + TestHelper.GetVersion(),
			"--set", "controllerImageVersion=" + TestHelper.GetVersion(),
		}
		if stdout, stderr, err := TestHelper.HelmInstallMulticluster(TestHelper.GetMulticlusterHelmChart(), flags...); err != nil {
			testutil.AnnotatedFatalf(t, "'helm install' command failed",
				"'helm install' command failed\n%s\n%s", stdout, stderr)
		}
	} else {
		exec := append([]string{"multicluster"}, []string{
			"install",
			"--namespace", TestHelper.GetMulticlusterNamespace(),
		}...)
		out, stderr, err := TestHelper.LinkerdRun(exec...)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd multicluster install' command failed",
				"'linkerd multicluster' command failed: \n%s\n%s", out, stderr)
		}

		out, err = TestHelper.KubectlApply(out, "")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}
	}
}

func TestMulticlusterResourcesPostInstall(t *testing.T) {
	if !TestHelper.Multicluster() {
		return
	}
	testutil.TestResourcesPostInstall(TestHelper.GetMulticlusterNamespace(), multiclusterSvcs, testutil.MulticlusterDeployReplicas, TestHelper, t)
}

func TestCheckHelmStableBeforeUpgrade(t *testing.T) {
	if TestHelper.UpgradeHelmFromVersion() == "" {
		t.Skip("Skipping as this is not a helm upgrade test")
	}

	// TODO: make checkOutput as true once 2.9 releases
	testCheckCommand(t, "", TestHelper.UpgradeHelmFromVersion(), "", TestHelper.UpgradeHelmFromVersion(), false)
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

		"--set", "global.proxy.resources.cpu.limit=200m",
		"--set", "global.proxy.resources.cpu.request=20m",
		"--set", "global.proxy.resources.memory.limit=200Mi",
		"--set", "global.proxy.resources.memory.request=100Mi",
		// actually sets the value for the controller pod
		"--set", "publicAPIProxyResources.cpu.limit=1010m",
		"--set", "publicAPIProxyResources.memory.request=101Mi",
		"--set", "destinationProxyResources.cpu.limit=1020m",
		"--set", "destinationProxyResources.memory.request=102Mi",
		"--set", "grafana.proxy.resources.cpu.limit=1030m",
		"--set", "grafana.proxy.resources.memory.request=103Mi",
		"--set", "identityProxyResources.cpu.limit=1040m",
		"--set", "identityProxyResources.memory.request=104Mi",
		"--set", "prometheus.proxy.resources.cpu.limit=1050m",
		"--set", "prometheus.proxy.resources.memory.request=105Mi",
		"--set", "proxyInjectorProxyResources.cpu.limit=1060m",
		"--set", "proxyInjectorProxyResources.memory.request=106Mi",
		"--set", "smiMetricsProxyResources.cpu.limit=1070m",
		"--set", "smiMetricsProxyResources.memory.request=107Mi",
		"--set", "spValidatorProxyResources.cpu.limit=1080m",
		"--set", "spValidatorProxyResources.memory.request=108Mi",
		"--set", "tapProxyResources.cpu.limit=1090m",
		"--set", "tapProxyResources.memory.request=109Mi",
		"--set", "webProxyResources.cpu.limit=1100m",
		"--set", "webProxyResources.memory.request=110Mi",
		"--atomic",
		"--wait",
	}
	args = append(args, helmOverridesEdge(helmTLSCerts)...)
	if stdout, stderr, err := TestHelper.HelmUpgrade(TestHelper.GetHelmChart(), args...); err != nil {
		testutil.AnnotatedFatalf(t, "'helm upgrade' command failed",
			"'helm upgrade' command failed\n%s\n%s", stdout, stderr)
	}
}

func TestRetrieveUidPostUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		newConfigMapUID, err := TestHelper.KubernetesHelper.GetConfigUID(TestHelper.GetLinkerdNamespace())
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

type expectedData struct {
	pod        string
	cpuLimit   string
	cpuRequest string
	memLimit   string
	memRequest string
}

var expectedResources = []expectedData{
	{
		pod:        "linkerd-controller",
		cpuLimit:   "1010m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "101Mi",
	},
	{
		pod:        "linkerd-destination",
		cpuLimit:   "1020m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "102Mi",
	},
	{
		pod:        "linkerd-grafana",
		cpuLimit:   "1030m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "103Mi",
	},
	{
		pod:        "linkerd-identity",
		cpuLimit:   "1040m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "104Mi",
	},
	{
		pod:        "linkerd-prometheus",
		cpuLimit:   "1050m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "105Mi",
	},
	{
		pod:        "linkerd-proxy-injector",
		cpuLimit:   "1060m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "106Mi",
	},
	/*	 not used in default case
	{
		pod:        "linkerd-smi-metrics",
		cpuLimit:   "1070m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "1007i",
	},
	*/
	{
		pod:        "linkerd-sp-validator",
		cpuLimit:   "1080m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "108Mi",
	},
	{
		pod:        "linkerd-tap",
		cpuLimit:   "1090m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "109Mi",
	},
	{
		pod:        "linkerd-web",
		cpuLimit:   "1100m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "110Mi",
	},
}

func TestComponentProxyResources(t *testing.T) {
	if TestHelper.UpgradeHelmFromVersion() == "" {
		t.Skip("Skipping as this is not a helm upgrade test")
	}

	for _, expected := range expectedResources {
		resourceReqs, err := TestHelper.GetResources("linkerd-proxy", expected.pod, TestHelper.GetLinkerdNamespace())
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

func testCheckCommand(t *testing.T, stage string, expectedVersion string, namespace string, cliVersionOverride string, compareOutput bool) {
	var cmd []string
	var golden string
	if stage == "proxy" {
		cmd = []string{"check", "--proxy", "--expected-version", expectedVersion, "--namespace", namespace, "--wait=0"}
		if TestHelper.Multicluster() {
			golden = "check.multicluster.proxy.golden"
		} else {
			golden = "check.proxy.golden"
		}
	} else if stage == "config" {
		cmd = []string{"check", "config", "--expected-version", expectedVersion, "--wait=0"}
		golden = "check.config.golden"
	} else {
		cmd = []string{"check", "--expected-version", expectedVersion, "--wait=0"}
		if TestHelper.Multicluster() {
			golden = "check.multicluster.golden"
		} else {
			golden = "check.golden"
		}
	}

	timeout := time.Minute
	err := TestHelper.RetryFor(timeout, func() error {
		if cliVersionOverride != "" {
			cliVOverride := []string{"--cli-version-override", cliVersionOverride}
			cmd = append(cmd, cliVOverride...)
		}
		out, stderr, err := TestHelper.LinkerdRun(cmd...)

		if err != nil {
			return fmt.Errorf("'linkerd check' command failed\n%s\n%s", stderr, out)
		}

		if !compareOutput {
			return nil
		}

		err = TestHelper.ValidateOutput(out, golden)
		if err != nil {
			return fmt.Errorf("received unexpected output\n%s", err.Error())
		}

		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd check' command timed-out (%s)", timeout), err)
	}
}

// TODO: run this after a `linkerd install config`
func TestCheckConfigPostInstall(t *testing.T) {
	testCheckCommand(t, "config", TestHelper.GetVersion(), "", "", true)
}

func TestCheckPostInstall(t *testing.T) {
	testCheckCommand(t, "", TestHelper.GetVersion(), "", "", true)
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

func TestInstallSP(t *testing.T) {
	cmd := []string{"install-sp"}

	out, stderr, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'linkerd install-sp' command failed",
			"'linkerd install-sp' command failed\n%s\n%s", out, stderr)
	}

	out, err = TestHelper.KubectlApply(out, TestHelper.GetLinkerdNamespace())
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}
}

func TestDashboard(t *testing.T) {
	dashboardPort := 52237
	dashboardURL := fmt.Sprintf("http://localhost:%d", dashboardPort)

	outputStream, err := TestHelper.LinkerdRunStream("dashboard", "-p",
		strconv.Itoa(dashboardPort), "--show", "url")
	if err != nil {
		testutil.AnnotatedFatalf(t, "error running command",
			"error running command:\n%s", err)
	}
	defer outputStream.Stop()

	outputLines, err := outputStream.ReadUntil(4, 1*time.Minute)
	if err != nil {
		testutil.AnnotatedFatalf(t, "error running command",
			"error running command:\n%s", err)
	}

	output := strings.Join(outputLines, "")
	if !strings.Contains(output, dashboardURL) {
		testutil.AnnotatedFatalf(t,
			"dashboard command failed. Expected url [%s] not present", dashboardURL)
	}

	resp, err := TestHelper.HTTPGetURL(dashboardURL + "/api/version")
	if err != nil {
		testutil.AnnotatedFatalf(t, "unexpected error",
			"unexpected error: %v", err)
	}

	if !strings.Contains(resp, TestHelper.GetVersion()) {
		testutil.AnnotatedFatalf(t, "dashboard command failed; response doesn't contain expected version",
			"dashboard command failed. Expected response [%s] to contain version [%s]",
			resp, TestHelper.GetVersion())
	}
}

func TestInject(t *testing.T) {
	resources, err := testutil.ReadFile("testdata/smoke_test.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read smoke test file",
			"failed to read smoke test file: %s", err)
	}

	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			var out string

			prefixedNs := TestHelper.GetTestNamespace(tc.ns)

			err := TestHelper.CreateDataPlaneNamespaceIfNotExists(prefixedNs, tc.annotations)
			if err != nil {
				testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", prefixedNs),
					"failed to create %s namespace: %s", prefixedNs, err)
			}

			if tc.injectArgs != nil {
				cmd := []string{"inject"}
				cmd = append(cmd, tc.injectArgs...)
				cmd = append(cmd, "testdata/smoke_test.yaml")

				var injectReport string
				out, injectReport, err = TestHelper.LinkerdRun(cmd...)
				if err != nil {
					testutil.AnnotatedFatalf(t, "'linkerd inject' command failed",
						"'linkerd inject' command failed: %s\n%s", err, out)
				}

				err = TestHelper.ValidateOutput(injectReport, "inject.report.golden")
				if err != nil {
					testutil.AnnotatedFatalf(t, "received unexpected output",
						"received unexpected output\n%s", err.Error())
				}
			} else {
				out = resources
			}

			out, err = TestHelper.KubectlApply(out, prefixedNs)
			if err != nil {
				testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
					"'kubectl apply' command failed\n%s", out)
			}

			for _, deploy := range []string{"smoke-test-terminus", "smoke-test-gateway"} {
				if err := TestHelper.CheckPods(prefixedNs, deploy, 1); err != nil {
					if rce, ok := err.(*testutil.RestartCountError); ok {
						testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
					} else {
						testutil.AnnotatedFatal(t, "CheckPods timed-out", err)
					}
				}
			}

			url, err := TestHelper.URLFor(prefixedNs, "smoke-test-gateway", 8080)
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to get URL",
					"failed to get URL: %s", err)
			}

			output, err := TestHelper.HTTPGetURL(url)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error",
					"unexpected error: %v %s", err, output)
			}

			expectedStringInPayload := "\"payload\":\"BANANA\""
			if !strings.Contains(output, expectedStringInPayload) {
				testutil.AnnotatedFatalf(t, "application response doesn't contain the expected response",
					"expected application response to contain string [%s], but it was [%s]",
					expectedStringInPayload, output)
			}
		})
	}
}

func TestServiceProfileDeploy(t *testing.T) {
	bbProto, err := TestHelper.HTTPGetURL("https://raw.githubusercontent.com/BuoyantIO/bb/v0.0.5/api.proto")
	if err != nil {
		testutil.AnnotatedFatalf(t, "unexpected error",
			"unexpected error: %v %s", err, bbProto)
	}

	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			prefixedNs := TestHelper.GetTestNamespace(tc.ns)

			cmd := []string{"profile", "-n", prefixedNs, "--proto", "-", "smoke-test-terminus-svc"}
			bbSP, stderr, err := TestHelper.PipeToLinkerdRun(bbProto, cmd...)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error",
					"unexpected error: %v %s", err, stderr)
			}

			out, err := TestHelper.KubectlApply(bbSP, prefixedNs)
			if err != nil {
				testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
					"'kubectl apply' command failed: %s\n%s", err, out)
			}
		})
	}
}

func TestCheckProxy(t *testing.T) {
	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			prefixedNs := TestHelper.GetTestNamespace(tc.ns)
			testCheckCommand(t, "proxy", TestHelper.GetVersion(), prefixedNs, "", true)
		})
	}
}

func TestLogs(t *testing.T) {
	okMessages, errs := testutil.FetchAndCheckLogs(TestHelper)
	for msg := range okMessages {
		t.Log(msg)
	}
	for err := range errs {
		testutil.AnnotatedError(t, "Error checking logs", err)
	}
}

func TestEvents(t *testing.T) {
	for _, err := range testutil.FetchAndCheckEvents(TestHelper) {
		testutil.AnnotatedError(t, "Error checking events", err)
	}
}

func TestRestarts(t *testing.T) {
	for deploy, spec := range testutil.LinkerdDeployReplicas {
		if err := TestHelper.CheckPods(TestHelper.GetLinkerdNamespace(), deploy, spec.Replicas); err != nil {
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedFatal(t, "CheckPods timed-out", err)
			}
		}
	}
}

//TODO Put that in test_cleanup when we have adequate resource labels
func TestUninstallMulticluster(t *testing.T) {
	if !TestHelper.Multicluster() {
		return
	}

	if TestHelper.GetMulticlusterHelmReleaseName() != "" {
		if stdout, stderr, err := TestHelper.HelmUninstallMulticluster(TestHelper.GetMulticlusterHelmChart()); err != nil {
			testutil.AnnotatedFatalf(t, "'helm delete' command failed",
				"'helm delete' command failed\n%s\n%s", stdout, stderr)
		}
	} else {
		exec := append([]string{"multicluster"}, []string{
			"install",
			"--namespace", TestHelper.GetMulticlusterNamespace(),
		}...)
		out, stderr, err := TestHelper.LinkerdRun(exec...)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd multicluster install' command failed",
				"'linkerd multicluster' command failed: \n%s\n%s", out, stderr)
		}

		out, err = TestHelper.Kubectl(out, []string{"delete", "-f", "-"}...)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl delete' command failed",
				"'kubectl apply' command failed\n%s", out)
		}
	}
}
