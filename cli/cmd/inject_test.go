package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"k8s.io/api/core/v1"
)

func TestInjectYAML(t *testing.T) {
	testInjectVersion := "testinjectversion"
	testCases := []struct {
		inputFileName  string
		goldenFileName string
	}{
		{"inject_emojivoto_deployment.input.yml", "inject_emojivoto_deployment.golden.yml"},
		{"inject_emojivoto_deployment_hostNetwork_false.input.yml", "inject_emojivoto_deployment_hostNetwork_false.golden.yml"},
		{"inject_emojivoto_deployment_hostNetwork_true.input.yml", "inject_emojivoto_deployment_hostNetwork_true.golden.yml"},
		{"inject_emojivoto_deployment_controller_name.input.yml", "inject_emojivoto_deployment_controller_name.golden.yml"},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.inputFileName), func(t *testing.T) {
			file, err := os.Open("testdata/" + tc.inputFileName)
			if err != nil {
				t.Errorf("error opening test input file: %v\n", err)
			}

			read := bufio.NewReader(file)

			output := new(bytes.Buffer)

			err = InjectYAML(read, output, &proxyConfig{
				version:         testInjectVersion,
				logLevel:        "warn,conduit_proxy=info",
				controlPlaneDNS: "",
				apiPort:         8086,
				controlPort:     4190,
				outboundPort:    4140,
				inboundPort:     4143,
				eventBufferSize: DefaultProxyEventBufferSize,
			})
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
	testInjectVersion := "testinjectversion"
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

			exitCode := runInjectCmd(in, errBuffer, outBuffer, testInjectVersion)
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

func TestMakeProxyEnvVar(t *testing.T) {
	testCases := []struct {
		inputProxyConfig proxyConfig
		expectedEnvVar   v1.EnvVar
	}{
		{
			proxyConfig{eventBufferSize: 0},
			v1.EnvVar{Name: "CONDUIT_PROXY_EVENT_BUFFER_CAPACITY", Value: "0"},
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d:%v", i, tc.inputProxyConfig), func(t *testing.T) {
			envs := makeProxyEnvVar(&tc.inputProxyConfig)
			if !containsEnvVar(envs, tc.expectedEnvVar) {
				t.Fatalf("failed to find expected EnvVar: %v", tc.expectedEnvVar)
			}
		})
	}
}

func containsEnvVar(envs []v1.EnvVar, expectedEnv v1.EnvVar) bool {
	for _, env := range envs {
		if env == expectedEnv {
			return true
		}
	}
	return false
}
