package healthcheck

import (
	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
)

type StatusChecker interface {
	SelfCheck() []*healthcheckPb.CheckResult
}

type CheckObserver func(result *healthcheckPb.CheckResult)

type HealthChecker struct {
	subsystemsToCheck []StatusChecker
}

func (hC *HealthChecker) Add(subsystemChecker StatusChecker) {
	hC.subsystemsToCheck = append(hC.subsystemsToCheck, subsystemChecker)
}

func (hC *HealthChecker) PerformCheck(observer CheckObserver) healthcheckPb.CheckStatus {
	var overallStatus healthcheckPb.CheckStatus

	for _, checker := range hC.subsystemsToCheck {
		for _, singleResult := range checker.SelfCheck() {
			checkResultContainsError := singleResult.Status == healthcheckPb.CheckStatus_ERROR
			shouldOverrideStatus := singleResult.Status == healthcheckPb.CheckStatus_FAIL && overallStatus == healthcheckPb.CheckStatus_OK

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
