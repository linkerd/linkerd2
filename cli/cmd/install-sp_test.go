package cmd

import (
	"bytes"
	"fmt"
	"testing"
)

func TestRenderSP(t *testing.T) {
	testCases := []struct {
		controlPlaneNamespace string
		clusterDomain         string
		goldenFileName        string
	}{
		{controlPlaneNamespace, "cluster.local", "install-sp_default.golden"},
		{"NAMESPACE", "CLUSTERDOMAIN", "install-sp_output.golden"},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, "testdata/"+tc.goldenFileName), func(t *testing.T) {
			var buf bytes.Buffer
			err := renderSP(&buf, tc.controlPlaneNamespace, tc.clusterDomain)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			testDataDiffer.DiffTestdata(t, tc.goldenFileName, buf.String())
		})
	}
}
