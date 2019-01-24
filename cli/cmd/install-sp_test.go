package cmd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"
)

func TestRenderSP(t *testing.T) {
	testCases := []struct {
		controlPlaneNamespace string
		goldenFileName        string
	}{
		{controlPlaneNamespace, "testdata/install-sp_default.golden"},
		{"NAMESPACE", "testdata/install-sp_output.golden"},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			var buf bytes.Buffer
			err := renderSP(&buf, tc.controlPlaneNamespace)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			content := buf.String()

			goldenFileBytes, err := ioutil.ReadFile(tc.goldenFileName)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			expectedContent := string(goldenFileBytes)
			diffCompare(t, content, expectedContent)
		})
	}
}
