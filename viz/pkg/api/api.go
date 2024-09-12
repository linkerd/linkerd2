package api

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	vizHealthCheck "github.com/linkerd/linkerd2/viz/pkg/healthcheck"
	promApi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// CheckClientOrExit builds a new Viz API client and executes default status
// checks to determine if the client can successfully perform cli commands. If the
// checks fail, then CLI will print an error and exit.
func CheckClientOrExit(hcOptions vizHealthCheck.VizOptions) pb.ApiClient {
	hcOptions.RetryDeadline = time.Time{}
	return CheckClientOrRetryOrExit(hcOptions, false)
}

// CheckClientOrRetryOrExit builds a new Viz API client and executes status
// checks to determine if the client can successfully connect to the API. If the
// checks fail, then CLI will print an error and exit. If the hcOptions.retryDeadline
// param is specified, then the CLI will print a message to stderr and retry.
func CheckClientOrRetryOrExit(hcOptions vizHealthCheck.VizOptions, apiChecks bool) pb.ApiClient {
	checks := []healthcheck.CategoryID{
		healthcheck.KubernetesAPIChecks,
	}

	if apiChecks {
		checks = append(checks, healthcheck.LinkerdControlPlaneExistenceChecks)
	}

	hc := vizHealthCheck.NewHealthChecker(checks, &hcOptions)

	hc.AppendCategories(hc.VizCategory(false))

	hc.RunChecks(exitOnError)
	return hc.VizAPIClient()
}

func exitOnError(result *healthcheck.CheckResult) {
	if result.Retry {
		fmt.Fprintln(os.Stderr, "Waiting for linkerd-viz extension to become available")
		return
	}

	if result.Err != nil && !result.Warning {
		var msg string
		switch result.Category {
		case healthcheck.KubernetesAPIChecks:
			msg = "Cannot connect to Kubernetes"
		case vizHealthCheck.LinkerdVizExtensionCheck:
			msg = "Cannot connect to Linkerd Viz"
		}
		fmt.Fprintf(os.Stderr, "%s: %s\n", msg, result.Err)

		checkCmd := "linkerd viz check"
		fmt.Fprintf(os.Stderr, "Validate the install with: %s\n", checkCmd)

		os.Exit(1)
	}
}

func NewPrometheusClient(ctx context.Context, hcOptions vizHealthCheck.VizOptions, addr string) (promv1.API, error) {
	if addr == "" {
		checks := []healthcheck.CategoryID{
			healthcheck.KubernetesAPIChecks,
		}
		hc := vizHealthCheck.NewHealthChecker(checks, &hcOptions)
		hc.AppendCategories(hc.VizCategory(false))
		hc.RunChecks(exitOnError)

		if hc.ExternalPrometheusURL() == "" {
			portforward, err := k8s.NewPortForward(
				ctx,
				hc.KubeAPIClient(),
				hc.VizNamespace(),
				"prometheus",
				"localhost",
				0,
				9090,
				false,
			)
			if err != nil {
				return nil, err
			}

			addr = fmt.Sprintf("http://%s", portforward.AddressAndPort())

			if err = portforward.Init(); err != nil {
				return nil, err
			}
		} else {
			addr = hc.ExternalPrometheusURL()
		}
	}

	promClient, err := promApi.NewClient(promApi.Config{Address: addr})
	if err != nil {
		return nil, err
	}

	return promv1.NewAPI(promClient), nil
}
