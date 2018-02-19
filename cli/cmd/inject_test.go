package cmd

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"os"
	"testing"
	"fmt"
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
		//file, err := os.Open("testdata/inject_emojivoto_deployment.input.yml")
		//if err != nil {
		//	t.Errorf("error opening test file: %v\n", err)
		//}
		root := &cobra.Command{
			Use:   "test",
			Short: "test",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Println("Running tests...")
				return nil
			},
		}
		root.AddCommand(injectCmd)

		//read := bufio.NewReader(file)

		output := new(bytes.Buffer)
		root.SetOutput(output)
		root.SetArgs([]string{""})
		_, err := root.ExecuteC()
		if err != nil {
			t.Error()
		}
		fmt.Println(output.String())
	})

	t.Run("Test", func(t *testing.T) {
		rootCmd := &cobra.Command{Use: "root", Args: cobra.NoArgs, Run: emptyRun}

		rootCmd.AddCommand(injectCmd)

		buf := new(bytes.Buffer)
		rootCmd.SetOutput(buf)
		rootCmd.SetArgs([]string{"inject", "testdata/inject_gettest_deployment.input"})

		_, err := rootCmd.ExecuteC()
		if err != nil {
			t.Error()
		}
	})
}
