package healthcheck

import (
	"errors"
	"reflect"
	"testing"
)

type mockSubsystem struct {
	checksToReturn []CheckResult
	errToReturn    error
}

func (m *mockSubsystem) SelfCheck() ([]CheckResult, error) {
	return m.checksToReturn, m.errToReturn
}

func TestSelfChecker(t *testing.T) {
	workingSubsystem1 := &mockSubsystem{
		checksToReturn: []CheckResult{
			{ComponentName: "w1", checkDescription: "w1a", status: CheckOk},
			{ComponentName: "w1", checkDescription: "w1b", status: CheckOk},
		},
	}
	workingSubsystem2 := &mockSubsystem{
		checksToReturn: []CheckResult{
			{ComponentName: "w2", checkDescription: "w2a", status: CheckOk},
			{ComponentName: "w2", checkDescription: "w2b", status: CheckOk},
		},
	}

	failingSubsystem1 := &mockSubsystem{
		checksToReturn: []CheckResult{
			{ComponentName: "f1", checkDescription: "fa", status: CheckOk},
			{ComponentName: "f1", checkDescription: "fb", status: CheckFailed},
		},
	}

	erroingSubsystem1 := &mockSubsystem{
		errToReturn: errors.New("Expected"),
	}

	t.Run("Returns all checks by subsystems", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(workingSubsystem2)
		healthChecker.Add(failingSubsystem1)

		results := healthChecker.PerformCheck()

		allExpectedResults := make([]CheckResult, 0)
		allExpectedResults = append(allExpectedResults, workingSubsystem1.checksToReturn...)
		allExpectedResults = append(allExpectedResults, workingSubsystem2.checksToReturn...)
		allExpectedResults = append(allExpectedResults, failingSubsystem1.checksToReturn...)

		expectedLength := len(allExpectedResults)
		actualLength := len(results.Results)

		if actualLength != expectedLength {
			t.Fatalf("Expecting check results to contain [%d] results, got [%d]", expectedLength, actualLength)
		}

		actualChecksSet := make(map[CheckResult]bool)
		for _, result := range results.Results {
			actualChecksSet[result] = true
		}

		for _, expected := range allExpectedResults {
			if !actualChecksSet[expected] {
				t.Fatalf("Expected results to contain [%v], but was: %v", expected,
					reflect.ValueOf(actualChecksSet).MapKeys())
			}
		}
	})

	t.Run("Is successful if all checks were succesful", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(workingSubsystem2)

		results := healthChecker.PerformCheck()

		if results.OverallStatus != CheckOk {
			t.Fatalf("Expecting check to be successful, but got [%v]", results)
		}
	})

	t.Run("Is failure if even a single test failed", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(failingSubsystem1)
		healthChecker.Add(workingSubsystem2)

		results := healthChecker.PerformCheck()

		if results.OverallStatus != CheckFailed {
			t.Fatalf("Expecting check to be error, but got [%v]", results)
		}
	})

	t.Run("Is in error if even a single test returned error", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(failingSubsystem1)
		healthChecker.Add(workingSubsystem2)
		healthChecker.Add(erroingSubsystem1)

		results := healthChecker.PerformCheck()

		if results.OverallStatus != CheckError {
			t.Fatalf("Expecting check to be error, but got [%v]", results)
		}
	})
}
