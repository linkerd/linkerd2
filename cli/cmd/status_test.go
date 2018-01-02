package cmd

import (
	"bytes"
	"io/ioutil"
	"net/url"
	"testing"

	"github.com/runconduit/conduit/pkg/k8s"

	"github.com/runconduit/conduit/cli/healthcheck"
)

type mockKubectl struct {
	resultsToReturn []healthcheck.CheckResult
	errToReturn     error
}

func (m *mockKubectl) Version() ([3]int, error) { return [3]int{}, nil }
func (m *mockKubectl) StartProxy(potentialErrorWhenStartingProxy chan error, port int) error {
	return nil
}
func (m *mockKubectl) UrlFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error) {
	return nil, nil
}
func (m *mockKubectl) ProxyPort() int { return -666 }
func (m *mockKubectl) SelfCheck() ([]healthcheck.CheckResult, error) {
	return m.resultsToReturn, m.errToReturn
}

func TestCheckStatus(t *testing.T) {
	t.Run("Prints expected output", func(t *testing.T) {
		kubectl := &mockKubectl{}
		kubectl.resultsToReturn = []healthcheck.CheckResult{
			{SubsystemName: k8s.KubectlSubsystemName,
				CheckDescription: k8s.KubectlConnectivityCheckDescription,
				Status:           healthcheck.CheckOk,
				NextSteps:        "This shouldnt be printed",
			},
			{SubsystemName: k8s.KubectlSubsystemName,
				CheckDescription: k8s.KubectlIsInstalledCheckDescription,
				Status:           healthcheck.CheckFailed,
				NextSteps:        "This should contain instructions for fail",
			},
			{SubsystemName: k8s.KubectlSubsystemName,
				CheckDescription: k8s.KubectlVersionCheckDescription,
				Status:           healthcheck.CheckError,
				NextSteps:        "This should contain instructions for err",
			},
		}

		output := bytes.NewBufferString("")
		checkStatus(output, kubectl)

		goldenFileBytes, err := ioutil.ReadFile("testdata/status_busy_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedContent := string(goldenFileBytes)

		if expectedContent != output.String() {
			t.Fatalf("Expected function to render:\n%s\bbut got:\n%s", expectedContent, output)
		}
	})
}
