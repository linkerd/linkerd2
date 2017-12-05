package srv

import (
	"context"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"google.golang.org/grpc"
)

type MockApiClient struct{}

func (m MockApiClient) Stat(ctx context.Context, in *pb.MetricRequest, opts ...grpc.CallOption) (*pb.MetricResponse, error) {
	return &pb.MetricResponse{}, nil
}
func (m MockApiClient) Version(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.VersionInfo, error) {
	version := &pb.VersionInfo{
		GoVersion:      "the best one",
		BuildDate:      "never",
		ReleaseVersion: "0.3.3",
	}
	return version, nil
}
func (m MockApiClient) ListPods(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	return &pb.ListPodsResponse{}, nil
}

func (m MockApiClient) Tap(context.Context, *pb.TapRequest, ...grpc.CallOption) (pb.Api_TapClient, error) {
	return nil, nil
}

func FakeServer() Server {
	return Server{
		templateDir: "../templates",
		reload:      true,
	}
}
