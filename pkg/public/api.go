package public

import (
	"context"
	"fmt"
	"os"

	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/public"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

// RawPublicAPIClient creates a raw public API client with no validation.
func RawPublicAPIClient(ctx context.Context, kubeAPI *k8s.KubernetesAPI, controlPlaneNamespace string, apiAddr string) (pb.ApiClient, error) {
	if apiAddr != "" {
		return public.NewInternalClient(controlPlaneNamespace, apiAddr)
	}

	return public.NewExternalClient(ctx, controlPlaneNamespace, kubeAPI)
}

// CheckPublicAPIClientOrRetryOrExit builds a new Public API client and executes status
// checks to determine if the client can successfully connect to the API. If the
// checks fail, then CLI will print an error and exit. If the hcOptions.retryDeadline
// param is specified, then the CLI will print a message to stderr and retry.
func CheckPublicAPIClientOrRetryOrExit(hcOptions healthcheck.Options) public.Client {
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.LinkerdControlPlaneExistenceChecks,
		healthcheck.LinkerdAPIChecks,
	}

	hc := healthcheck.NewHealthChecker(checks, &hcOptions)

	hc.RunChecks(exitOnError)
	return hc.PublicAPIClient()
}

// GetDestinationClient builds a new destination client and executes status
// checks to determine if the client can successfully connect. If the checks
// fail, then CLI will print an error and exit. If the hcOptions.retryDeadline
// param is specified, then the CLI will print a message to stderr and retry.
func GetDestinationClient(hcOptions healthcheck.Options) destinationPb.DestinationClient {
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.LinkerdControlPlaneExistenceChecks,
		healthcheck.LinkerdAPIChecks,
	}

	hc := healthcheck.NewHealthChecker(checks, &hcOptions)

	hc.RunChecks(exitOnError)
	return hc.DestinationClient()
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
