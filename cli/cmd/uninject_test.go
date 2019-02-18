package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestUninjectYAML does the reverse of TestInjectYAML.
// We use as input the same "golden" file and as expected output the same "input" file as in the inject tests.
func TestUninjectYAML(t *testing.T) {

	testCases := []struct {
		inputFileName     string
		goldenFileName    string
		reportFileName    string
		testInjectOptions *injectOptions
	}{
		{
			inputFileName:  "inject_emojivoto_deployment.golden.yml",
			goldenFileName: "inject_emojivoto_deployment.input.yml",
			reportFileName: "inject_emojivoto_deployment_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_list.golden.yml",
			goldenFileName: "inject_emojivoto_list.input.yml",
			reportFileName: "inject_emojivoto_list_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_deployment_hostNetwork_true.input.yml",
			goldenFileName: "inject_emojivoto_deployment_hostNetwork_true.input.yml",
			reportFileName: "inject_emojivoto_deployment_hostNetwork_true_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_deployment_injectDisabled.input.yml",
			goldenFileName: "inject_emojivoto_deployment_injectDisabled.input.yml",
			reportFileName: "inject_emojivoto_deployment_injectDisabled_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_deployment_controller_name.golden.yml",
			goldenFileName: "inject_emojivoto_deployment_controller_name.input.yml",
			reportFileName: "inject_emojivoto_deployment_controller_name_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_statefulset.golden.yml",
			goldenFileName: "inject_emojivoto_statefulset.input.yml",
			reportFileName: "inject_emojivoto_statefulset_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_pod.golden.yml",
			goldenFileName: "inject_emojivoto_pod.input.yml",
			reportFileName: "inject_emojivoto_pod_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_pod_with_requests.golden.yml",
			goldenFileName: "inject_emojivoto_pod_with_requests.input.yml",
			reportFileName: "inject_emojivoto_pod_with_requests_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_deployment_tls.golden.yml",
			goldenFileName: "inject_emojivoto_deployment.input.yml",
			reportFileName: "inject_emojivoto_deployment_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_pod_tls.golden.yml",
			goldenFileName: "inject_emojivoto_pod.input.yml",
			reportFileName: "inject_emojivoto_pod_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_deployment_udp.golden.yml",
			goldenFileName: "inject_emojivoto_deployment_udp.input.yml",
			reportFileName: "inject_emojivoto_deployment_udp_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_istio.input.yml",
			goldenFileName: "inject_emojivoto_istio.input.yml",
			reportFileName: "inject_emojivoto_istio_uninject.report",
		},
		{
			inputFileName:  "inject_contour.input.yml",
			goldenFileName: "inject_contour.input.yml",
			reportFileName: "inject_contour_uninject.report",
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.inputFileName), func(t *testing.T) {
			file, err := os.Open("testdata/" + tc.inputFileName)
			if err != nil {
				t.Errorf("error opening test input file: %v\n", err)
			}

			read := bufio.NewReader(file)

			output := new(bytes.Buffer)
			report := new(bytes.Buffer)

			err = UninjectYAML(read, output, report, nil)
			if err != nil {
				t.Errorf("Unexpected error uninjecting YAML: %v\n", err)
			}

			actualOutput := stripDashes(output.String())
			expectedOutput := stripDashes(readTestdata(t, tc.goldenFileName))
			if expectedOutput != actualOutput {
				writeTestdataIfUpdate(t, tc.goldenFileName, output.Bytes())
				diffCompare(t, expectedOutput, actualOutput)
			}

			actualReport := report.String()
			expectedReport := readTestdata(t, tc.reportFileName)
			if expectedReport != actualReport {
				writeTestdataIfUpdate(t, tc.reportFileName, report.Bytes())
				diffCompare(t, expectedReport, actualReport)
			}
		})
	}
}

// stripDashes removes the YAML dashes (---) found at the beginning and ending of the
// input and golden files respectively.
func stripDashes(str string) string {
	return strings.Trim(str, "-\n")
}
