package public

import (
	"context"
	"io"

	common "github.com/runconduit/conduit/controller/gen/common"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"github.com/runconduit/conduit/pkg/healthcheck"
	"google.golang.org/grpc"
)

type MockConduitApiClient struct {
	ErrorToReturn            error
	VersionInfoToReturn      *pb.VersionInfo
	ListPodsResponseToReturn *pb.ListPodsResponse
	MetricResponseToReturn   *pb.MetricResponse
	SelfCheckResultsToReturn []healthcheck.CheckResult
	Api_TapClientToReturn    pb.Api_TapClient
}

func (c *MockConduitApiClient) Stat(ctx context.Context, in *pb.MetricRequest, opts ...grpc.CallOption) (*pb.MetricResponse, error) {
	return c.MetricResponseToReturn, c.ErrorToReturn
}

func (c *MockConduitApiClient) Version(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.VersionInfo, error) {
	return c.VersionInfoToReturn, c.ErrorToReturn
}

func (c *MockConduitApiClient) ListPods(ctx context.Context, in *pb.Empty, opts ...grpc.CallOption) (*pb.ListPodsResponse, error) {
	return c.ListPodsResponseToReturn, c.ErrorToReturn
}

func (c *MockConduitApiClient) Tap(ctx context.Context, in *pb.TapRequest, opts ...grpc.CallOption) (pb.Api_TapClient, error) {
	return c.Api_TapClientToReturn, c.ErrorToReturn
}

func (c *MockConduitApiClient) SelfCheck() ([]healthcheck.CheckResult, error) {
	return c.SelfCheckResultsToReturn, c.ErrorToReturn
}

type MockApi_TapClient struct {
	TapEventsToReturn []common.TapEvent
	ErrorsToReturn    []error
	grpc.ClientStream
}

func (a *MockApi_TapClient) Recv() (*common.TapEvent, error) {
	var eventPopped common.TapEvent
	var errorPopped error
	if len(a.TapEventsToReturn) == 0 && len(a.ErrorsToReturn) == 0 {
		return nil, io.EOF
	}
	if len(a.TapEventsToReturn) != 0 {
		eventPopped, a.TapEventsToReturn = a.TapEventsToReturn[0], a.TapEventsToReturn[1:]
	}
	if len(a.ErrorsToReturn) != 0 {
		errorPopped, a.ErrorsToReturn = a.ErrorsToReturn[0], a.ErrorsToReturn[1:]
	}

	return &eventPopped, errorPopped
}
