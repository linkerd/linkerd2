package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"

	charts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
)

// TestUninjectYAML does the reverse of TestInjectYAML.
// We use as input the same "golden" file and as expected output the same "input" file as in the inject tests.
func TestUninjectYAML(t *testing.T) {

	testCases := []struct {
		inputFileName  string
		goldenFileName string
		reportFileName string
	}{
		{
			inputFileName:  "inject_emojivoto_deployment.golden.yml",
			goldenFileName: "inject_emojivoto_deployment.input.yml",
			reportFileName: "inject_emojivoto_deployment_uninject.report",
		},
		{
			// remove all the linkerd.io/* annotations
			inputFileName:  "inject_emojivoto_deployment_overridden_noinject.golden.yml",
			goldenFileName: "inject_emojivoto_deployment_uninjected.input.yml",
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
			inputFileName:  "inject_contour.golden.yml",
			goldenFileName: "inject_contour.input.yml",
			reportFileName: "inject_contour_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_deployment_config_overrides.golden.yml",
			goldenFileName: "inject_emojivoto_deployment_config_overrides.input.yml",
			reportFileName: "inject_emojivoto_deployment_uninject.report",
		},
		{
			inputFileName:  "inject_emojivoto_namespace_good.golden.yml",
			goldenFileName: "inject_emojivoto_namespace_uninjected_good.golden.yml",
			reportFileName: "inject_emojivoto_namespace_uninjected_good.golden.report",
		},
		{
			inputFileName:  "inject_emojivoto_namespace_overidden_good.golden.yml",
			goldenFileName: "inject_emojivoto_namespace_uninjected_good.golden.yml",
			reportFileName: "inject_emojivoto_namespace_uninjected_good.golden.report",
		},
	}

	values, err := charts.NewValues()
	if err != nil {
		t.Fatal(err)
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.inputFileName), func(t *testing.T) {
			file, err := os.Open("testdata/" + tc.inputFileName)
			if err != nil {
				t.Errorf("error opening test input file: %v\n", err)
			}

			read := []io.Reader{bufio.NewReader(file)}

			output := new(bytes.Buffer)
			report := new(bytes.Buffer)

			exitCode := runUninjectCmd(read, report, output, values)
			if exitCode != 0 {
				t.Errorf("Failed to uninject %s\n", tc.inputFileName)
			}

			testDataDiffer.DiffTestdata(t, tc.goldenFileName, output.String())
			testDataDiffer.DiffTestdata(t, tc.reportFileName, report.String())
		})
	}
}
