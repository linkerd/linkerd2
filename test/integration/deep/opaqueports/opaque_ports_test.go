package opaqueports

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

var opaquePortsClientTemplate = template.Must(template.New("opaque_ports_client.yaml").ParseFiles("testdata/opaque_ports_client.yaml"))

var (
	opaquePodApp         = "opaque-pod"
	opaquePodSC          = "slow-cooker-opaque-pod"
	opaqueSvcApp         = "opaque-service"
	opaqueSvcSC          = "slow-cooker-opaque-service"
	opaqueUnmeshedSvcApp = "opaque-unmeshed"
	opaqueUnmeshedSvcPod = "opaque-unmeshed-svc"
	opaqueUnmeshedSvcSC  = "slow-cooker-opaque-unmeshed-svc"
)

type testCase struct {
	name      string
	appName   string
	appChecks []check
	scName    string
	scChecks  []check
}

type check func(metrics, ns string) error

func checks(c ...check) []check { return c }

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	// Block test execution until control plane is running
	TestHelper.WaitUntilDeployReady(testutil.LinkerdDeployReplicasEdge)
	os.Exit(m.Run())
}

// clientTemplateArgs is a struct that contains the arguments to be supplied
// to the deployment template opaque_ports_client.yaml.
type clientTemplateArgs struct {
	ServiceCookerOpaqueServiceTargetHost     string
	ServiceCookerOpaquePodTargetHost         string
	ServiceCookerOpaqueUnmeshedSVCTargetHost string
}

func serviceName(n string) string {
	return fmt.Sprintf("svc-%s", n)
}

//////////////////////
/// TEST EXECUTION ///
//////////////////////

func TestOpaquePortsCalledByServiceTarget(t *testing.T) {
	ctx := context.Background()
	TestHelper.WithDataPlaneNamespace(ctx, "opaque-ports-called-by-service-name-test", map[string]string{}, t, func(t *testing.T, opaquePortsNs string) {
		checks := func(c ...check) []check { return c }

		if err := deployApplications(opaquePortsNs); err != nil {
			testutil.AnnotatedFatal(t, "failed to deploy applications", err)
		}
		waitForAppDeploymentReady(t, opaquePortsNs)

		tmplArgs := clientTemplateArgs{
			ServiceCookerOpaqueServiceTargetHost:     serviceName(opaqueSvcApp),
			ServiceCookerOpaquePodTargetHost:         serviceName(opaquePodApp),
			ServiceCookerOpaqueUnmeshedSVCTargetHost: serviceName(opaqueUnmeshedSvcApp),
		}
		if err := deployTemplate(opaquePortsNs, opaquePortsClientTemplate, tmplArgs); err != nil {
			testutil.AnnotatedFatal(t, "failed to deploy client pods", err)
		}
		waitForClientDeploymentReady(t, opaquePortsNs)

		runTests(ctx, t, opaquePortsNs, []testCase{
			{
				name:   "calling a meshed service when opaque annotation is on receiving pod",
				scName: opaquePodSC,
				scChecks: checks(
					hasNoOutboundHTTPRequest,
					hasOutboundTCPWithTLSAndNoAuthority,
				),
				appName:   opaquePodApp,
				appChecks: checks(hasInboundTCPTrafficWithTLS),
			},
			{
				name:   "calling a meshed service when opaque annotation is on receiving service",
				scName: opaqueSvcSC,
				scChecks: checks(
					hasNoOutboundHTTPRequest,
					hasOutboundTCPWithTLSAndAuthority,
				),
				appName:   opaqueSvcApp,
				appChecks: checks(hasInboundTCPTrafficWithTLS),
			},
			{
				name:   "calling an unmeshed service when opaque annotation is on service",
				scName: opaqueUnmeshedSvcSC,
				scChecks: checks(
					hasNoOutboundHTTPRequest,
					hasOutboundTCPWithAuthorityAndNoTLS,
				),
			},
		})
	})
}

