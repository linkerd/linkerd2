package healthcheck

import (
	"context"
	"fmt"

	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	"google.golang.org/grpc"
)

type grpcStatusChecker interface {
	SelfCheck(ctx context.Context, in *healthcheckPb.SelfCheckRequest, opts ...grpc.CallOption) (*healthcheckPb.SelfCheckResponse, error)
}

type statusCheckerProxy struct {
	delegate grpcStatusChecker
	prefix   string
}

func (proxy *statusCheckerProxy) SelfCheck() []*healthcheckPb.CheckResult {
	canConnectViaGrpcCheck := &healthcheckPb.CheckResult{
		Status:           healthcheckPb.CheckStatus_OK,
		SubsystemName:    proxy.prefix,
		CheckDescription: "can query the Conduit API",
	}

	selfCheckResponse, err := proxy.delegate.SelfCheck(context.Background(), &healthcheckPb.SelfCheckRequest{})
	if err != nil {
		canConnectViaGrpcCheck.Status = healthcheckPb.CheckStatus_ERROR
		canConnectViaGrpcCheck.FriendlyMessageToUser = err.Error()
		return []*healthcheckPb.CheckResult{canConnectViaGrpcCheck}
	}

	for _, check := range selfCheckResponse.Results {
		fullSubsystemName := fmt.Sprintf("%s[%s]", proxy.prefix, check.SubsystemName)
		check.SubsystemName = fullSubsystemName
	}

	subsystemResults := []*healthcheckPb.CheckResult{canConnectViaGrpcCheck}
	subsystemResults = append(subsystemResults, selfCheckResponse.Results...)
	return subsystemResults
}

func NewGrpcStatusChecker(name string, grpClient grpcStatusChecker) StatusChecker {
	return &statusCheckerProxy{
		prefix:   name,
		delegate: grpClient,
	}
}
