package cmd

import (
	"bytes"
	"fmt"
	"testing"
)

func TestRenderSP(t *testing.T) {
	testCases := []struct {
		controlPlaneNamespace string
		goldenFileName        string
	}{
		{controlPlaneNamespace, "install-sp_default.golden"},
		{"NAMESPACE", "install-sp_output.golden"},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, "testdata/"+tc.goldenFileName), func(t *testing.T) {
			var buf bytes.Buffer
			err := renderSP(&buf, tc.controlPlaneNamespace)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			diffTestdata(t, tc.goldenFileName, buf.String())
		})
	}
}