func TestOpaquePortsCalledByPodTarget(t *testing.T) {
	ctx := context.Background()
	TestHelper.WithDataPlaneNamespace(ctx, "opaque-ports-called-by-pod-ip-test", map[string]string{}, t, func(t *testing.T, opaquePortsNs string) {

		if err := deployApplications(opaquePortsNs); err != nil {
			testutil.AnnotatedFatal(t, "failed to deploy applications", err)
		}
		waitForAppDeploymentReady(t, opaquePortsNs)

		tmplArgs, err := templateArgsPodIP(ctx, opaquePortsNs)
		if err != nil {
			testutil.AnnotatedFatal(t, "failed to fetch pod IPs", err)
		}

		if err := deployTemplate(opaquePortsNs, opaquePortsClientTemplate, tmplArgs); err != nil {
			testutil.AnnotatedFatal(t, "failed to deploy client pods", err)
		}
		waitForClientDeploymentReady(t, opaquePortsNs)

		runTests(ctx, t, opaquePortsNs, []testCase{
			{
				name:   "calling a meshed service when opaque annotation is on receiving pod",
				scName: opaquePodSC,
				scChecks: checks(
					hasNoOutboundHTTPRequest,
					hasOutboundTCPWithTLSAndNoAuthority,
				),
				appName:   opaquePodApp,
				appChecks: checks(hasInboundTCPTrafficWithTLS),
			},
			{
				name:   "calling a meshed service when opaque annotation is on receiving service",
				scName: opaqueSvcSC,
				scChecks: checks(
					// We call pods directly, so annotation on a service is ignored.
					hasOutboundHTTPRequestWithTLS,
					// No authority here, because we are calling the pod directly.
					hasOutboundTCPWithTLSAndNoAuthority,
				),
				appName:   opaqueSvcApp,
				appChecks: checks(hasInboundTCPTrafficWithTLS),
			},
			{
				name:   "calling an unmeshed service",
				scName: opaqueUnmeshedSvcSC,
				scChecks: checks(
					// We call pods directly, so annotation on a service is ignored.
					hasOutboundHTTPRequestNoTLS,
					// No authority here, because we are calling the pod directly.
					hasOutboundTCPWithNoTLSAndNoAuthority,
				),
			},
		})
	})
}

func waitForAppDeploymentReady(t *testing.T, opaquePortsNs string) {
	TestHelper.WaitRollout(t, map[string]testutil.DeploySpec{
		opaquePodApp: {
			Namespace: opaquePortsNs,
			Replicas:  1,
		},
		opaqueSvcApp: {
			Namespace: opaquePortsNs,
			Replicas:  1,
		},
		opaqueUnmeshedSvcPod: {
			Namespace: opaquePortsNs,
			Replicas:  1,
		},
	})
}

func waitForClientDeploymentReady(t *testing.T, opaquePortsNs string) {
	TestHelper.WaitRollout(t, map[string]testutil.DeploySpec{
		opaquePodSC: {
			Namespace: opaquePortsNs,
			Replicas:  1,
		},
		opaqueSvcSC: {
			Namespace: opaquePortsNs,
			Replicas:  1,
		},
		opaqueUnmeshedSvcSC: {
			Namespace: opaquePortsNs,
			Replicas:  1,
		},
	})
}

func templateArgsPodIP(ctx context.Context, ns string) (clientTemplateArgs, error) {
	opaquePodSCPodIP, err := getPodIPByAppLabel(ctx, ns, opaquePodApp)
	if err != nil {
		return clientTemplateArgs{}, fmt.Errorf("failed to fetch pod IP for %q: %w", opaquePodApp, err)
	}
	opaqueSvcSCPodIP, err := getPodIPByAppLabel(ctx, ns, opaqueSvcApp)
	if err != nil {
		return clientTemplateArgs{}, fmt.Errorf("failed to fetch pod IP for %q: %w", opaqueSvcApp, err)
	}
	opaqueUnmeshedSvcPodIP, err := getPodIPByAppLabel(ctx, ns, opaqueUnmeshedSvcPod)
	if err != nil {
		return clientTemplateArgs{}, fmt.Errorf("failed to fetch pod IP for %q: %w", opaqueUnmeshedSvcPod, err)
	}
	return clientTemplateArgs{
		ServiceCookerOpaquePodTargetHost:         opaquePodSCPodIP,
		ServiceCookerOpaqueServiceTargetHost:     opaqueSvcSCPodIP,
		ServiceCookerOpaqueUnmeshedSVCTargetHost: opaqueUnmeshedSvcPodIP,
	}, nil
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

func checkPodMetrics(ctx context.Context, opaquePortsNs string, podAppLabel string, checks []check) error {
	pods, err := TestHelper.GetPods(ctx, opaquePortsNs, map[string]string{"app": podAppLabel})
	if err != nil {
		return fmt.Errorf("error getting pods for label 'app: %q': %w", podAppLabel, err)
	}
	if len(pods) == 0 {
		return fmt.Errorf("no pods found for label 'app: %q'", podAppLabel)
	}
	metrics, err := getPodMetrics(pods[0], opaquePortsNs)
	if err != nil {
		return fmt.Errorf("error getting metrics for pod %q: %w", pods[0].Name, err)
	}
	for _, check := range checks {
		if err := check(metrics, opaquePortsNs); err != nil {
			return fmt.Errorf("validation of pod metrics failed: %w", err)
		}
	}
	return nil
}

func deployApplications(ns string) error {
	out, err := TestHelper.Kubectl("", "apply", "-f", "testdata/opaque_ports_application.yaml", "-n", ns)
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

func getPodIPByAppLabel(ctx context.Context, ns string, app string) (string, error) {
	labels := map[string]string{"app": app}
	pods, err := TestHelper.GetPods(ctx, ns, labels)
	if err != nil {
		return "", fmt.Errorf("failed to get pod by labels %v: %w", labels, err)
	}
	if len(pods) == 0 {
		return "", fmt.Errorf("no pods found for labels %v", labels)
	}
	return pods[0].Status.PodIP, nil
}
