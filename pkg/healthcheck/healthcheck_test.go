package healthcheck

import (
	"reflect"
	"testing"

	pb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
)

type mockSubsystem struct {
	checksToReturn []*pb.CheckResult
}

func (m *mockSubsystem) SelfCheck() []*pb.CheckResult {
	return m.checksToReturn
}

func TestSelfChecker(t *testing.T) {
	workingSubsystem1 := &mockSubsystem{
		checksToReturn: []*pb.CheckResult{
			{SubsystemName: "w1", CheckDescription: "w1a", Status: pb.CheckStatus_OK},
			{SubsystemName: "w1", CheckDescription: "w1b", Status: pb.CheckStatus_OK},
		},
	}
	workingSubsystem2 := &mockSubsystem{
		checksToReturn: []*pb.CheckResult{
			{SubsystemName: "w2", CheckDescription: "w2a", Status: pb.CheckStatus_OK},
			{SubsystemName: "w2", CheckDescription: "w2b", Status: pb.CheckStatus_OK},
		},
	}

	failingSubsystem1 := &mockSubsystem{
		checksToReturn: []*pb.CheckResult{
			{SubsystemName: "f1", CheckDescription: "fa", Status: pb.CheckStatus_OK},
			{SubsystemName: "f1", CheckDescription: "fb", Status: pb.CheckStatus_FAIL},
		},
	}

	errorSubsystem1 := &mockSubsystem{
		checksToReturn: []*pb.CheckResult{
			{SubsystemName: "e1", CheckDescription: "ea", Status: pb.CheckStatus_ERROR},
			{SubsystemName: "e1", CheckDescription: "eb", Status: pb.CheckStatus_OK},
		},
	}

	t.Run("Notifies observer of all results", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(workingSubsystem2)
		healthChecker.Add(failingSubsystem1)

		observedResults := make([]*pb.CheckResult, 0)
		observer := func(r *pb.CheckResult) {
			observedResults = append(observedResults, r)
		}

		allChecks := make([]*pb.CheckResult, 0)
		allChecks = append(allChecks, workingSubsystem1.checksToReturn...)
		allChecks = append(allChecks, workingSubsystem2.checksToReturn...)
		allChecks = append(allChecks, failingSubsystem1.checksToReturn...)

		expectedResults := make([]*pb.CheckResult, 0)
		for _, check := range allChecks {
			expectedResults = append(expectedResults, check)
		}

		healthChecker.PerformCheck(observer)

		observedLength := len(observedResults)
		expectedLength := len(expectedResults)

		if expectedLength != observedLength {
			t.Fatalf("Expecting observed check to contain [%d] check, got [%d]", expectedLength, observedLength)
		}

		observedResultsSet := make(map[pb.CheckResult]bool)
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

		if checkStatus != pb.CheckStatus_OK {
			t.Fatalf("Expecting check to be successful, but got [%s]", checkStatus)
		}
	})

	t.Run("Is failure if even a single test failed and no errors", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(failingSubsystem1)
		healthChecker.Add(workingSubsystem2)

		checkStatus := healthChecker.PerformCheck(nil)

		if checkStatus != pb.CheckStatus_FAIL {
			t.Fatalf("Expecting check to be error, but got [%s]", checkStatus)
		}
	})

	t.Run("Is error if even a single test errored", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(failingSubsystem1)
		healthChecker.Add(errorSubsystem1)

		checkStatus := healthChecker.PerformCheck(nil)

		if checkStatus != pb.CheckStatus_ERROR {
			t.Fatalf("Expecting check to be error, but got [%s]", checkStatus)
		}
	})
}
