package cmd

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"os"
	"testing"
)

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
}
