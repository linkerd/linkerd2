package cmd

import (
	"bytes"
	"io/ioutil"
	"testing"

	"github.com/runconduit/conduit/controller/api/public"

	"github.com/runconduit/conduit/pkg/healthcheck"
	"github.com/runconduit/conduit/pkg/k8s"
)

func TestCheckStatus(t *testing.T) {
	t.Run("Prints expected output", func(t *testing.T) {
		kubectl := &k8s.MockKubectl{}
		kubectl.SelfCheckResultsToReturn = []healthcheck.CheckResult{
			{
				SubsystemName:         k8s.KubectlSubsystemName,
				CheckDescription:      k8s.KubectlConnectivityCheckDescription,
				Status:                healthcheck.CheckOk,
				FriendlyMessageToUser: "This shouldnt be printed",
			},
			{
				SubsystemName:         k8s.KubectlSubsystemName,
				CheckDescription:      k8s.KubectlIsInstalledCheckDescription,
				Status:                healthcheck.CheckFailed,
				FriendlyMessageToUser: "This should contain instructions for fail",
			},
			{
				SubsystemName:         k8s.KubectlSubsystemName,
				CheckDescription:      k8s.KubectlVersionCheckDescription,
				Status:                healthcheck.CheckError,
				FriendlyMessageToUser: "This should contain instructions for err",
			},
		}

		kubeApi := &k8s.MockKubeApi{}
		kubeApi.SelfCheckResultsToReturn = []healthcheck.CheckResult{
			{
				SubsystemName:         k8s.KubeapiSubsystemName,
				CheckDescription:      k8s.KubeapiClientCheckDescription,
				Status:                healthcheck.CheckFailed,
				FriendlyMessageToUser: "This should contain instructions for fail",
			},
			{
				SubsystemName:         k8s.KubeapiSubsystemName,
				CheckDescription:      k8s.KubeapiAccessCheckDescription,
				Status:                healthcheck.CheckOk,
				FriendlyMessageToUser: "This shouldnt be printed",
			},
		}

		conduitApi := &public.MockConduitApiClient{}
		conduitApi.SelfCheckResultsToReturn = []healthcheck.CheckResult{
			{
				SubsystemName:         public.ConduitApiSubsystemName,
				CheckDescription:      public.ConduitApiConnectivityCheckDescription,
				Status:                healthcheck.CheckFailed,
				FriendlyMessageToUser: "This should contain instructions for fail",
			},
		}

		output := bytes.NewBufferString("")
		checkStatus(output, kubectl, kubeApi, conduitApi)

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
