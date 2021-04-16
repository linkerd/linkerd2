package public

import (
	"fmt"
	"os"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
)

// CheckPublicAPIClientOrRetryOrExit executes status checks on the control
// plane. If the checks fail, then CLI will print an error and exit. If the
// hcOptions.retryDeadline param is specified, then the CLI will print a
// message to stderr and retry.
func CheckPublicAPIClientOrRetryOrExit(hcOptions healthcheck.Options) {
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
		healthcheck.LinkerdControlPlaneExistenceChecks,
	}

	hc := healthcheck.NewHealthChecker(checks, &hcOptions)

	hc.RunChecks(exitOnError)
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
		}
		fmt.Fprintf(os.Stderr, "%s: %s\n", msg, result.Err)

		checkCmd := "linkerd check"
		fmt.Fprintf(os.Stderr, "Validate the install with: %s\n", checkCmd)

		os.Exit(1)
	}
}
