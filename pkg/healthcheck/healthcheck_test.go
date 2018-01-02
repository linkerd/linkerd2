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
			{SubsystemName: "w1", CheckDescription: "w1a", Status: CheckOk},
			{SubsystemName: "w1", CheckDescription: "w1b", Status: CheckOk},
		},
	}
	workingSubsystem2 := &mockSubsystem{
		checksToReturn: []CheckResult{
			{SubsystemName: "w2", CheckDescription: "w2a", Status: CheckOk},
			{SubsystemName: "w2", CheckDescription: "w2b", Status: CheckOk},
		},
	}

	failingSubsystem1 := &mockSubsystem{
		checksToReturn: []CheckResult{
			{SubsystemName: "f1", CheckDescription: "fa", Status: CheckOk},
			{SubsystemName: "f1", CheckDescription: "fb", Status: CheckFailed},
		},
	}

	erroringSubsystem1 := &mockSubsystem{
		errToReturn:    errors.New("expected"),
		checksToReturn: []CheckResult{{SubsystemName: "e1", CheckDescription: "this should always be ignored because of the error", Status: CheckOk}},
	}

	t.Run("Returns all checks by subsystems", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(workingSubsystem2)
		healthChecker.Add(failingSubsystem1)

		results := healthChecker.PerformCheck(nil)

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

	t.Run("Notifies observer of all results", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(workingSubsystem2)
		healthChecker.Add(failingSubsystem1)

		observedResults := make([]CheckResult, 0)
		observer := func(r CheckResult) {
			observedResults = append(observedResults, r)
		}

		check := healthChecker.PerformCheck(observer)

		observedLength := len(observedResults)
		expectedLength := len(check.Results)

		if expectedLength != observedLength {
			t.Fatalf("Expecting observed check to contain [%d] check, got [%d]", expectedLength, observedLength)
		}

		observedResultsSet := make(map[CheckResult]bool)
		for _, result := range observedResults {
			observedResultsSet[result] = true
		}

		for _, observed := range check.Results {
			if !observedResultsSet[observed] {
				t.Fatalf("Expected observed results to contain [%v], but was: %v", observed,
					reflect.ValueOf(observedResultsSet).MapKeys())
			}
		}
	})

	t.Run("Is successful if all checks were successful", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(workingSubsystem2)

		results := healthChecker.PerformCheck(nil)

		if results.OverallStatus != CheckOk {
			t.Fatalf("Expecting check to be successful, but got [%v]", results)
		}
	})

	t.Run("Is failure if even a single test failed", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(failingSubsystem1)
		healthChecker.Add(workingSubsystem2)

		results := healthChecker.PerformCheck(nil)

		if results.OverallStatus != CheckFailed {
			t.Fatalf("Expecting check to be error, but got [%v]", results)
		}
	})

	t.Run("Is in error if even a single test returned error", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(failingSubsystem1)
		healthChecker.Add(workingSubsystem2)
		healthChecker.Add(erroringSubsystem1)

		results := healthChecker.PerformCheck(nil)

		if results.OverallStatus != CheckError {
			t.Fatalf("Expecting check to be error, but got [%v]", results)
		}

		expectedNumberOfChecks := len(workingSubsystem1.checksToReturn) + len(workingSubsystem2.checksToReturn) + len(failingSubsystem1.checksToReturn)
		if len(results.Results) > expectedNumberOfChecks {
			t.Fatalf("Expecting errored checks to be ignored, but got [%v]", results)
		}
	})
}
