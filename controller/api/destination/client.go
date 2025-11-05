package destination

import (
	"context"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	destinationPort       = 8086
	destinationDeployment = "linkerd-destination"
)

// NewClient creates a client for the control plane Destination API that
// implements the Destination service.
func NewClient(addr string) (pb.DestinationClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		return nil, nil, err
	}

	return pb.NewDestinationClient(conn), conn, nil
}

// NewExternalClient creates a client for the control plane Destination API
// to run from outside a Kubernetes cluster.
func NewExternalClient(ctx context.Context, controlPlaneNamespace string, kubeAPI *k8s.KubernetesAPI, pod string) (pb.DestinationClient, *grpc.ClientConn, error) {
	var portForward *k8s.PortForward
	var err error
	if pod == "" {
		portForward, err = k8s.NewPortForward(
			ctx,
			kubeAPI,
			controlPlaneNamespace,
			destinationDeployment,
			"localhost",
			0,
			destinationPort,
			false,
		)
	} else {
		portForward, err = k8s.NewPodPortForward(kubeAPI, controlPlaneNamespace, pod, "localhost", 0, destinationPort, false)
	}
	if err != nil {
		return nil, nil, err
	}

	destinationAddress := portForward.AddressAndPort()
	if err = portForward.Init(); err != nil {
		return nil, nil, err
	}

	return NewClient(destinationAddress)
}
