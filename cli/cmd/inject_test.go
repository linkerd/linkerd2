package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

type injectYAML struct {
	inputFileName     string
	goldenFileName    string
	reportFileName    string
	testInjectOptions *injectOptions
}

func testUninjectAndInject(t *testing.T, tc injectYAML) {
	file, err := os.Open("testdata/" + tc.inputFileName)
	if err != nil {
		t.Errorf("error opening test input file: %v\n", err)
	}

	read := bufio.NewReader(file)

	output := new(bytes.Buffer)
	report := new(bytes.Buffer)

	if exitCode := uninjectAndInject([]io.Reader{read}, report, output, tc.testInjectOptions); exitCode != 0 {
		t.Errorf("Unexpected error injecting YAML: %v\n", report)
	}

	actualOutput := output.String()
	expectedOutput := readTestdata(t, tc.goldenFileName)
	if actualOutput != expectedOutput {
		writeTestdataIfUpdate(t, tc.goldenFileName, output.Bytes())
		diffCompare(t, actualOutput, expectedOutput)
	}

	actualReport := report.String()
	reportFileName := tc.reportFileName
	if verbose {
		reportFileName += ".verbose"
	}
	expectedReport := readTestdata(t, reportFileName)
	if expectedReport != actualReport {
		writeTestdataIfUpdate(t, reportFileName, report.Bytes())
		diffCompare(t, actualReport, expectedReport)
	}
}

func TestUninjectAndInject(t *testing.T) {
	defaultOptions := newInjectOptions()
	defaultOptions.linkerdVersion = "testinjectversion"

	tlsOptions := newInjectOptions()
	tlsOptions.linkerdVersion = "testinjectversion"
	tlsOptions.tls = "optional"

	proxyRequestOptions := newInjectOptions()
	proxyRequestOptions.linkerdVersion = "testinjectversion"
	proxyRequestOptions.proxyCPURequest = "110m"
	proxyRequestOptions.proxyMemoryRequest = "100Mi"

	noInitContainerOptions := newInjectOptions()
	noInitContainerOptions.linkerdVersion = "testinjectversion"
	noInitContainerOptions.noInitContainer = true

	testCases := []injectYAML{
		{
			inputFileName:     "inject_emojivoto_deployment.input.yml",
			goldenFileName:    "inject_emojivoto_deployment.golden.yml",
			reportFileName:    "inject_emojivoto_deployment.report",
			testInjectOptions: defaultOptions,
		},
		{
			inputFileName:     "inject_emojivoto_list.input.yml",
			goldenFileName:    "inject_emojivoto_list.golden.yml",
			reportFileName:    "inject_emojivoto_list.report",
			testInjectOptions: defaultOptions,
		},
		{
			inputFileName:     "inject_emojivoto_deployment_hostNetwork_false.input.yml",
			goldenFileName:    "inject_emojivoto_deployment_hostNetwork_false.golden.yml",
			reportFileName:    "inject_emojivoto_deployment_hostNetwork_false.report",
			testInjectOptions: defaultOptions,
		},
		{
			inputFileName:     "inject_emojivoto_deployment_hostNetwork_true.input.yml",
			goldenFileName:    "inject_emojivoto_deployment_hostNetwork_true.input.yml",
			reportFileName:    "inject_emojivoto_deployment_hostNetwork_true.report",
			testInjectOptions: defaultOptions,
		},
		{
			inputFileName:     "inject_emojivoto_deployment_injectDisabled.input.yml",
			goldenFileName:    "inject_emojivoto_deployment_injectDisabled.input.yml",
			reportFileName:    "inject_emojivoto_deployment_injectDisabled.report",
			testInjectOptions: defaultOptions,
		},
		{
			inputFileName:     "inject_emojivoto_deployment_controller_name.input.yml",
			goldenFileName:    "inject_emojivoto_deployment_controller_name.golden.yml",
			reportFileName:    "inject_emojivoto_deployment_controller_name.report",
			testInjectOptions: defaultOptions,
		},
		{
			inputFileName:     "inject_emojivoto_statefulset.input.yml",
			goldenFileName:    "inject_emojivoto_statefulset.golden.yml",
			reportFileName:    "inject_emojivoto_statefulset.report",
			testInjectOptions: defaultOptions,
		},
		{
			inputFileName:     "inject_emojivoto_pod.input.yml",
			goldenFileName:    "inject_emojivoto_pod.golden.yml",
			reportFileName:    "inject_emojivoto_pod.report",
			testInjectOptions: defaultOptions,
		},
		{
			inputFileName:     "inject_emojivoto_pod_with_requests.input.yml",
			goldenFileName:    "inject_emojivoto_pod_with_requests.golden.yml",
			reportFileName:    "inject_emojivoto_pod_with_requests.report",
			testInjectOptions: proxyRequestOptions,
		},
		{
			inputFileName:     "inject_emojivoto_deployment.input.yml",
			goldenFileName:    "inject_emojivoto_deployment_tls.golden.yml",
			reportFileName:    "inject_emojivoto_deployment.report",
			testInjectOptions: tlsOptions,
		},
		{
			inputFileName:     "inject_emojivoto_pod.input.yml",
			goldenFileName:    "inject_emojivoto_pod_tls.golden.yml",
			reportFileName:    "inject_emojivoto_pod.report",
			testInjectOptions: tlsOptions,
		},
		{
			inputFileName:     "inject_emojivoto_deployment_udp.input.yml",
			goldenFileName:    "inject_emojivoto_deployment_udp.golden.yml",
			reportFileName:    "inject_emojivoto_deployment_udp.report",
			testInjectOptions: defaultOptions,
		},
		{
			inputFileName:     "inject_emojivoto_already_injected.input.yml",
			goldenFileName:    "inject_emojivoto_already_injected.golden.yml",
			reportFileName:    "inject_emojivoto_already_injected.report",
			testInjectOptions: defaultOptions,
		},
		{
			inputFileName:     "inject_emojivoto_istio.input.yml",
			goldenFileName:    "inject_emojivoto_istio.input.yml",
			reportFileName:    "inject_emojivoto_istio.report",
			testInjectOptions: defaultOptions,
		},
		{
			inputFileName:     "inject_contour.input.yml",
			goldenFileName:    "inject_contour.input.yml",
			reportFileName:    "inject_contour.report",
			testInjectOptions: defaultOptions,
		},
		{
			inputFileName:     "inject_emojivoto_deployment.input.yml",
			goldenFileName:    "inject_emojivoto_deployment_no_init_container.golden.yml",
			reportFileName:    "inject_emojivoto_deployment.report",
			testInjectOptions: noInitContainerOptions,
		},
	}

	for i, tc := range testCases {
		verbose = true
		t.Run(fmt.Sprintf("%d: %s --verbose", i, tc.inputFileName), func(t *testing.T) {
			testUninjectAndInject(t, tc)
		})
		verbose = false
		t.Run(fmt.Sprintf("%d: %s", i, tc.inputFileName), func(t *testing.T) {
			testUninjectAndInject(t, tc)
		})
	}
}

