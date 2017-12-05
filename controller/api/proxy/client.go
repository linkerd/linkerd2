package proxy

import (
	pb "github.com/runconduit/conduit/controller/gen/proxy/telemetry"
	"google.golang.org/grpc"
)

func NewTelemetryClient(addr string) (pb.TelemetryClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}

	return pb.NewTelemetryClient(conn), conn, nil
}
