package k8s

import (
	"testing"
)

func TestGetK8sVersion(t *testing.T) {
	t.Run("Correctly parses a Version string", func(t *testing.T) {
		versions := map[string][3]int{
			"v1.8.4":               {1, 8, 4},
			"v2.7.1":               {2, 7, 1},
			"v2.0.1":               {2, 0, 1},
			"v1.9.0-beta.2":        {1, 9, 0},
			"v1.7.9+7f63532e4ff4f": {1, 7, 9},
		}

		for k, expectedVersion := range versions {
			actualVersion, err := getK8sVersion(k)
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
			"1",
			"1.8.",
			"1.9-beta.2",
			"v1.7+7f63532e4ff4f",
			"Client Version: v1.8.4",
			"Version.Info{Major:\"1\", Minor:\"8\", GitVersion:\"v1.8.4\", GitCommit:\"9befc2b8928a9426501d3bf62f72849d5cbcd5a3\", GitTreeState:\"clean\", BuildDate:\"2017-11-20T05:28:34Z\", GoVersion:\"go1.8.3\", Compiler:\"gc\", Platform:\"darwin/amd64\"}",
		}

		for _, invalidVersion := range versions {
			_, err := getK8sVersion(invalidVersion)

			if err == nil {
				t.Fatalf("Expected error parsing string: %s", invalidVersion)
			}
		}
	})
}

func TestIsCompatibleVersion(t *testing.T) {
	t.Run("Success when compatible versions", func(t *testing.T) {
		compatibleVersions := map[[3]int][3]int{
			{1, 8, 4}: {1, 8, 4},
			{1, 9, 2}: {1, 9, 4},
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
			{1, 9, 2}:    {1, 9, 0},
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
