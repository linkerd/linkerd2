package opaqueports

import (
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

type check func(metrics string) error

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

func TestOpaquePortsCalledByServiceName(t *testing.T) {
	ctx := context.Background()
	TestHelper.WithDataPlaneNamespace(ctx, "opaque-ports-called-by-service-name-test", map[string]string{}, t, func(t *testing.T, opaquePortsNs string) {
		checks := func(c ...check) []check { return c }

		if err := deployApplications(opaquePortsNs); err != nil {
			testutil.AnnotatedFatal(t, "failed to deploy applications", err)
		}
		tmplArgs := clientTemplateArgs{
			ServiceCookerOpaqueServiceTargetHost:     serviceName(opaqueSvcApp),
			ServiceCookerOpaquePodTargetHost:         serviceName(opaquePodApp),
			ServiceCookerOpaqueUnmeshedSVCTargetHost: serviceName(opaqueUnmeshedSvcApp),
		}
		if err := deployClients(t, opaquePortsNs, tmplArgs); err != nil {
			testutil.AnnotatedFatal(t, "failed to deploy client pods", err)
		}

		tests := []struct {
			name      string
			appName   string
			appChecks []check
			scName    string
			scChecks  []check
		}{
			{
				name:   "calling a meshed service when opaque annotation is on receiving pod",
				scName: opaquePodSC,
				scChecks: checks(
					hashNoOutbondHTTPRequest,
					hasOutboundTCPWithTLSAndNoAuthority,
				),
				appName:   opaquePodApp,
				appChecks: checks(hasInboundTCPTraffic),
			},
			{
				name:   "calling a meshed service when opaque annotation is on calling pod",
				scName: opaqueSvcSC,
				scChecks: checks(
					hashNoOutbondHTTPRequest,
					hasOutboundTCPWithTLSAndAuthority,
				),
				appName:   opaqueSvcApp,
				appChecks: checks(hasInboundTCPTraffic),
			},
			{
				name:   "calling an unmeshed service",
				scName: opaqueUnmeshedSvcSC,
				scChecks: checks(
					hashNoOutbondHTTPRequest,
					hasOutboundTCPWithAuthorityAndNoTLS,
				),
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				err := TestHelper.RetryFor(30*time.Second, func() error {
					if err := checkPodMetrics(ctx, opaquePortsNs, tc.scName, tc.scChecks); err != nil {
						return fmt.Errorf("failed to check metrics for client pod:%w", err)
					}
					if tc.appName == "" {
						return nil
					}
					if err := checkPodMetrics(ctx, opaquePortsNs, tc.appName, tc.appChecks); err != nil {
						return fmt.Errorf("failed to check metrics for app pod:%w", err)
					}
					return nil
				})
				if err != nil {
					testutil.AnnotatedFatalf(t, "unexpected metric for client pod", "unexpected metric for client pod: %s", err)
				}
			})
		}
	})
}

func TestOpaquePortsCalledByServiceIP(t *testing.T) {
	ctx := context.Background()
	TestHelper.WithDataPlaneNamespace(ctx, "opaque-ports-called-by-service-ip-test", map[string]string{}, t, func(t *testing.T, opaquePortsNs string) {
		checks := func(c ...check) []check { return c }

		if err := deployApplications(opaquePortsNs); err != nil {
			testutil.AnnotatedFatal(t, "failed to deploy applications", err)
		}
		tmplArgs, err := templateArgsServiceIP(ctx, opaquePortsNs)
		if err != nil {
			testutil.AnnotatedFatal(t, "failed to fetch service IPs", err)
		}

		if err := deployClients(t, opaquePortsNs, tmplArgs); err != nil {
			testutil.AnnotatedFatal(t, "failed to deploy client pods", err)
		}
		tests := []struct {
			name      string
			appName   string
			appChecks []check
			scName    string
			scChecks  []check
		}{
			// There is no test "calling a meshed service when opaque annotation is on receiving pod" in this
			// scenario, because the service is created with `clusterIP: None`.
			{
				name:   "calling a meshed service when opaque annotation is on calling pod",
				scName: opaqueSvcSC,
				scChecks: checks(
					hashNoOutbondHTTPRequest,
					hasOutboundTCPWithTLSAndAuthority,
				),
				appName:   opaqueSvcApp,
				appChecks: checks(hasInboundTCPTraffic),
			},
			{
				name:   "calling an unmeshed service",
				scName: opaqueUnmeshedSvcSC,
				scChecks: checks(
					hashNoOutbondHTTPRequest,
					hasOutboundTCPWithAuthorityAndNoTLS,
				),
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				err := TestHelper.RetryFor(30*time.Second, func() error {
					if err := checkPodMetrics(ctx, opaquePortsNs, tc.scName, tc.scChecks); err != nil {
						return fmt.Errorf("failed to check metrics for client pod:%w", err)
					}
					if tc.appName == "" {
						return nil
					}
					if err := checkPodMetrics(ctx, opaquePortsNs, tc.appName, tc.appChecks); err != nil {
						return fmt.Errorf("failed to check metrics for app pod:%w", err)
					}
					return nil
				})
				if err != nil {
					testutil.AnnotatedFatalf(t, "unexpected metric for client pod", "unexpected metric for client pod: %s", err)
				}
			})
		}
	})
}

