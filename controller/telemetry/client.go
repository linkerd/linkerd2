package telemetry

import (
	pb "github.com/runconduit/conduit/controller/gen/controller/telemetry"
	"google.golang.org/grpc"
)

func NewClient(addr string) (pb.TelemetryClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return pb.NewTelemetryClient(conn), conn, nil
}
