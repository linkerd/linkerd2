package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
)

func TestInjectYAML(t *testing.T) {
	testCases := []struct {
		inputFileName  string
		goldenFileName string
	}{
		{"inject_emojivoto_deployment.input.yml", "inject_emojivoto_deployment.golden.yml"},
		{"inject_emojivoto_deployment_hostNetwork_false.input.yml", "inject_emojivoto_deployment_hostNetwork_false.golden.yml"},
		{"inject_emojivoto_deployment_hostNetwork_true.input.yml", "inject_emojivoto_deployment_hostNetwork_true.golden.yml"},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.inputFileName), func(t *testing.T) {
			file, err := os.Open("testdata/" + tc.inputFileName)
			if err != nil {
				t.Errorf("error opening test input file: %v\n", err)
			}

			read := bufio.NewReader(file)

			output := new(bytes.Buffer)

			err = InjectYAML(read, output)
			if err != nil {
				t.Errorf("Unexpected error injecting YAML: %v\n", err)
			}

			actualOutput := output.String()

			goldenFileBytes, err := ioutil.ReadFile("testdata/" + tc.goldenFileName)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			expectedOutput := string(goldenFileBytes)
			diffCompare(t, actualOutput, expectedOutput)
		})
	}

	t.Run("Do not print invalid YAML on inject error", func(t *testing.T) {
		errBuffer := &bytes.Buffer{}
		outBuffer := &bytes.Buffer{}
		expectedErrorMsg := `Error injecting conduit proxy: error converting YAML to JSON: yaml: line 14: did not find expected key`

		in, err := os.Open("testdata/inject_gettest_deployment.input")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		runInjectCmd(in, errBuffer, outBuffer)

		if len(outBuffer.Bytes()) != 0 {
			t.Fatalf("Expected output buffer to be empty but got: \n%v", outBuffer)
		}

		actualErrorMsg := errBuffer.String()
		diffCompare(t, actualErrorMsg, expectedErrorMsg)
	})
}