func TestOpaquePortsCalledByPodIP(t *testing.T) {
	ctx := context.Background()
	TestHelper.WithDataPlaneNamespace(ctx, "opaque-ports-called-by-pod-ip-test", map[string]string{}, t, func(t *testing.T, opaquePortsNs string) {
		checks := func(c ...check) []check { return c }

		if err := deployApplications(opaquePortsNs); err != nil {
			testutil.AnnotatedFatal(t, "failed to deploy applications", err)
		}
		tmplArgs, err := templateArgsPodIP(ctx, opaquePortsNs)
		if err != nil {
			testutil.AnnotatedFatal(t, "failed to fetch service IPs", err)
		}

		if err := deployClients(t, opaquePortsNs, tmplArgs); err != nil {
			testutil.AnnotatedFatal(t, "failed to deploy client pods", err)
		}
		tests := []struct {
			name      string
			appName   string
			appChecks []check
			scName    string
			scChecks  []check
		}{
			{
				name:   "calling a meshed service when opaque annotation is on receiving pod",
				scName: opaquePodSC,
				scChecks: checks(
					hashNoOutbondHTTPRequest,
					hasOutboundTCPWithTLSAndNoAuthority,
				),
				appName:   opaquePodApp,
				appChecks: checks(hasInboundTCPTraffic),
			},
			{
				name:   "calling a meshed service when opaque annotation is on calling pod",
				scName: opaqueSvcSC,
				scChecks: checks(
					hashNoOutbondHTTPRequest,
					// No authority here, because we are calling the pod directly.
					hasOutboundTCPWithTLSAndNoAuthority,
				),
				appName:   opaqueSvcApp,
				appChecks: checks(hasInboundTCPTraffic),
			},
			{
				name:   "calling an unmeshed service",
				scName: opaqueUnmeshedSvcSC,
				scChecks: checks(
					hashNoOutbondHTTPRequest,
					// No authority here, because we are calling the pod directly.
					hasOutboundTCPWithNoTLSAndNoAuthority,
				),
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				err := TestHelper.RetryFor(30*time.Second, func() error {
					if err := checkPodMetrics(ctx, opaquePortsNs, tc.scName, tc.scChecks); err != nil {
						return fmt.Errorf("failed to check metrics for client pod:%w", err)
					}
					if tc.appName == "" {
						return nil
					}
					if err := checkPodMetrics(ctx, opaquePortsNs, tc.appName, tc.appChecks); err != nil {
						return fmt.Errorf("failed to check metrics for app pod:%w", err)
					}
					return nil
				})
				if err != nil {
					testutil.AnnotatedFatalf(t, "unexpected metric for client pod", "unexpected metric for client pod: %s", err)
				}
			})
		}
	})
}

func templateArgsServiceIP(ctx context.Context, ns string) (clientTemplateArgs, error) {
	opaqueSvcServiceIP, err := getServiceIP(ctx, ns, serviceName(opaqueSvcApp))
	if err != nil {
		return clientTemplateArgs{}, fmt.Errorf("failed to fetch service IP for %q: %w", opaqueSvcApp, err)
	}
	opaqueUnmeshedSvcServiceP, err := getServiceIP(ctx, ns, serviceName(opaqueUnmeshedSvcApp))
	if err != nil {
		return clientTemplateArgs{}, fmt.Errorf("failed to fetch service IP for %q: %w", opaqueUnmeshedSvcApp, err)
	}
	return clientTemplateArgs{
		// Field ServiceCookerOpaqueServiceTargetHost is skipped, because the corresponding service does
		// not have the IP.
		ServiceCookerOpaqueServiceTargetHost:     opaqueSvcServiceIP,
		ServiceCookerOpaqueUnmeshedSVCTargetHost: opaqueUnmeshedSvcServiceP,
	}, nil
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
		if err := check(metrics); err != nil {
			return fmt.Errorf("validation of client pod metrics failed: %w", err)
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

func deployClients(t *testing.T, ns string, templateArgs clientTemplateArgs) error {
	return deployTemplate(t, ns, opaquePortsClientTemplate, templateArgs)
}

func deployTemplate(t *testing.T, ns string, tmpl *template.Template, templateArgs interface{}) error {
	deploymentFile, err := os.CreateTemp("", "deployment.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer func() {
		if err := os.Remove(deploymentFile.Name()); err != nil {
			testutil.AnnotatedWarn(t, "failed to remove temporary file", "failed to remove temporary file: %w", err)
		}
	}()
	if err := tmpl.Execute(deploymentFile, templateArgs); err != nil {
		return fmt.Errorf("failed to write deployment template: %w", err)
	}
	if err := deploymentFile.Close(); err != nil {
		return fmt.Errorf("failed to close deployment file: %w", err)
	}
	out, err := TestHelper.Kubectl("", "apply", "-f", deploymentFile.Name(), "-n", ns)
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

func getServiceIP(ctx context.Context, ns string, serviceName string) (string, error) {
	svc, err := TestHelper.GetService(ctx, ns, serviceName)
	if err != nil {
		return "", fmt.Errorf("failed to get service %q: %w", serviceName, err)
	}
	return svc.Spec.ClusterIP, nil
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
