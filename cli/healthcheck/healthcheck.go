package healthcheck

type CheckStatus string

const (
	CheckOk     = CheckStatus("OK")
	CheckFailed = CheckStatus("FAIL")
	CheckError  = CheckStatus("ERROR")
)

type CheckResult struct {
	ComponentName    string
	checkDescription string
	status           CheckStatus
}

type Check struct {
	Results       []CheckResult
	OverallStatus CheckStatus
}

type StatusChecker interface {
	SelfCheck() ([]CheckResult, error)
}

type HealthChecker struct {
	subsystemsToCheck []StatusChecker
}

func (hC *HealthChecker) Add(subsystemChecker StatusChecker) {
	hC.subsystemsToCheck = append(hC.subsystemsToCheck, subsystemChecker)
}

func (hC *HealthChecker) PerformCheck() Check {
	check := Check{
		Results:       make([]CheckResult, 0),
		OverallStatus: CheckOk,
	}

	for _, checker := range hC.subsystemsToCheck {
		results, err := checker.SelfCheck()
		if err != nil {
			check.OverallStatus = CheckError
		}
		for _, r := range results {
			check.Results = append(check.Results, r)
			checkResultContainsError := r.status == CheckError
			shouldOverriveStatus := r.status == CheckFailed && check.OverallStatus == CheckOk

			if checkResultContainsError || shouldOverriveStatus {
				check.OverallStatus = r.status
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
