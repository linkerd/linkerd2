package tap

import (
	pb "github.com/runconduit/conduit/controller/gen/controller/tap"
	"google.golang.org/grpc"
)

func NewClient(addr string) (pb.TapClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return pb.NewTapClient(conn), conn, nil
}
