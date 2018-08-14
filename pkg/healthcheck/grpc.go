package healthcheck

import (
	"context"
	"fmt"
	"time"

	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	"google.golang.org/grpc"
)

const grpcApiSubsystemName = "linkerd-api"

type grpcStatusChecker interface {
	SelfCheck(ctx context.Context, in *healthcheckPb.SelfCheckRequest, opts ...grpc.CallOption) (*healthcheckPb.SelfCheckResponse, error)
}

type statusCheckerProxy struct {
	delegate grpcStatusChecker
}

func (proxy *statusCheckerProxy) SelfCheck() []*healthcheckPb.CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	selfCheckResponse, err := proxy.delegate.SelfCheck(ctx, &healthcheckPb.SelfCheckRequest{})
	if err != nil {
		return []*healthcheckPb.CheckResult{
			&healthcheckPb.CheckResult{
				SubsystemName:         grpcApiSubsystemName,
				CheckDescription:      "can query the Linkerd API",
				Status:                healthcheckPb.CheckStatus_ERROR,
				FriendlyMessageToUser: err.Error(),
			},
		}
	}

	for _, check := range selfCheckResponse.Results {
		fullSubsystemName := fmt.Sprintf("%s[%s]", grpcApiSubsystemName, check.SubsystemName)
		check.SubsystemName = fullSubsystemName
	}

	return selfCheckResponse.Results
}

func NewGrpcStatusChecker(grpClient grpcStatusChecker) StatusChecker {
	return &statusCheckerProxy{
		delegate: grpClient,
	}
}
