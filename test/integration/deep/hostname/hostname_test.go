package hostname

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/testutil"
	v1 "k8s.io/api/core/v1"
)

var TestHelper *testutil.TestHelper

var hostnameClientTemplate = template.Must(template.New("hostname_client.yaml").ParseFiles("testdata/hostname_client.yaml"))

var (
	disabledApp = "disabled"
	disabledSC  = "slow-cooker-disabled"
	enabledApp  = "enabled"
	enabledSC   = "slow-cooker-enabled"
)

type testCase struct {
	name      string
	appName   string
	appChecks []check
	scName    string
	scChecks  []check
}

type check func(metrics, ns string) error

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until control plane is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdDeployReplicasEdge)
	os.Exit(m.Run())
}

// clientTemplateArgs is a struct that contains the arguments to be supplied
// to the deployment template hostname_client.yaml.
type clientTemplateArgs struct {
	ServiceCookerDisabledTargetHost string
	ServiceCookerEnabledTargetHost  string
}

func serviceName(n string) string {
	return fmt.Sprintf("svc-%s", n)
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestHostnameCalledByServiceTarget(t *testing.T) {
	ctx := context.Background()
	TestHelper.WithDataPlaneNamespace(ctx, "hostname-called-by-service-name-test", map[string]string{}, t, func(t *testing.T, hostnameNs string) {
		checks := func(c ...check) []check { return c }

		if err := deployApplications(hostnameNs); err != nil {
			testutil.AnnotatedFatal(t, "failed to deploy applications", err)
		}
		waitForAppDeploymentReady(t, hostnameNs)

		tmplArgs := clientTemplateArgs{
			ServiceCookerDisabledTargetHost: serviceName(disabledApp),
			ServiceCookerEnabledTargetHost:  serviceName(enabledApp),
		}
		if err := deployTemplate(hostnameNs, hostnameClientTemplate, tmplArgs); err != nil {
			testutil.AnnotatedFatal(t, "failed to deploy client pods", err)
		}
		waitForClientDeploymentReady(t, hostnameNs)

		runTests(ctx, t, hostnameNs, []testCase{
			{
				name:   "calling a meshed service with hostname metrics disabled",
				scName: disabledSC,
				scChecks: checks(
					hasOutboundHTTPRequestWithoutHostname,
					hasOutboundTCPWithTLSAndAuthority,
				),
				appName:   disabledApp,
				appChecks: checks(hasInboundTCPTrafficWithTLS),
			},
		})
		runTests(ctx, t, hostnameNs, []testCase{
			{
				name:   "calling a meshed service with hostname metrics enabled",
				scName: enabledSC,
				scChecks: checks(
					hasOutboundHTTPRequestWithHostname,
					hasOutboundTCPWithTLSAndAuthority,
				),
				appName:   enabledApp,
				appChecks: checks(hasInboundTCPTrafficWithTLS),
			},
		})
	})
}

func waitForAppDeploymentReady(t *testing.T, hostnameNs string) {
	TestHelper.WaitRollout(t, map[string]testutil.DeploySpec{
		disabledApp: {
			Namespace: hostnameNs,
			Replicas:  1,
		},
		enabledApp: {
			Namespace: hostnameNs,
			Replicas:  1,
		},
	})
}

func waitForClientDeploymentReady(t *testing.T, hostnameNs string) {
	TestHelper.WaitRollout(t, map[string]testutil.DeploySpec{
		disabledSC: {
			Namespace: hostnameNs,
			Replicas:  1,
		},
		enabledSC: {
			Namespace: hostnameNs,
			Replicas:  1,
		},
	})
}

func runTests(ctx context.Context, t *testing.T, ns string, tcs []testCase) {
	t.Helper()
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			err := testutil.RetryFor(30*time.Second, func() error {
				if err := checkPodMetrics(ctx, ns, tc.scName, tc.scChecks); err != nil {
					return fmt.Errorf("failed to check metrics for client pod: %w", err)
				}
				if tc.appName == "" {
					return nil
				}
				if err := checkPodMetrics(ctx, ns, tc.appName, tc.appChecks); err != nil {
					return fmt.Errorf("failed to check metrics for app pod: %w", err)
				}
				return nil
			})
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected metric for pod", "unexpected metric for pod: %s", err)
			}
		})
	}
}

func checkPodMetrics(ctx context.Context, hostnameNs string, podAppLabel string, checks []check) error {
	pods, err := TestHelper.GetPods(ctx, hostnameNs, map[string]string{"app": podAppLabel})
	if err != nil {
		return fmt.Errorf("error getting pods for label 'app: %q': %w", podAppLabel, err)
	}
	if len(pods) == 0 {
		return fmt.Errorf("no pods found for label 'app: %q'", podAppLabel)
	}
	metrics, err := getPodMetrics(pods[0], hostnameNs)
	if err != nil {
		return fmt.Errorf("error getting metrics for pod %q: %w", pods[0].Name, err)
	}
	for _, check := range checks {
		if err := check(metrics, hostnameNs); err != nil {
			return fmt.Errorf("validation of pod metrics failed: %w", err)
		}
	}
	return nil
}

func deployApplications(ns string) error {
	out, err := TestHelper.Kubectl("", "apply", "-f", "testdata/hostname_application.yaml", "-n", ns)
	if err != nil {
		return fmt.Errorf("failed apply deployment file %q: %w", out, err)
	}
	return nil
}

func deployTemplate(ns string, tmpl *template.Template, templateArgs interface{}) error {
	bb := &bytes.Buffer{}
	if err := tmpl.Execute(bb, templateArgs); err != nil {
		return fmt.Errorf("failed to write deployment template: %w", err)
	}
	out, err := TestHelper.KubectlApply(bb.String(), ns)
	if err != nil {
		return fmt.Errorf("failed apply deployment file %q: %w", out, err)
	}
	return nil
}

func getPodMetrics(pod v1.Pod, ns string) (string, error) {
	podName := fmt.Sprintf("pod/%s", pod.Name)
	cmd := []string{"diagnostics", "proxy-metrics", "--namespace", ns, podName}
	metrics, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		return "", err
	}
	return metrics, nil
}
