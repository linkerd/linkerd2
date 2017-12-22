package cmd

import (
	"context"
	"io"

	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"google.golang.org/grpc"
)

type mockApiClient struct {
	errorToReturn            error
	versionInfoToReturn      *pb.VersionInfo
	listPodsResponseToReturn *pb.ListPodsResponse
	metricResponseToReturn   *pb.MetricResponse
	api_TapClientToReturn    pb.Api_TapClient
}

func (c *mockApiClient) Stat(ctx context.Context, in *pb.MetricRequest, opts ...grpc.CallOption) (*pb.MetricResponse, error) {
	return c.metricResponseToReturn, c.errorToReturn
}

func (c *mockApiClient) Version(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.VersionInfo, error) {
	return c.versionInfoToReturn, c.errorToReturn
}

func (c *mockApiClient) ListPods(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	return c.listPodsResponseToReturn, c.errorToReturn
}

func (c *mockApiClient) Tap(ctx context.Context, in *pb.TapRequest, opts ...grpc.CallOption) (pb.Api_TapClient, error) {
	return c.api_TapClientToReturn, c.errorToReturn
}

type mockApi_TapClient struct {
	tapEventsToReturn []common.TapEvent
	errorsToReturn    []error
	grpc.ClientStream
}

func (a *mockApi_TapClient) Recv() (*common.TapEvent, error) {
	var eventPopped common.TapEvent
	var errorPopped error
	if len(a.tapEventsToReturn) == 0 && len(a.errorsToReturn) == 0 {
		return nil, io.EOF
	}
	if len(a.tapEventsToReturn) != 0 {
		eventPopped, a.tapEventsToReturn = a.tapEventsToReturn[0], a.tapEventsToReturn[1:]
	}
	if len(a.errorsToReturn) != 0 {
		errorPopped, a.errorsToReturn = a.errorsToReturn[0], a.errorsToReturn[1:]
	}

	return &eventPopped, errorPopped
}
