package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/linkerd/linkerd2/pkg/healthcheck"
)

func TestCheckStatus(t *testing.T) {
	t.Run("Prints expected output", func(t *testing.T) {
		hc := healthcheck.NewHealthChecker(
			[]healthcheck.CategoryID{},
			&healthcheck.Options{},
		)
		hc.AppendCategories(healthcheck.NewCategory("category", []healthcheck.Checker{
			*healthcheck.NewChecker("check1").
				WithCheck(func(context.Context) error {
					return nil
				}),
			*healthcheck.NewChecker("check2").
				WithHintAnchor("hint-anchor").
				WithCheck(func(context.Context) error {
					return fmt.Errorf("This should contain instructions for fail")
				}),
		},
			true,
		))

		output := bytes.NewBufferString("")
		healthcheck.RunChecks(output, stderr, hc, tableOutput)

		goldenFileBytes, err := ioutil.ReadFile("testdata/check_output.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedContent := string(goldenFileBytes)

		if expectedContent != output.String() {
			t.Fatalf("Expected function to render:\n%s\bbut got:\n%s", expectedContent, output)
		}
	})

	t.Run("Prints expected output in json", func(t *testing.T) {
		hc := healthcheck.NewHealthChecker(
			[]healthcheck.CategoryID{},
			&healthcheck.Options{},
		)
		hc.AppendCategories(healthcheck.NewCategory("category", []healthcheck.Checker{
			*healthcheck.NewChecker("check1").
				WithCheck(func(context.Context) error {
					return nil
				}),
			*healthcheck.NewChecker("check2").
				WithHintAnchor("hint-anchor").
				WithCheck(func(context.Context) error {
					return fmt.Errorf("This should contain instructions for fail")
				}),
		},
			true,
		))

		output := bytes.NewBufferString("")
		healthcheck.RunChecks(output, stderr, hc, jsonOutput)

		goldenFileBytes, err := ioutil.ReadFile("testdata/check_output_json.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expectedContent := string(goldenFileBytes)

		if expectedContent != output.String() {
			t.Fatalf("Expected function to render:\n%s\bbut got:\n%s", expectedContent, output)
		}
	})
}
