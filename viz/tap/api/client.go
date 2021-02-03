package api

import (
	pb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
	"google.golang.org/grpc"
)

// NewClient creates a client for the control-plane's Tap service.
func NewClient(addr string) (pb.TapClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}
	return pb.NewTapClient(conn), conn, nil
}
