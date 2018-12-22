package proxy

import (
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"google.golang.org/grpc"
)

// NewClient creates a new gRPC client to the Destination service.
// TODO: consider moving this into destination-client, or removing altogether.
func NewClient(addr string) (pb.DestinationClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return pb.NewDestinationClient(conn), conn, nil
}
