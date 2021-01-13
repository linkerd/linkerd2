package public

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/linkerd/linkerd2/controller/api/public"
	publicPb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
)

// rawPublicAPIClient creates a raw public API client with no validation.
func rawPublicAPIClient(ctx context.Context, kubeAPI *k8s.KubernetesAPI, controlPlaneNamespace string, apiAddr string) (publicPb.ApiClient, error) {
	if apiAddr != "" {
		return public.NewInternalPublicClient(controlPlaneNamespace, apiAddr)
	}

	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
	if err != nil {
		return nil, err
	}

	return public.NewExternalPublicClient(ctx, controlPlaneNamespace, kubeAPI)
}

// rawVizAPIClient creates a raw viz API client with no validation.
func rawVizAPIClient(ctx context.Context, kubeAPI *k8s.KubernetesAPI, controlPlaneNamespace string, apiAddr string) (pb.ApiClient, error) {
	if apiAddr != "" {
		return public.NewInternalClient(controlPlaneNamespace, apiAddr)
	}

	return public.NewExternalClient(ctx, controlPlaneNamespace, kubeAPI)
}

// checkPublicAPIClientOrExit builds a new Public API client and executes default status
// checks to determine if the client can successfully perform cli commands. If the
// checks fail, then CLI will print an error and exit.
func checkPublicAPIClientOrExit(hcOptions healthcheck.Options) public.PublicAPIClient {
	hcOptions.RetryDeadline = time.Time{}
	return checkPublicAPIClientOrRetryOrExit(hcOptions, false)
}

func checkVizAPIClientOrExit(hcOptions healthcheck.Options) public.VizAPIClient {
	hcOptions.RetryDeadline = time.Time{}
	return checkVizAPIClientOrRetryOrExit(hcOptions, false)
}

// checkPublicAPIClientWithDeadlineOrExit builds a new Public API client and executes status
// checks to determine if the client can successfully connect to the API. If the
// checks fail, then CLI will print an error and exit. If the retryDeadline
// param is specified, then the CLI will print a message to stderr and retry.
func checkPublicAPIClientOrRetryOrExit(hcOptions healthcheck.Options, apiChecks bool) public.PublicAPIClient {
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.LinkerdControlPlaneExistenceChecks,
	}

	if apiChecks {
		checks = append(checks, healthcheck.LinkerdAPIChecks)
	}

	hc := healthcheck.NewHealthChecker(checks, &hcOptions)

	hc.RunChecks(exitOnError)
	return hc.PublicAPIClient()
}

func checkVizAPIClientOrRetryOrExit(retryDeadline time.Time, apiChecks bool) public.VizAPIClient {
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.LinkerdControlPlaneExistenceChecks,
	}

	if apiChecks {
		checks = append(checks, healthcheck.LinkerdAPIChecks)
	}

	hc := newHealthChecker(checks, retryDeadline)

	hc.RunChecks(exitOnError)
	return hc.VizAPIClient()
}

func newHealthChecker(checks []healthcheck.CategoryID, retryDeadline time.Time) *healthcheck.HealthChecker {
	return healthcheck.NewHealthChecker(checks, &healthcheck.Options{
		ControlPlaneNamespace: controlPlaneNamespace,
		KubeConfig:            kubeconfigPath,
		KubeContext:           kubeContext,
		Impersonate:           impersonate,
		ImpersonateGroup:      impersonateGroup,
		APIAddr:               apiAddr,
		RetryDeadline:         retryDeadline,
	})
}

func exitOnError(result *healthcheck.CheckResult) {
	if result.Retry {
		fmt.Fprintln(os.Stderr, "Waiting for control plane to become available")
		return
	}

	if result.Err != nil && !result.Warning {
		var msg string
		switch result.Category {
		case healthcheck.KubernetesAPIChecks:
			msg = "Cannot connect to Kubernetes"
		case healthcheck.LinkerdControlPlaneExistenceChecks:
			msg = "Cannot find Linkerd"
		case healthcheck.LinkerdAPIChecks:
			msg = "Cannot connect to Linkerd"
		}
		fmt.Fprintf(os.Stderr, "%s: %s\n", msg, result.Err)

		checkCmd := "linkerd check"
		fmt.Fprintf(os.Stderr, "Validate the install with: %s\n", checkCmd)

		os.Exit(1)
	}
}
