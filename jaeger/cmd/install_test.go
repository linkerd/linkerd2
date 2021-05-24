package cmd

import (
	"bytes"
	"fmt"
	"testing"
)

func TestRender(t *testing.T) {

	testCases := []struct {
		values         map[string]interface{}
		goldenFileName string
	}{
		{
			nil,
			"install_default.golden",
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			var buf bytes.Buffer
			// Merge overrides with default
			if err := render(&buf, tc.values); err != nil {
				t.Fatalf("Failed to render templates: %v", err)
			}
			testDataDiffer.DiffTestdata(t, tc.goldenFileName, buf.String())
		})
	}
}
