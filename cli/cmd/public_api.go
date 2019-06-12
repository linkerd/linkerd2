package cmd

// TODO: this file should probably move out of `./cli/cmd` into something like
// `./cli/publicapi` or `./cli/pkg`:
// https://github.com/linkerd/linkerd2/issues/2735

import (
	"fmt"
	"os"
	"time"

	"github.com/linkerd/linkerd2/controller/api/public"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

// rawPublicAPIClient creates a raw public API client with no validation.
func rawPublicAPIClient() (pb.ApiClient, error) {
	if apiAddr != "" {
		return public.NewInternalClient(controlPlaneNamespace, apiAddr)
	}

	kubeAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, 0)
	if err != nil {
		return nil, err
	}

	return public.NewExternalPublicAPIClient(controlPlaneNamespace, kubeAPI)
}

// checkPublicAPIClientOrExit builds a new public API client and executes default status
// checks to determine if the client can successfully perform cli commands. If the
// checks fail, then CLI will print an error and exit.
func checkPublicAPIClientOrExit() public.APIClient {
	return checkPublicAPIClientOrRetryOrExit(time.Time{}, false)
}

// checkPublicAPIClientWithDeadlineOrExit builds a new public API client and executes status
// checks to determine if the client can successfully connect to the API. If the
// checks fail, then CLI will print an error and exit. If the retryDeadline
// param is specified, then the CLI will print a message to stderr and retry.
func checkPublicAPIClientOrRetryOrExit(retryDeadline time.Time, apiChecks bool) public.APIClient {
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.LinkerdControlPlaneExistenceChecks,
	}

	if apiChecks {
		checks = append(checks, healthcheck.LinkerdAPIChecks)
	}

	hc := newHealthChecker(checks, retryDeadline)

	hc.RunChecks(exitOnError)
	return hc.PublicAPIClient()
}

func newHealthChecker(checks []healthcheck.CategoryID, retryDeadline time.Time) *healthcheck.HealthChecker {
	return healthcheck.NewHealthChecker(checks, &healthcheck.Options{
		ControlPlaneNamespace: controlPlaneNamespace,
		KubeConfig:            kubeconfigPath,
		KubeContext:           kubeContext,
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
		if controlPlaneNamespace != defaultNamespace {
			checkCmd += fmt.Sprintf(" --linkerd-namespace %s", controlPlaneNamespace)
		}
		fmt.Fprintf(os.Stderr, "Validate the install with: %s\n", checkCmd)

		os.Exit(1)
	}
}
