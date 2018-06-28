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
	defaultOptions := newInjectOptions()
	defaultOptions.conduitVersion = "testinjectversion"

	tlsOptions := newInjectOptions()
	tlsOptions.conduitVersion = "testinjectversion"
	tlsOptions.tls = "optional"

	testCases := []struct {
		inputFileName     string
		goldenFileName    string
		testInjectOptions *injectOptions
	}{
		{"inject_emojivoto_deployment.input.yml", "inject_emojivoto_deployment.golden.yml", defaultOptions},
		{"inject_emojivoto_list.input.yml", "inject_emojivoto_list.golden.yml", defaultOptions},
		{"inject_emojivoto_deployment_hostNetwork_false.input.yml", "inject_emojivoto_deployment_hostNetwork_false.golden.yml", defaultOptions},
		{"inject_emojivoto_deployment_hostNetwork_true.input.yml", "inject_emojivoto_deployment_hostNetwork_true.golden.yml", defaultOptions},
		{"inject_emojivoto_deployment_controller_name.input.yml", "inject_emojivoto_deployment_controller_name.golden.yml", defaultOptions},
		{"inject_emojivoto_statefulset.input.yml", "inject_emojivoto_statefulset.golden.yml", defaultOptions},
		{"inject_emojivoto_pod.input.yml", "inject_emojivoto_pod.golden.yml", defaultOptions},
		{"inject_emojivoto_deployment.input.yml", "inject_emojivoto_deployment_tls.golden.yml", tlsOptions},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.inputFileName), func(t *testing.T) {
			file, err := os.Open("testdata/" + tc.inputFileName)
			if err != nil {
				t.Errorf("error opening test input file: %v\n", err)
			}

			read := bufio.NewReader(file)

			output := new(bytes.Buffer)

			err = InjectYAML(read, output, tc.testInjectOptions)
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

func TestRunInjectCmd(t *testing.T) {
	testInjectOptions := newInjectOptions()
	testInjectOptions.conduitVersion = "testinjectversion"
	testCases := []struct {
		inputFileName        string
		stdErrGoldenFileName string
		stdOutGoldenFileName string
		exitCode             int
	}{
		{
			inputFileName:        "inject_gettest_deployment.bad.input.yml",
			stdErrGoldenFileName: "inject_gettest_deployment.bad.golden",
			exitCode:             1,
		},
		{
			inputFileName:        "inject_gettest_deployment.good.input.yml",
			stdOutGoldenFileName: "inject_gettest_deployment.good.golden.yml",
			exitCode:             0,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.inputFileName), func(t *testing.T) {
			errBuffer := &bytes.Buffer{}
			outBuffer := &bytes.Buffer{}

			in, err := os.Open(fmt.Sprintf("testdata/%s", tc.inputFileName))
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			exitCode := runInjectCmd(in, errBuffer, outBuffer, testInjectOptions)
			if exitCode != tc.exitCode {
				t.Fatalf("Expected exit code to be %d but got: %d", tc.exitCode, exitCode)
			}

			actualStdOutResult := outBuffer.String()
			expectedStdOutResult := readOptionalTestFile(t, tc.stdOutGoldenFileName)

			diffCompare(t, actualStdOutResult, expectedStdOutResult)

			actualStdErrResult := errBuffer.String()
			expectedStdErrResult := readOptionalTestFile(t, tc.stdErrGoldenFileName)
			diffCompare(t, actualStdErrResult, expectedStdErrResult)
		})
	}
}
