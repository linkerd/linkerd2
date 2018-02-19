package cmd

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func emptyRun(*cobra.Command, []string) {}
func TestInjectYAML(t *testing.T) {
	t.Run("Run successful conduit inject on valid k8s yaml", func(t *testing.T) {
		file, err := os.Open("testdata/inject_emojivoto_deployment.input.yml")
		if err != nil {
			t.Errorf("error opening test file: %v\n", err)
		}

		read := bufio.NewReader(file)

		output := new(bytes.Buffer)

		err = InjectYAML(read, output)
		if err != nil {
			t.Errorf("Unexpected error injecting YAML: %v\n", err)
		}

		actualOutput := output.String()

		goldenFileBytes, err := ioutil.ReadFile("testdata/inject_emojivoto_deployment.golden.yml")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		expectedOutput := string(goldenFileBytes)
		diffCompare(t, actualOutput, expectedOutput)
	})

	t.Run("Do not print invalid YAML on inject error", func(t *testing.T) {
		rootTestCmd := &cobra.Command{Use: "root", Args: cobra.NoArgs, Run: emptyRun}

		rootTestCmd.AddCommand(injectCmd)

		buf := new(bytes.Buffer)
		rootTestCmd.SetOutput(buf)
		rootTestCmd.SetArgs([]string{"inject", "testdata/inject_gettest_deployment.input"})

		_, err := rootTestCmd.ExecuteC()
		if err == nil {
			t.Fatal("Command returned nil but should have returned error")
		}

		goldenFileBytes, err := ioutil.ReadFile("testdata/inject_gettest_deployment.golden")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		actualOutput := buf.String()

		expectedOutput := string(goldenFileBytes)
		diffCompare(t, strings.TrimSpace(actualOutput), strings.TrimSpace(expectedOutput))
	})
}