type injectCmd struct {
	inputFileName        string
	stdErrGoldenFileName string
	stdOutGoldenFileName string
	exitCode             int
}

func testInjectCmd(t *testing.T, tc injectCmd) {
	testInjectOptions := newInjectOptions()
	testInjectOptions.linkerdVersion = "testinjectversion"

	errBuffer := &bytes.Buffer{}
	outBuffer := &bytes.Buffer{}

	in, err := os.Open(fmt.Sprintf("testdata/%s", tc.inputFileName))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	exitCode := runInjectCmd([]io.Reader{in}, errBuffer, outBuffer, testInjectOptions)
	if exitCode != tc.exitCode {
		t.Fatalf("Expected exit code to be %d but got: %d", tc.exitCode, exitCode)
	}

	actualStdOutResult := outBuffer.String()
	expectedStdOutResult := readTestdataIfFileName(t, tc.stdOutGoldenFileName)
	if expectedStdOutResult != actualStdOutResult {
		writeTestdataIfUpdate(t, tc.stdOutGoldenFileName, outBuffer.Bytes())
		diffCompare(t, actualStdOutResult, expectedStdOutResult)
	}

	stdErrGoldenFileName := tc.stdErrGoldenFileName
	if verbose {
		stdErrGoldenFileName += ".verbose"
	}

	actualStdErrResult := errBuffer.String()
	expectedStdErrResult := readTestdataIfFileName(t, stdErrGoldenFileName)
	if expectedStdErrResult != actualStdErrResult {
		writeTestdataIfUpdate(t, tc.stdOutGoldenFileName, errBuffer.Bytes())
		diffCompare(t, actualStdErrResult, expectedStdErrResult)
	}
}
func TestRunInjectCmd(t *testing.T) {
	testCases := []injectCmd{
		{
			inputFileName:        "inject_gettest_deployment.bad.input.yml",
			stdErrGoldenFileName: "inject_gettest_deployment.bad.golden",
			exitCode:             1,
		},
		{
			inputFileName:        "inject_gettest_deployment.good.input.yml",
			stdOutGoldenFileName: "inject_gettest_deployment.good.golden.yml",
			stdErrGoldenFileName: "inject_gettest_deployment.good.golden.stderr",
			exitCode:             0,
		},
	}

	for i, tc := range testCases {
		verbose = true
		t.Run(fmt.Sprintf("%d: %s --verbose", i, tc.inputFileName), func(t *testing.T) {
			testInjectCmd(t, tc)
		})
		verbose = false
		t.Run(fmt.Sprintf("%d: %s", i, tc.inputFileName), func(t *testing.T) {
			testInjectCmd(t, tc)
		})
	}
}

type injectFilePath struct {
	resource     string
	resourceFile string
	expectedFile string
	stdErrFile   string
}

