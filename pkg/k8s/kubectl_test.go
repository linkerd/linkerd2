package k8s

import (
	"io/ioutil"
	"testing"

	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	"github.com/runconduit/conduit/pkg/healthcheck"
	"github.com/runconduit/conduit/pkg/shell"
)

func TestKubectlVersion(t *testing.T) {
	t.Run("Correctly parses a Version string", func(t *testing.T) {
		versions := map[string][3]int{
			"Client Version: v1.8.4":        {1, 8, 4},
			"Client Version: v2.7.1":        {2, 7, 1},
			"Client Version: v2.0.1":        {2, 0, 1},
			"Client Version: v1.9.0-beta.2": {1, 9, 0},
		}

		shell := &shell.MockShell{}
		for k, expectedVersion := range versions {
			shell.OutputToReturn = append(shell.OutputToReturn, k)
			kctl, err := NewKubectl(shell)
			shell.OutputToReturn = append(shell.OutputToReturn, k)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			actualVersion, err := kctl.Version()

			if err != nil {
				t.Fatalf("Error parsing string: %v", err)
			}

			if actualVersion != expectedVersion {
				t.Fatalf("Expecting %s to be parsed into %v but got %v", k, expectedVersion, actualVersion)
			}
		}
	})

	t.Run("Returns error if Version string looks broken", func(t *testing.T) {
		versions := []string{
			"",
			"Client Version: 1.8.4",
			"Client Version: 1.8.",
			"Client Version",
			"Client Version: Version.Info{Major:\"1\", Minor:\"8\", GitVersion:\"v1.8.4\", GitCommit:\"9befc2b8928a9426501d3bf62f72849d5cbcd5a3\", GitTreeState:\"clean\", BuildDate:\"2017-11-20T05:28:34Z\", GoVersion:\"go1.8.3\", Compiler:\"gc\", Platform:\"darwin/amd64\"}",
		}

		shell := &shell.MockShell{}
		for _, expectedVersion := range versions {
			shell.OutputToReturn = append(shell.OutputToReturn, expectedVersion)
			_, err := NewKubectl(shell)

			if err == nil {
				t.Fatalf("Expected error parsing string: %s", expectedVersion)
			}
		}
	})
}

func TestIsCompatibleVersion(t *testing.T) {
	t.Run("Success when compatible versions", func(t *testing.T) {
		compatibleVersions := map[[3]int][3]int{
			{1, 8, 4}: {1, 8, 4},
			{1, 1, 1}: {1, 1, 1},
			{1, 1, 1}: {2, 1, 2},
			{1, 1, 1}: {1, 2, 1},
			{1, 1, 1}: {1, 1, 2},
			{1, 1, 1}: {100, 1, 2},
		}

		for e, a := range compatibleVersions {
			if !isCompatibleVersion(e, a) {
				t.Fatalf("Expected required version [%v] to be compatible with [%v] but it wasn't", e, a)
			}
		}
	})

	t.Run("Fail when incompatible versions", func(t *testing.T) {
		inCompatibleVersions := map[[3]int][3]int{
			{1, 8, 4}:    {1, 7, 1},
			{10, 10, 10}: {9, 10, 10},
			{10, 10, 10}: {10, 9, 10},
			{10, 10, 10}: {10, 10, 9},
			{10, 10, 10}: {0, 10, 9},
		}
		for e, a := range inCompatibleVersions {
			if isCompatibleVersion(e, a) {
				t.Fatalf("Expected required version [%v] to  NOT be compatible with [%v] but it was'", e, a)
			}
		}
	})
}

