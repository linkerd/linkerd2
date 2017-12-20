package cmd

import (
	"context"

	pb "github.com/runconduit/conduit/controller/gen/public"
	"google.golang.org/grpc"
)

type mockApiClient struct {
	errorToReturn            error
	versionInfoToReturn      *pb.VersionInfo
	listPodsResponseToReturn *pb.ListPodsResponse
}

func (c *mockApiClient) Stat(ctx context.Context, in *pb.MetricRequest, opts ...grpc.CallOption) (*pb.MetricResponse, error) {
	return nil, c.errorToReturn
}

func (c *mockApiClient) Version(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.VersionInfo, error) {
	return c.versionInfoToReturn, c.errorToReturn
}

func (c *mockApiClient) ListPods(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	return c.listPodsResponseToReturn, c.errorToReturn
}

func (c *mockApiClient) Tap(ctx context.Context, in *pb.TapRequest, opts ...grpc.CallOption) (pb.Api_TapClient, error) {
	return nil, c.errorToReturn
}
