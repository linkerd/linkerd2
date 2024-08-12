package api

import (
	"context"
	"fmt"

	"github.com/linkerd/linkerd2/pkg/k8s"
	promApi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

func NewExternalPrometheusClient(ctx context.Context, kubeAPI *k8s.KubernetesAPI) (promv1.API, error) {
	portforward, err := k8s.NewPortForward(
		ctx,
		kubeAPI,
		"linkerd-viz",
		"prometheus",
		"localhost",
		0,
		9090,
		false,
	)
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("http://%s", portforward.AddressAndPort())
	if err = portforward.Init(); err != nil {
		return nil, err
	}

	promClient, err := promApi.NewClient(promApi.Config{Address: addr})
	if err != nil {
		return nil, err
	}

	return promv1.NewAPI(promClient), nil
}
