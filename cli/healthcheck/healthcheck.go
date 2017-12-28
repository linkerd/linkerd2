package healthcheck

import log "github.com/sirupsen/logrus"

type CheckStatus string

const (
	CheckOk     = CheckStatus("OK")
	CheckFailed = CheckStatus("FAIL")
	CheckError  = CheckStatus("ERROR")
)

type CheckResult struct {
	SubsystemName    string
	CheckDescription string
	Status           CheckStatus
	NextSteps        string
}

type Check struct {
	Results       []CheckResult
	OverallStatus CheckStatus
}

type StatusChecker interface {
	SelfCheck() ([]CheckResult, error)
}

type CheckObserver func(result CheckResult)

type HealthChecker struct {
	subsystemsToCheck []StatusChecker
}

func (hC *HealthChecker) Add(subsystemChecker StatusChecker) {
	hC.subsystemsToCheck = append(hC.subsystemsToCheck, subsystemChecker)
}

func (hC *HealthChecker) PerformCheck(observer CheckObserver) Check {
	if observer == nil {
		observer = func(_ CheckResult) {}
	}

	check := Check{
		Results:       make([]CheckResult, 0),
		OverallStatus: CheckOk,
	}

	for _, checker := range hC.subsystemsToCheck {
		results, err := checker.SelfCheck()
		if err != nil {
			log.Errorf("Error checking [%s]: %s", checker, err)
			check.OverallStatus = CheckError
			continue
		}
		for _, singleResult := range results {
			check.Results = append(check.Results, singleResult)
			checkResultContainsError := singleResult.Status == CheckError
			shouldOverrideStatus := singleResult.Status == CheckFailed && check.OverallStatus == CheckOk

			if checkResultContainsError || shouldOverrideStatus {
				check.OverallStatus = singleResult.Status
			}
			observer(singleResult)
		}
	}
	return check
}

func MakeHealthChecker() *HealthChecker {
	return &HealthChecker{
		subsystemsToCheck: make([]StatusChecker, 0),
	}
}
