package healthcheck

import (
	pb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
)

type StatusChecker interface {
	SelfCheck() []*pb.CheckResult
}

type CheckObserver func(result *pb.CheckResult)

type HealthChecker struct {
	subsystemsToCheck []StatusChecker
}

func (hC *HealthChecker) Add(subsystemChecker StatusChecker) {
	hC.subsystemsToCheck = append(hC.subsystemsToCheck, subsystemChecker)
}

func (hC *HealthChecker) PerformCheck(observer CheckObserver) pb.CheckStatus {
	var overallStatus pb.CheckStatus

	for _, checker := range hC.subsystemsToCheck {
		for _, singleResult := range checker.SelfCheck() {
			checkResultContainsError := singleResult.Status == pb.CheckStatus_ERROR
			shouldOverrideStatus := singleResult.Status == pb.CheckStatus_FAIL && overallStatus == pb.CheckStatus_OK

			if checkResultContainsError || shouldOverrideStatus {
				overallStatus = singleResult.Status
			}

			if observer != nil {
				observer(singleResult)
			}
		}
	}

	return overallStatus
}

func MakeHealthChecker() *HealthChecker {
	return &HealthChecker{
		subsystemsToCheck: make([]StatusChecker, 0),
	}
}
