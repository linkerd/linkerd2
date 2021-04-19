package destination

import (
	"context"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/grpc"
)

const (
	destinationPort       = 8086
	destinationDeployment = "linkerd-destination"
)

// NewClient creates a client for the control plane Destination API that
// implements the Destination service.
func NewClient(addr string) (pb.DestinationClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithStatsHandler(&ocgrpc.ClientHandler{}))
	if err != nil {
		return nil, nil, err
	}

	return pb.NewDestinationClient(conn), conn, nil
}

// NewExternalClient creates a client for the control plane Destination API
// to run from outside a Kubernetes cluster.
func NewExternalClient(ctx context.Context, controlPlaneNamespace string, kubeAPI *k8s.KubernetesAPI) (pb.DestinationClient, *grpc.ClientConn, error) {
	portforward, err := k8s.NewPortForward(
		ctx,
		kubeAPI,
		controlPlaneNamespace,
		destinationDeployment,
		"localhost",
		0,
		destinationPort,
		false,
	)
	if err != nil {
		return nil, nil, err
	}

	destinationAddress := portforward.AddressAndPort()
	if err = portforward.Init(); err != nil {
		return nil, nil, err
	}

	return NewClient(destinationAddress)
}