func TestKubectlSelfCheck(t *testing.T) {
	t.Run("Returns success when no problems found", func(t *testing.T) {
		shell := &shell.MockShell{}
		shell.OutputToReturn = append(shell.OutputToReturn, "Client Version: v1.8.4")
		var kubectlAsStatusChecker healthcheck.StatusChecker
		kubectlAsStatusChecker, _ = NewKubectl(shell)

		output, err := ioutil.ReadFile("testdata/kubectl_config.output")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		shell.OutputToReturn = append(shell.OutputToReturn, string(output))

		shell.OutputToReturn = append(shell.OutputToReturn, "Client Version: v1.8.4")

		output, err = ioutil.ReadFile("testdata/kubectl_cluster-info.output")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		shell.OutputToReturn = append(shell.OutputToReturn, string(output))

		results := kubectlAsStatusChecker.SelfCheck()

		expectedNumChecks := 3
		actualNumChecks := len(results)
		if actualNumChecks != expectedNumChecks {
			t.Fatalf("Expecting [%d] checks, got [%d]", expectedNumChecks, actualNumChecks)
		}

		checkResult(t, results[0], KubectlIsInstalledCheckDescription, healthcheckPb.CheckStatus_OK)
		checkResult(t, results[1], KubectlVersionCheckDescription, healthcheckPb.CheckStatus_OK)
		checkResult(t, results[2], KubectlConnectivityCheckDescription, healthcheckPb.CheckStatus_OK)
	})

	t.Run("Returns failures when problems were found", func(t *testing.T) {
		shell := &shell.MockShell{}
		shell.OutputToReturn = append(shell.OutputToReturn, "Client Version: v1.8.4")
		var kubectlAsStatusChecker healthcheck.StatusChecker
		kubectlAsStatusChecker, _ = NewKubectl(shell)

		output, err := ioutil.ReadFile("testdata/kubectl_config.output")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		shell.OutputToReturn = append(shell.OutputToReturn, string(output))

		shell.OutputToReturn = append(shell.OutputToReturn, "Client Version: v0.0.0")

		output, err = ioutil.ReadFile("testdata/kubectl_cluster-info.output")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		shell.OutputToReturn = append(shell.OutputToReturn, string(output))

		results := kubectlAsStatusChecker.SelfCheck()

		expectedNumChecks := 3
		actualNumChecks := len(results)
		if actualNumChecks != expectedNumChecks {
			t.Fatalf("Expecting [%d] checks, got [%d]", expectedNumChecks, actualNumChecks)
		}
		checkResult(t, results[0], KubectlIsInstalledCheckDescription, healthcheckPb.CheckStatus_OK)
		checkResult(t, results[1], KubectlVersionCheckDescription, healthcheckPb.CheckStatus_FAIL)
		checkResult(t, results[2], KubectlConnectivityCheckDescription, healthcheckPb.CheckStatus_OK)
	})
}

func TestNewKubectl(t *testing.T) {
	t.Run("Starts when kubectl is at compatible version", func(t *testing.T) {
		versions := map[string][3]int{
			"Client Version: v1.8.4":        {1, 8, 4},
			"Client Version: v1.9.0-beta.2": {1, 9, 0},
		}

		shell := &shell.MockShell{}
		for k, v := range versions {
			shell.OutputToReturn = append(shell.OutputToReturn, k)
			_, err := NewKubectl(shell)

			if err != nil {
				t.Fatalf("Unexpected error when kubectl is at version [%v]: %v", v, err)
			}
		}
	})

	t.Run("Doesnt start when kubectl is at incompatible version", func(t *testing.T) {
		versions := map[string][3]int{
			"Client Version: v1.7.1": {1, 7, 1},
			"Client Version: v0.0.1": {0, 0, 1},
		}

		shell := &shell.MockShell{}
		for k, v := range versions {
			shell.OutputToReturn = append(shell.OutputToReturn, k)
			_, err := NewKubectl(shell)

			if err == nil {
				t.Fatalf("Expecting error when starting with incompatible version [%v] but got nothing", v)
			}
		}
	})
}

func TestCanonicalKubernetesNameFromFriendlyName(t *testing.T) {
	t.Run("Returns canonical name for all known variants", func(t *testing.T) {
		expectations := map[string]string{
			"po":          KubernetesPods,
			"pod":         KubernetesPods,
			"deployment":  KubernetesDeployments,
			"deployments": KubernetesDeployments,
		}

		for input, expectedName := range expectations {
			actualName, err := CanonicalKubernetesNameFromFriendlyName(input)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if actualName != expectedName {
				t.Fatalf("Expected friendly name [%s] to resolve to [%s], but got [%s]", input, expectedName, actualName)
			}
		}
	})

	t.Run("Returns error if inout isn't a supported name", func(t *testing.T) {
		unsupportedNames := []string{
			"pdo", "dop", "paths", "path", "", "mesh",
		}

		for _, n := range unsupportedNames {
			out, err := CanonicalKubernetesNameFromFriendlyName(n)
			if err == nil {
				t.Fatalf("Expecting error when resolving [%s], but it did resolkve to [%s]", n, out)
			}
		}
	})
}

func checkResult(t *testing.T, actualResult *healthcheckPb.CheckResult, expectedDescription string, expectedStatus healthcheckPb.CheckStatus) {
	if actualResult.SubsystemName != KubectlSubsystemName || actualResult.CheckDescription != expectedDescription || actualResult.Status != expectedStatus {
		t.Fatalf("Expecting results to have subsytem [%s], description [%s] and status [%s], but got: %v", KubectlSubsystemName, expectedDescription, expectedStatus, actualResult)
	}
}
