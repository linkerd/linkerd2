package client

import (
	"context"

	"github.com/linkerd/linkerd2/pkg/k8s"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	apiPort       = 8085
	apiDeployment = "metrics-api"
)

// NewInternalClient creates a new Viz API client intended to run inside a
// Kubernetes cluster.
func NewInternalClient(addr string) (pb.ApiClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		return nil, err
	}

	return pb.NewApiClient(conn), nil
}

// NewExternalClient creates a new Viz API client intended to run from
// outside a Kubernetes cluster.
func NewExternalClient(ctx context.Context, namespace string, kubeAPI *k8s.KubernetesAPI) (pb.ApiClient, error) {
	portforward, err := k8s.NewPortForward(
		ctx,
		kubeAPI,
		namespace,
		apiDeployment,
		"localhost",
		0,
		apiPort,
		false,
	)
	if err != nil {
		return nil, err
	}

	addr := portforward.AddressAndPort()
	if err = portforward.Init(); err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		return nil, err
	}

	return pb.NewApiClient(conn), nil
}
