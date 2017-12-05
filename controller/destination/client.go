package destination

import (
	pb "github.com/runconduit/conduit/controller/gen/proxy/destination"
	"google.golang.org/grpc"
)

func NewClient(addr string) (pb.DestinationClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return pb.NewDestinationClient(conn), conn, nil
}
