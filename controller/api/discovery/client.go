package discovery

import (
	pb "github.com/linkerd/linkerd2/controller/gen/controller/discovery"
	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/grpc"
)

// NewClient creates a client for control plane APIs that implement the
// Discovery service. This includes the public API and the destination API.
func NewClient(addr string) (pb.DiscoveryClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithStatsHandler(&ocgrpc.ClientHandler{}))
	if err != nil {
		return nil, nil, err
	}

	return pb.NewDiscoveryClient(conn), conn, nil
}
