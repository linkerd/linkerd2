package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"
)

func TestDebugSidecar(t *testing.T) {
	//TODO: Add test for non control plane components + other skipped in which we skip injection/uninjection
	testCases := []struct {
		inputFileName  string
		goldenFileName string
		reportFileName string
		inject         bool
		exitCode       int
	}{
		{
			inputFileName:  "debug_inject_linkerd_tap.input.yml",
			goldenFileName: "debug_inject_linkerd_tap.golden.yml",
			reportFileName: "debug_inject_linkerd_tap.report",
			inject:         true,
		},
		{
			inputFileName:  "debug_inject_linkerd_tap.golden.yml",
			goldenFileName: "debug_inject_linkerd_tap.golden.yml",
			reportFileName: "debug_inject_skipped_tap.report",
			inject:         true,
		},
		{
			inputFileName:  "debug_inject_linkerd_tap.golden.yml",
			goldenFileName: "debug_inject_linkerd_tap.input.yml",
			reportFileName: "debug_uninject_linkerd_tap.report",
			inject:         false,
		},
		{
			inputFileName:  "debug_inject_linkerd_tap.input.yml",
			goldenFileName: "debug_inject_linkerd_tap.input.yml",
			reportFileName: "debug_inject_skipped_tap.report",
			inject:         false,
		},
		{
			inputFileName:  "inject_emojivoto_deployment.input.yml",
			reportFileName: "debug_inject_emojivoto_deployment.bad.golden",
			inject:         true,
			exitCode:       1,
		},
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

			transf := &resourceTransformerDebugSidecar{
				configs: testInstallConfig(),
				inject:  tc.inject,
			}

			exitCode := runDebugSidecarCmd(read, report, output, transf)
			if exitCode != tc.exitCode {
				t.Errorf("Unexpected exit code. Got %d but was expecting %d\n", exitCode, tc.exitCode)
			}

			if tc.exitCode == 0 {
				diffTestdata(t, tc.goldenFileName, output.String())
			}
			diffTestdata(t, tc.reportFileName, report.String())
		})
	}

}
