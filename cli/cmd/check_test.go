package cmd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
)

func TestCheckStatus(t *testing.T) {
	t.Run("Prints expected output", func(t *testing.T) {
		hc := healthcheck.NewHealthChecker(
			[]healthcheck.Checks{},
			&healthcheck.Options{},
		)
		hc.Add("category", "check1", func() error {
			return nil
		})
		hc.Add("category", "check2", func() error {
			return fmt.Errorf("This should contain instructions for fail")
		})

		output := bytes.NewBufferString("")
		runChecks(output, hc)

		goldenFileBytes, err := ioutil.ReadFile("testdata/check_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedContent := string(goldenFileBytes)

		if expectedContent != output.String() {
			t.Fatalf("Expected function to render:\n%s\bbut got:\n%s", expectedContent, output)
		}
	})
}
