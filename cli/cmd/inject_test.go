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
}
