package destination

import (
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"google.golang.org/grpc"
)

// NewClient creates a client for the control plane Destination API that
// implements the Destination service.
func NewClient(addr string) (pb.DestinationClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return pb.NewDestinationClient(conn), conn, nil
}
