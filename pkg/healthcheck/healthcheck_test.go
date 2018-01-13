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

	t.Run("Returns all checks by subsystems", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(workingSubsystem2)
		healthChecker.Add(failingSubsystem1)

		results := healthChecker.PerformCheck(nil)

		allExpectedResults := make([]*pb.CheckResult, 0)
		allExpectedResults = append(allExpectedResults, workingSubsystem1.checksToReturn...)
		allExpectedResults = append(allExpectedResults, workingSubsystem2.checksToReturn...)
		allExpectedResults = append(allExpectedResults, failingSubsystem1.checksToReturn...)

		expectedLength := len(allExpectedResults)
		actualLength := len(results.Results)

		if actualLength != expectedLength {
			t.Fatalf("Expecting check results to contain [%d] results, got [%d]", expectedLength, actualLength)
		}

		actualChecksSet := make(map[pb.CheckResult]bool)
		for _, result := range results.Results {
			actualChecksSet[*result] = true
		}

		for _, expected := range allExpectedResults {
			if !actualChecksSet[*expected] {
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

		observedResults := make([]*pb.CheckResult, 0)
		observer := func(r *pb.CheckResult) {
			observedResults = append(observedResults, r)
		}

		check := healthChecker.PerformCheck(observer)

		observedLength := len(observedResults)
		expectedLength := len(check.Results)

		if expectedLength != observedLength {
			t.Fatalf("Expecting observed check to contain [%d] check, got [%d]", expectedLength, observedLength)
		}

		observedResultsSet := make(map[pb.CheckResult]bool)
		for _, result := range observedResults {
			observedResultsSet[*result] = true
		}

		for _, observed := range check.Results {
			if !observedResultsSet[*observed] {
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

		if results.OverallStatus != pb.CheckStatus_OK {
			t.Fatalf("Expecting check to be successful, but got [%v]", results)
		}
	})

	t.Run("Is failure if even a single test failed", func(t *testing.T) {
		healthChecker := MakeHealthChecker()

		healthChecker.Add(workingSubsystem1)
		healthChecker.Add(failingSubsystem1)
		healthChecker.Add(workingSubsystem2)

		results := healthChecker.PerformCheck(nil)

		if results.OverallStatus != pb.CheckStatus_FAIL {
			t.Fatalf("Expecting check to be error, but got [%v]", results)
		}
	})
}
