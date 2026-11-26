package testutil

import (
	"context"
	"fmt"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

const prometheusPort = 9090

// WaitForPrometheusQuery blocks until query, evaluated by the Prometheus
// deployment promDeploy in promNamespace, returns at least one series.
//
// Use it to wait until Prometheus has ingested the samples an assertion depends
// on, instead of retrying the assertion itself. The wait ends as soon as the
// data is present; timeout only guards against a hang and should be well above
// the time a healthy cluster needs.
//
// Prometheus is reached through a port-forward, which connects over the pod's
// loopback interface and so is not subject to the proxy's inbound policy.
func (h *TestHelper) WaitForPrometheusQuery(ctx context.Context, promNamespace, promDeploy, query string, timeout time.Duration) error {
	addr, err := h.URLFor(ctx, promNamespace, promDeploy, prometheusPort)
	if err != nil {
		return fmt.Errorf("failed to port-forward to Prometheus %s/%s: %w", promNamespace, promDeploy, err)
	}

	client, err := promapi.NewClient(promapi.Config{Address: addr})
	if err != nil {
		return fmt.Errorf("failed to create Prometheus client for %s: %w", addr, err)
	}
	api := promv1.NewAPI(client)

	return RetryFor(timeout, func() error {
		queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		result, _, err := api.Query(queryCtx, query, time.Now())
		if err != nil {
			return fmt.Errorf("query %q to Prometheus failed: %w", query, err)
		}
		vector, ok := result.(model.Vector)
		if !ok {
			return fmt.Errorf("query %q to Prometheus returned a %s, expected a vector", query, result.Type())
		}
		if len(vector) == 0 {
			return fmt.Errorf("query %q to Prometheus returned no series", query)
		}
		return nil
	})
}
