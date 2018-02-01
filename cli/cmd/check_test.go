package cmd

import (
	"bytes"
	"io/ioutil"
	"testing"

	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	"github.com/runconduit/conduit/pkg/k8s"
)

func TestCheckStatus(t *testing.T) {
	t.Run("Prints expected output", func(t *testing.T) {
		kubectl := &k8s.MockKubectl{}
		kubectl.SelfCheckResultsToReturn = []*healthcheckPb.CheckResult{
			{
				SubsystemName:         k8s.KubectlSubsystemName,
				CheckDescription:      k8s.KubectlConnectivityCheckDescription,
				Status:                healthcheckPb.CheckStatus_OK,
				FriendlyMessageToUser: "This shouldnt be printed",
			},
			{
				SubsystemName:         k8s.KubectlSubsystemName,
				CheckDescription:      k8s.KubectlIsInstalledCheckDescription,
				Status:                healthcheckPb.CheckStatus_FAIL,
				FriendlyMessageToUser: "This should contain instructions for fail",
			},
			{
				SubsystemName:         k8s.KubectlSubsystemName,
				CheckDescription:      k8s.KubectlVersionCheckDescription,
				Status:                healthcheckPb.CheckStatus_ERROR,
				FriendlyMessageToUser: "This should contain instructions for err",
			},
		}

		kubeApi := &k8s.MockKubeApi{}
		kubeApi.SelfCheckResultsToReturn = []*healthcheckPb.CheckResult{
			{
				SubsystemName:         k8s.KubeapiSubsystemName,
				CheckDescription:      k8s.KubeapiClientCheckDescription,
				Status:                healthcheckPb.CheckStatus_FAIL,
				FriendlyMessageToUser: "This should contain instructions for fail",
			},
			{
				SubsystemName:         k8s.KubeapiSubsystemName,
				CheckDescription:      k8s.KubeapiAccessCheckDescription,
				Status:                healthcheckPb.CheckStatus_OK,
				FriendlyMessageToUser: "This shouldnt be printed",
			},
		}

		output := bytes.NewBufferString("")
		checkStatus(output, kubectl, kubeApi)

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