func testInjectFilePath(t *testing.T, tc injectFilePath) {
	in, err := read(tc.resourceFile)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	errBuf := &bytes.Buffer{}
	actual := &bytes.Buffer{}
	if exitCode := runInjectCmd(in, errBuf, actual, newInjectOptions()); exitCode != 0 {
		t.Fatal("Unexpected error. Exit code from runInjectCmd: ", exitCode)
	}

	expected := readTestdata(t, tc.expectedFile)
	if expected != actual.String() {
		writeTestdataIfUpdate(t, tc.expectedFile, actual.Bytes())
		diffCompare(t, actual.String(), expected)
	}

	stdErrFile := tc.stdErrFile
	if verbose {
		stdErrFile += ".verbose"
	}

	stdErr := readTestdata(t, stdErrFile)
	if stdErr != errBuf.String() {
		writeTestdataIfUpdate(t, errBuf.Bytes(), stdErr)
		diffCompare(t, errBuf.String(), stdErr)
	}
}

func testReadFromFolder(t *testing.T, resourceFolder string, expectedFolder string) {
	in, err := read(resourceFolder)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	errBuf := &bytes.Buffer{}
	actual := &bytes.Buffer{}
	if exitCode := runInjectCmd(in, errBuf, actual, newInjectOptions()); exitCode != 0 {
		t.Fatal("Unexpected error. Exit code from runInjectCmd: ", exitCode)
	}

	expectedFile := filepath.Join(expectedFolder, "injected_nginx_redis.yaml")
	expected := readTestdata(t, expectedFile)
	if expected != actual.String() {
		writeTestdataIfUpdate(t, actual.Bytes(), expectedFile)
		diffCompare(t, actual.String(), expected)
	}

	stdErrFileName := filepath.Join(expectedFolder, "injected_nginx_redis.stderr")
	if verbose {
		stdErrFileName += ".verbose"
	}

	stdErr := readTestdata(t, stdErrFileName)
	if stdErr != errBuf.String() {
		writeTestdataIfUpdate(t, stdErrFileName, errBuf.Bytes())
		diffCompare(t, errBuf.String(), stdErr)
	}
}

func TestInjectFilePath(t *testing.T) {
	var (
		resourceFolder = filepath.Join("testdata", "inject-filepath", "resources")
		expectedFolder = filepath.Join("inject-filepath", "expected")
	)

	t.Run("read from files", func(t *testing.T) {
		testCases := []injectFilePath{
			{
				resource:     "nginx",
				resourceFile: filepath.Join(resourceFolder, "nginx.yaml"),
				expectedFile: filepath.Join(expectedFolder, "injected_nginx.yaml"),
				stdErrFile:   filepath.Join(expectedFolder, "injected_nginx.stderr"),
			},
			{
				resource:     "redis",
				resourceFile: filepath.Join(resourceFolder, "db/redis.yaml"),
				expectedFile: filepath.Join(expectedFolder, "injected_redis.yaml"),
				stdErrFile:   filepath.Join(expectedFolder, "injected_redis.stderr"),
			},
		}

		for i, testCase := range testCases {
			verbose = true
			t.Run(fmt.Sprintf("%d %s", i, testCase.resource), func(t *testing.T) {
				testInjectFilePath(t, testCase)
			})
			verbose = false
			t.Run(fmt.Sprintf("%d %s", i, testCase.resource), func(t *testing.T) {
				testInjectFilePath(t, testCase)
			})
		}
	})

	verbose = true
	t.Run("read from folder --verbose", func(t *testing.T) {
		testReadFromFolder(t, resourceFolder, expectedFolder)
	})
	verbose = false
	t.Run("read from folder --verbose", func(t *testing.T) {
		testReadFromFolder(t, resourceFolder, expectedFolder)
	})
}

func TestWalk(t *testing.T) {
	// create two data files, one in the root folder and the other in a subfolder.
	// walk should be able to read the content of the two data files recursively.
	var (
		tmpFolderRoot = "linkerd-testdata"
		tmpFolderData = filepath.Join(tmpFolderRoot, "data")
	)

	if err := os.MkdirAll(tmpFolderData, os.ModeDir|os.ModePerm); err != nil {
		t.Fatal("Unexpected error: ", err)
	}
	defer os.RemoveAll(tmpFolderRoot)

	var (
		data  = []byte(readTestdata(t, "inject_gettest_deployment.bad.input.yml"))
		file1 = filepath.Join(tmpFolderRoot, "root.txt")
		file2 = filepath.Join(tmpFolderData, "data.txt")
	)
	if err := ioutil.WriteFile(file1, data, 0666); err != nil {
		t.Fatal("Unexpected error: ", err)
	}
	if err := ioutil.WriteFile(file2, data, 0666); err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	actual, err := walk(tmpFolderRoot)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	for _, r := range actual {
		b := make([]byte, len(data))
		r.Read(b)

		if string(b) != string(data) {
			t.Errorf("Content mismatch. Expected %q, but got %q", data, b)
		}
	}
}
