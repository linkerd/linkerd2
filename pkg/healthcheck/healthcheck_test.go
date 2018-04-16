package healthcheck

import (
	"reflect"
	"testing"

	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
)

type mockSubsystem struct {
	checksToReturn []*healthcheckPb.CheckResult
}

func (m *mockSubsystem) SelfCheck() []*healthcheckPb.CheckResult {
	return m.checksToReturn
}

func TestSelfChecker(t *testing.T) {
	workingSubsystem1 := &mockSubsystem{
		checksToReturn: []*healthcheckPb.CheckResult{
			{SubsystemName: "w1", CheckDescription: "w1a", Status: healthcheckPb.CheckStatus_OK},
			{SubsystemName: "w1", CheckDescription: "w1b", Status: healthcheckPb.CheckStatus_OK},
		},
	}
	workingSubsystem2 := &mockSubsystem{
		checksToReturn: []*healthcheckPb.CheckResult{
			{SubsystemName: "w2", CheckDescription: "w2a", Status: healthcheckPb.CheckStatus_OK},
			{SubsystemName: "w2", CheckDescription: "w2b", Status: healthcheckPb.CheckStatus_OK},
		},
	}

	failingSubsystem1 := &mockSubsystem{
		checksToReturn: []*healthcheckPb.CheckResult{
			{SubsystemName: "f1", CheckDescription: "fa", Status: healthcheckPb.CheckStatus_OK},
			{SubsystemName: "f1", CheckDescription: "fb", Status: healthcheckPb.CheckStatus_FAIL},
		},
	}

	errorSubsystem1 := &mockSubsystem{
		checksToReturn: []*healthcheckPb.CheckResult{
			{SubsystemName: "e1", CheckDescription: "ea", Status: healthcheckPb.CheckStatus_ERROR},
			{SubsystemName: "e1", CheckDescription: "eb", Status: healthcheckPb.CheckStatus_OK},
		},
	}

	t.Run("Notifies observer of all results", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(workingSubsystem2)
		healthChecker.Add(failingSubsystem1)

		observedResults := make([]*healthcheckPb.CheckResult, 0)
		observer := func(r *healthcheckPb.CheckResult) {
			observedResults = append(observedResults, r)
		}

		expectedResults := make([]*healthcheckPb.CheckResult, 0)
		expectedResults = append(expectedResults, workingSubsystem1.checksToReturn...)
		expectedResults = append(expectedResults, workingSubsystem2.checksToReturn...)
		expectedResults = append(expectedResults, failingSubsystem1.checksToReturn...)

		healthChecker.PerformCheck(observer)

		observedLength := len(observedResults)
		expectedLength := len(expectedResults)

		if expectedLength != observedLength {
			t.Fatalf("Expecting observed check to contain [%d] check, got [%d]", expectedLength, observedLength)
		}

		observedResultsSet := make(map[healthcheckPb.CheckResult]bool)
		for _, result := range observedResults {
			observedResultsSet[*result] = true
		}

		for _, expected := range expectedResults {
			if !observedResultsSet[*expected] {
				t.Fatalf("Expected observed results to contain [%v], but was: %v", expected,
					reflect.ValueOf(observedResultsSet).MapKeys())
			}
		}
	})

	t.Run("Is successful if all checks were successful", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(workingSubsystem2)

		checkStatus := healthChecker.PerformCheck(nil)

		if checkStatus != healthcheckPb.CheckStatus_OK {
			t.Fatalf("Expecting check to be successful, but got [%s]", checkStatus)
		}
	})

	t.Run("Is failure if even a single test failed and no errors", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(failingSubsystem1)
		healthChecker.Add(workingSubsystem2)

		checkStatus := healthChecker.PerformCheck(nil)

		if checkStatus != healthcheckPb.CheckStatus_FAIL {
			t.Fatalf("Expecting check to be error, but got [%s]", checkStatus)
		}
	})

	t.Run("Is error if even a single test errored", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(failingSubsystem1)
		healthChecker.Add(errorSubsystem1)

		checkStatus := healthChecker.PerformCheck(nil)

		if checkStatus != healthcheckPb.CheckStatus_ERROR {
			t.Fatalf("Expecting check to be error, but got [%s]", checkStatus)
		}
	})
}
