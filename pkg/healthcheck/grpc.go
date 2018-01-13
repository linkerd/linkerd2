package healthcheck

import (
	"context"
	"fmt"

	pb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	"google.golang.org/grpc"
)

type grpcStatusChecker interface {
	SelfCheck(ctx context.Context, in *pb.SelfCheckRequest, opts ...grpc.CallOption) (*pb.SelfCheckResponse, error)
}

type statusCheckerProxy struct {
	delegate grpcStatusChecker
	prefix   string
}

func (proxy *statusCheckerProxy) SelfCheck() []*pb.CheckResult {
	canConnectViaGrpcCheck := &pb.CheckResult{
		Status:           pb.CheckStatus_OK,
		SubsystemName:    proxy.prefix,
		CheckDescription: "can retrieve status via gRPC",
	}

	selfCheckResponse, err := proxy.delegate.SelfCheck(context.Background(), &pb.SelfCheckRequest{})
	if err != nil {
		canConnectViaGrpcCheck.Status = pb.CheckStatus_ERROR
		canConnectViaGrpcCheck.FriendlyMessageToUser = err.Error()
		return []*pb.CheckResult{canConnectViaGrpcCheck}
	}

	for _, check := range selfCheckResponse.Results {
		fullSubsystemName := fmt.Sprintf("%s[%s]", proxy.prefix, check.SubsystemName)
		check.SubsystemName = fullSubsystemName
	}

	subsystemResults := []*pb.CheckResult{canConnectViaGrpcCheck}
	subsystemResults = append(subsystemResults, selfCheckResponse.Results...)
	return subsystemResults
}

func NewGrpcStatusChecker(name string, grpClient grpcStatusChecker) StatusChecker {
	return &statusCheckerProxy{
		prefix:   name,
		delegate: grpClient,
	}
}
