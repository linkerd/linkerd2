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

func (hC *HealthChecker) PerformCheck(observer CheckObserver) pb.Check {
	check := pb.Check{
		Results:       make([]*pb.CheckResult, 0),
		OverallStatus: pb.CheckStatus_OK,
	}

	for _, checker := range hC.subsystemsToCheck {
		for _, singleResult := range checker.SelfCheck() {
			check.Results = append(check.Results, singleResult)
			checkResultContainsError := singleResult.Status == pb.CheckStatus_ERROR
			shouldOverrideStatus := singleResult.Status == pb.CheckStatus_FAIL && check.OverallStatus == pb.CheckStatus_OK

			if checkResultContainsError || shouldOverrideStatus {
				check.OverallStatus = singleResult.Status
			}

			if observer != nil {
				observer(singleResult)
			}
		}
	}

	return check
}

func MakeHealthChecker() *HealthChecker {
	return &HealthChecker{
		subsystemsToCheck: make([]StatusChecker, 0),
	}
}
