package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/k8s"
)

type testCase struct {
	inputFileName          string
	goldenFileName         string
	reportFileName         string
	injectProxy            bool
	testInjectConfig       *linkerd2.Values
	overrideAnnotations    map[string]string
	enableDebugSidecarFlag bool
}

func mkFilename(filename string, verbose bool) string {
	if verbose {
		return fmt.Sprintf("%s.verbose", filename)
	}
	return filename
}

func testUninjectAndInject(t *testing.T, tc testCase) {
	file, err := os.Open("testdata/" + tc.inputFileName)
	if err != nil {
		t.Errorf("error opening test input file: %v\n", err)
	}

	read := bufio.NewReader(file)

	output := new(bytes.Buffer)
	report := new(bytes.Buffer)
	transformer := &resourceTransformerInject{
		injectProxy:         tc.injectProxy,
		values:              tc.testInjectConfig,
		overrideAnnotations: tc.overrideAnnotations,
		enableDebugSidecar:  tc.enableDebugSidecarFlag,
		allowNsInject:       true,
	}

	if exitCode := uninjectAndInject([]io.Reader{read}, report, output, transformer); exitCode != 0 {
		t.Errorf("Unexpected error injecting YAML: %v\n", report)
	}
	diffTestdata(t, tc.goldenFileName, output.String())

	reportFileName := mkFilename(tc.reportFileName, verbose)
	diffTestdata(t, reportFileName, report.String())
}

func defaultConfig() *linkerd2.Values {
	defaultConfig, err := testInstallValues()
	if err != nil {
		log.Fatalf("Unexpected error: %v", err)
	}
	defaultConfig.Global.LinkerdVersion = "test-inject-control-plane-version"
	defaultConfig.Global.Proxy.Image.Version = "test-inject-proxy-version"
	defaultConfig.DebugContainer.Image.Version = "test-inject-debug-version"

	return defaultConfig
}

func TestUninjectAndInject(t *testing.T) {
	defaultValues, err := testInstallValues()
	if err != nil {
		log.Fatalf("Unexpected error: %v", err)
	}
	defaultValues.Global.LinkerdVersion = "test-inject-control-plane-version"
	defaultValues.Global.Proxy.Image.Version = "test-inject-proxy-version"
	defaultValues.DebugContainer.Image.Version = "test-inject-debug-version"

	overrideConfig, err := testInstallValues()
	if err != nil {
		log.Fatalf("Unexpected error: %v", err)
	}
	overrideConfig.Global.Proxy.Image.Version = "override"

	proxyResourceConfig, err := testInstallValues()
	if err != nil {
		log.Fatalf("Unexpected error: %v", err)
	}
	proxyResourceConfig.Global.Proxy.Image.Version = defaultValues.Global.Proxy.Image.Version
	proxyResourceConfig.Global.Proxy.Resources = &linkerd2.Resources{
		CPU: linkerd2.Constraints{
			Request: "110m",
			Limit:   "160m",
		},
		Memory: linkerd2.Constraints{
			Request: "100Mi",
			Limit:   "150Mi",
		},
	}

	cniEnabledConfig, err := testInstallValues()
	if err != nil {
		log.Fatalf("Unexpected error: %v", err)
	}
	cniEnabledConfig.Global.Proxy.Image.Version = defaultValues.Global.Proxy.Image.Version
	cniEnabledConfig.Global.CNIEnabled = true

	proxyIgnorePortsConfig, err := testInstallValues()
	if err != nil {
		log.Fatalf("Unexpected error: %v", err)
	}
	proxyIgnorePortsConfig.Global.ProxyInit.IgnoreInboundPorts = "22,8100-8102"
	proxyIgnorePortsConfig.Global.ProxyInit.IgnoreOutboundPorts = "5432"

	testCases := []testCase{
		{
			inputFileName:    "inject_emojivoto_deployment.input.yml",
			goldenFileName:   "inject_emojivoto_deployment.golden.yml",
			reportFileName:   "inject_emojivoto_deployment.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_emojivoto_deployment.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_overridden_noinject.golden.yml",
			reportFileName:   "inject_emojivoto_deployment.report",
			injectProxy:      false,
			testInjectConfig: defaultConfig(),
			overrideAnnotations: map[string]string{
				k8s.ProxyAdminPortAnnotation: "1234",
			},
		},
		{
			inputFileName:    "inject_emojivoto_deployment.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_overridden.golden.yml",
			reportFileName:   "inject_emojivoto_deployment.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
			overrideAnnotations: map[string]string{
				k8s.ProxyAdminPortAnnotation: "1234",
			},
		},
		{
			inputFileName:    "inject_emojivoto_list.input.yml",
			goldenFileName:   "inject_emojivoto_list.golden.yml",
			reportFileName:   "inject_emojivoto_list.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_deployment_hostNetwork_false.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_hostNetwork_false.golden.yml",
			reportFileName:   "inject_emojivoto_deployment_hostNetwork_false.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_deployment_capabilities.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_capabilities.golden.yml",
			reportFileName:   "inject_emojivoto_deployment.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_deployment_injectDisabled.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_injectDisabled.input.yml",
			reportFileName:   "inject_emojivoto_deployment_injectDisabled.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_deployment_controller_name.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_controller_name.golden.yml",
			reportFileName:   "inject_emojivoto_deployment_controller_name.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_statefulset.input.yml",
			goldenFileName:   "inject_emojivoto_statefulset.golden.yml",
			reportFileName:   "inject_emojivoto_statefulset.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_cronjob.input.yml",
			goldenFileName:   "inject_emojivoto_cronjob.golden.yml",
			reportFileName:   "inject_emojivoto_cronjob.report",
			injectProxy:      false,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_cronjob_nometa.input.yml",
			goldenFileName:   "inject_emojivoto_cronjob_nometa.golden.yml",
			reportFileName:   "inject_emojivoto_cronjob.report",
			injectProxy:      false,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_pod.input.yml",
			goldenFileName:   "inject_emojivoto_pod.golden.yml",
			reportFileName:   "inject_emojivoto_pod.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_pod_with_requests.input.yml",
			goldenFileName:   "inject_emojivoto_pod_with_requests.golden.yml",
			reportFileName:   "inject_emojivoto_pod_with_requests.report",
			injectProxy:      true,
			testInjectConfig: proxyResourceConfig,
		},
		{
			inputFileName:    "inject_emojivoto_deployment_udp.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_udp.golden.yml",
			reportFileName:   "inject_emojivoto_deployment_udp.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_already_injected.input.yml",
			goldenFileName:   "inject_emojivoto_already_injected.golden.yml",
			reportFileName:   "inject_emojivoto_already_injected.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_contour.input.yml",
			goldenFileName:   "inject_contour.golden.yml",
			reportFileName:   "inject_contour.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_deployment_empty_resources.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_empty_resources.golden.yml",
			reportFileName:   "inject_emojivoto_deployment_empty_resources.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_list_empty_resources.input.yml",
			goldenFileName:   "inject_emojivoto_list_empty_resources.golden.yml",
			reportFileName:   "inject_emojivoto_list_empty_resources.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_deployment.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_no_init_container.golden.yml",
			reportFileName:   "inject_emojivoto_deployment.report",
			injectProxy:      true,
			testInjectConfig: cniEnabledConfig,
		},
		{
			inputFileName:    "inject_emojivoto_deployment_config_overrides.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_config_overrides.golden.yml",
			reportFileName:   "inject_emojivoto_deployment.report",
			injectProxy:      true,
			testInjectConfig: overrideConfig,
		},
		{
			inputFileName:          "inject_emojivoto_deployment.input.yml",
			goldenFileName:         "inject_emojivoto_deployment_debug.golden.yml",
			reportFileName:         "inject_emojivoto_deployment.report",
			injectProxy:            true,
			testInjectConfig:       defaultConfig(),
			enableDebugSidecarFlag: true,
		},
		{
			inputFileName:          "inject_tap_deployment.input.yml",
			goldenFileName:         "inject_tap_deployment_debug.golden.yml",
			reportFileName:         "inject_tap_deployment_debug.report",
			injectProxy:            true,
			testInjectConfig:       defaultConfig(),
			enableDebugSidecarFlag: true,
		},
		{
			inputFileName:    "inject_emojivoto_namespace_good.input.yml",
			goldenFileName:   "inject_emojivoto_namespace_good.golden.yml",
			reportFileName:   "inject_emojivoto_namespace_good.golden.report",
			injectProxy:      false,
			testInjectConfig: defaultConfig(),
		},
		{
			inputFileName:    "inject_emojivoto_namespace_good.input.yml",
			goldenFileName:   "inject_emojivoto_namespace_overidden_good.golden.yml",
			reportFileName:   "inject_emojivoto_namespace_good.golden.report",
			injectProxy:      false,
			testInjectConfig: defaultConfig(),
			overrideAnnotations: map[string]string{
				k8s.IdentityModeAnnotation: "default",
				k8s.CreatedByAnnotation:    "linkerd/cli dev-undefined",
			},
		},
		{
			inputFileName:    "inject_emojivoto_deployment.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_proxyignores.golden.yml",
			reportFileName:   "inject_emojivoto_deployment.report",
			injectProxy:      true,
			testInjectConfig: proxyIgnorePortsConfig,
		},
		{
			inputFileName:    "inject_emojivoto_pod.input.yml",
			goldenFileName:   "inject_emojivoto_pod_proxyignores.golden.yml",
			reportFileName:   "inject_emojivoto_pod.report",
			injectProxy:      true,
			testInjectConfig: proxyIgnorePortsConfig,
		},
		{
			inputFileName:    "inject_emojivoto_deployment.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_trace.golden.yml",
			reportFileName:   "inject_emojivoto_deployment_trace.report",
			injectProxy:      true,
			testInjectConfig: defaultConfig(),
			overrideAnnotations: map[string]string{
				k8s.ProxyTraceCollectorSvcAddrAnnotation:    "linkerd-collector",
				k8s.ProxyTraceCollectorSvcAccountAnnotation: "linkerd-collector.linkerd",
			},
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
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
	injectProxy          bool
}

func testInjectCmd(t *testing.T, tc injectCmd) {
	testConfig, err := testInstallValues()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	testConfig.Global.Proxy.Image.Version = "testinjectversion"

	errBuffer := &bytes.Buffer{}
	outBuffer := &bytes.Buffer{}

	in, err := os.Open(fmt.Sprintf("testdata/%s", tc.inputFileName))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	transformer := &resourceTransformerInject{
		injectProxy: tc.injectProxy,
		values:      testConfig,
	}
	exitCode := runInjectCmd([]io.Reader{in}, errBuffer, outBuffer, transformer)
	if exitCode != tc.exitCode {
		t.Fatalf("Expected exit code to be %d but got: %d", tc.exitCode, exitCode)
	}
	if tc.stdOutGoldenFileName != "" {
		diffTestdata(t, tc.stdOutGoldenFileName, outBuffer.String())
	} else if outBuffer.Len() != 0 {
		t.Fatalf("Expected no standard output, but got: %s", outBuffer)
	}

	stdErrGoldenFileName := mkFilename(tc.stdErrGoldenFileName, verbose)
	diffTestdata(t, stdErrGoldenFileName, errBuffer.String())
}

func TestRunInjectCmd(t *testing.T) {
	testCases := []injectCmd{
		{
			inputFileName:        "inject_gettest_deployment.bad.input.yml",
			stdErrGoldenFileName: "inject_gettest_deployment.bad.golden",
			exitCode:             1,
			injectProxy:          true,
		},
		{
			inputFileName:        "inject_tap_deployment.input.yml",
			stdErrGoldenFileName: "inject_tap_deployment.bad.golden",
			exitCode:             1,
			injectProxy:          false,
		},
		{
			inputFileName:        "inject_gettest_deployment.good.input.yml",
			stdOutGoldenFileName: "inject_gettest_deployment.good.golden.yml",
			stdErrGoldenFileName: "inject_gettest_deployment.good.golden.stderr",
			exitCode:             0,
			injectProxy:          true,
		},
		{
			inputFileName:        "inject_emojivoto_deployment_automountServiceAccountToken_false.input.yml",
			stdOutGoldenFileName: "inject_emojivoto_deployment_automountServiceAccountToken_false.golden.yml",
			stdErrGoldenFileName: "inject_emojivoto_deployment_automountServiceAccountToken_false.golden.stderr",
			exitCode:             1,
			injectProxy:          false,
		},
		{
			inputFileName:        "inject_emojivoto_istio.input.yml",
			stdOutGoldenFileName: "inject_emojivoto_istio.golden.yml",
			stdErrGoldenFileName: "inject_emojivoto_istio.golden.stderr",
			exitCode:             1,
			injectProxy:          true,
		},
		{
			inputFileName:        "inject_emojivoto_deployment_hostNetwork_true.input.yml",
			stdOutGoldenFileName: "inject_emojivoto_deployment_hostNetwork_true.golden.yml",
			stdErrGoldenFileName: "inject_emojivoto_deployment_hostNetwork_true.golden.stderr",
			exitCode:             1,
			injectProxy:          true,
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
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
	in, err := read("testdata/" + tc.resourceFile)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	errBuf := &bytes.Buffer{}
	actual := &bytes.Buffer{}
	values, err := testInstallValues()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	transformer := &resourceTransformerInject{
		injectProxy: true,
		values:      values,
	}
	if exitCode := runInjectCmd(in, errBuf, actual, transformer); exitCode != 0 {
		t.Fatal("Unexpected error. Exit code from runInjectCmd: ", exitCode)
	}
	diffTestdata(t, tc.expectedFile, actual.String())

	stdErrFile := mkFilename(tc.stdErrFile, verbose)
	diffTestdata(t, stdErrFile, errBuf.String())
}

func testReadFromFolder(t *testing.T, resourceFolder string, expectedFolder string) {
	in, err := read("testdata/" + resourceFolder)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	values, err := testInstallValues()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	errBuf := &bytes.Buffer{}
	actual := &bytes.Buffer{}
	transformer := &resourceTransformerInject{
		injectProxy: true,
		values:      values,
	}
	if exitCode := runInjectCmd(in, errBuf, actual, transformer); exitCode != 0 {
		t.Fatal("Unexpected error. Exit code from runInjectCmd: ", exitCode)
	}

	expectedFile := filepath.Join(expectedFolder, "injected_nginx_redis.yaml")
	diffTestdata(t, expectedFile, actual.String())

	stdErrFileName := mkFilename(filepath.Join(expectedFolder, "injected_nginx_redis.stderr"), verbose)
	diffTestdata(t, stdErrFileName, errBuf.String())
}

func TestInjectFilePath(t *testing.T) {
	var (
		resourceFolder = filepath.Join("inject-filepath", "resources")
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
			testCase := testCase // pin
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

func TestValidURL(t *testing.T) {
	// if the string follows a URL pattern, true has to be returned
	// if not false is returned

	tests := map[string]bool{
		"http://www.linkerd.io":  true,
		"https://www.linkerd.io": true,
		"www.linkerd.io/":        false,
		"~/foo/bar.yaml":         false,
		"./foo/bar.yaml":         false,
		"/foo/bar/baz.yml":       false,
		"../foo/bar/baz.yaml":    false,
		"https//":                false,
	}

	for url, expectedValue := range tests {
		value := isValidURL(url)
		if value != expectedValue {
			t.Errorf("Result mismatch for %s. expected %v, but got %v", url, expectedValue, value)
		}
	}

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
	defer func() {
		err := os.RemoveAll(tmpFolderRoot)
		if err != nil {
			t.Errorf("failed to remove temp dir %q: %v", tmpFolderRoot, err)
		}
	}()

	var (
		data  = []byte(readTestdata(t, "inject_gettest_deployment.bad.input.yml"))
		file1 = filepath.Join(tmpFolderRoot, "root.txt")
		file2 = filepath.Join(tmpFolderData, "data.txt")
	)
	if err := ioutil.WriteFile(file1, data, 0644); err != nil {
		t.Fatal("Unexpected error: ", err)
	}
	if err := ioutil.WriteFile(file2, data, 0644); err != nil {
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

func TestOverrideConfigsParameterized(t *testing.T) {

	tests := []struct {
		description       string
		configOptions     proxyConfigOptions
		expectedOverrides map[string]string
	}{
		{
			description: "proxy configuration overrides",
			configOptions: proxyConfigOptions{
				ignoreInboundPorts:       []string{"8500-8505"},
				ignoreOutboundPorts:      []string{"3306"},
				proxyAdminPort:           1234,
				proxyControlPort:         4190,
				proxyInboundPort:         4143,
				proxyOutboundPort:        4140,
				proxyUID:                 999,
				proxyLogLevel:            "debug",
				proxyLogFormat:           "plain",
				disableIdentity:          true,
				disableTap:               true,
				enableExternalProfiles:   true,
				proxyCPURequest:          "10m",
				proxyCPULimit:            "100m",
				proxyMemoryRequest:       "10Mi",
				proxyMemoryLimit:         "50Mi",
				traceCollector:           "oc-collector.tracing:55678",
				traceCollectorSvcAccount: "default",
				waitBeforeExitSeconds:    10,
			},
			expectedOverrides: map[string]string{
				k8s.ProxyIgnoreInboundPortsAnnotation:       "8500-8505",
				k8s.ProxyIgnoreOutboundPortsAnnotation:      "3306",
				k8s.ProxyAdminPortAnnotation:                "1234",
				k8s.ProxyControlPortAnnotation:              "4190",
				k8s.ProxyInboundPortAnnotation:              "4143",
				k8s.ProxyOutboundPortAnnotation:             "4140",
				k8s.ProxyUIDAnnotation:                      "999",
				k8s.ProxyLogLevelAnnotation:                 "debug",
				k8s.ProxyLogFormatAnnotation:                "plain",
				k8s.ProxyDisableIdentityAnnotation:          "true",
				k8s.ProxyDisableTapAnnotation:               "true",
				k8s.ProxyEnableExternalProfilesAnnotation:   "true",
				k8s.ProxyCPURequestAnnotation:               "10m",
				k8s.ProxyCPULimitAnnotation:                 "100m",
				k8s.ProxyMemoryRequestAnnotation:            "10Mi",
				k8s.ProxyMemoryLimitAnnotation:              "50Mi",
				k8s.ProxyTraceCollectorSvcAddrAnnotation:    "oc-collector.tracing:55678",
				k8s.ProxyTraceCollectorSvcAccountAnnotation: "default",
				k8s.ProxyWaitBeforeExitSecondsAnnotation:    "10",
			},
		},
		{
			description: "proxy image overrides",
			configOptions: proxyConfigOptions{
				proxyImage:      "ghcr.io/linkerd/proxy",
				proxyVersion:    "test-proxy-version",
				imagePullPolicy: "IfNotPresent",
			},
			expectedOverrides: map[string]string{
				k8s.ProxyImageAnnotation:           "ghcr.io/linkerd/proxy",
				k8s.ProxyVersionOverrideAnnotation: "test-proxy-version",
				k8s.ProxyImagePullPolicyAnnotation: "IfNotPresent",
			},
		},
		{
			description: "proxy-init image overrides",
			configOptions: proxyConfigOptions{
				initImage:        "ghcr.io/linkerd/proxy-init",
				initImageVersion: "test-proxy-init-version",
				imagePullPolicy:  "IfNotPresent",
			},
			expectedOverrides: map[string]string{
				k8s.ProxyInitImageAnnotation:        "ghcr.io/linkerd/proxy-init",
				k8s.ProxyInitImageVersionAnnotation: "test-proxy-init-version",
				k8s.ProxyImagePullPolicyAnnotation:  "IfNotPresent",
			},
		},
		{
			description: "custom docker registry with proxy and proxy-init",
			configOptions: proxyConfigOptions{
				proxyImage:     "ghcr.io/linkerd/proxy",
				initImage:      "ghcr.io/linkerd/proxy-init",
				dockerRegistry: "my.custom.registry/linkerd-io",
			},
			expectedOverrides: map[string]string{
				k8s.ProxyImageAnnotation:     "ghcr.io/linkerd/proxy",
				k8s.ProxyInitImageAnnotation: "ghcr.io/linkerd/proxy-init",
				k8s.DebugImageAnnotation:     "my.custom.registry/linkerd-io/debug",
			},
		},
		{
			description: "custom docker registry",
			configOptions: proxyConfigOptions{
				dockerRegistry: "my.custom.registry/linkerd-io",
			},
			expectedOverrides: map[string]string{
				k8s.ProxyImageAnnotation:     "my.custom.registry/linkerd-io/proxy",
				k8s.ProxyInitImageAnnotation: "my.custom.registry/linkerd-io/proxy-init",
				k8s.DebugImageAnnotation:     "my.custom.registry/linkerd-io/debug",
			},
		},
		{
			description:       "no overrides",
			configOptions:     proxyConfigOptions{},
			expectedOverrides: map[string]string{},
		},
	}

	for _, tt := range tests {
		tt := tt // pin
		t.Run(tt.description, func(t *testing.T) {
			defaultConfig, err := testInstallValues()
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			actualOverrides := map[string]string{}
			tt.configOptions.overrideConfigs(defaultConfig, actualOverrides)
			if len(tt.expectedOverrides) != len(actualOverrides) {
				t.Fatalf("expected %d annotation(s), but received %d", len(tt.expectedOverrides), len(actualOverrides))
			}
			for key, expected := range tt.expectedOverrides {
				actual := actualOverrides[key]
				if actual != expected {
					t.Fatalf("expected annotation %q with %q, but got %q", key, expected, actual)
				}
			}
		})
	}
}

func TestOverrideConfigsWithCustomRegistryInstall(t *testing.T) {

	tests := []struct {
		description       string
		configOptions     proxyConfigOptions
		expectedOverrides map[string]string
	}{
		{
			description: "proxy image overrides",
			configOptions: proxyConfigOptions{
				proxyImage:      "ghcr.io/linkerd/proxy",
				proxyVersion:    "test-proxy-version",
				imagePullPolicy: "IfNotPresent",
			},
			expectedOverrides: map[string]string{
				k8s.ProxyImageAnnotation:           "ghcr.io/linkerd/proxy",
				k8s.ProxyVersionOverrideAnnotation: "test-proxy-version",
				k8s.ProxyImagePullPolicyAnnotation: "IfNotPresent",
			},
		},
		{
			description: "proxy-init image overrides",
			configOptions: proxyConfigOptions{
				initImage:        "ghcr.io/linkerd/proxy-init",
				initImageVersion: "test-proxy-init-version",
				imagePullPolicy:  "IfNotPresent",
			},
			expectedOverrides: map[string]string{
				k8s.ProxyInitImageAnnotation:        "ghcr.io/linkerd/proxy-init",
				k8s.ProxyInitImageVersionAnnotation: "test-proxy-init-version",
				k8s.ProxyImagePullPolicyAnnotation:  "IfNotPresent",
			},
		},
		{
			description: "custom docker registry with proxy and proxy-init",
			configOptions: proxyConfigOptions{
				proxyImage:     "ghcr.io/linkerd/proxy",
				initImage:      "ghcr.io/linkerd/proxy-init",
				dockerRegistry: "my.custom.registry/linkerd-io",
			},
			expectedOverrides: map[string]string{
				k8s.ProxyImageAnnotation:     "ghcr.io/linkerd/proxy",
				k8s.ProxyInitImageAnnotation: "ghcr.io/linkerd/proxy-init",
				k8s.DebugImageAnnotation:     "my.custom.registry/linkerd-io/debug",
			},
		},
		{
			description: "custom docker registry",
			configOptions: proxyConfigOptions{
				dockerRegistry: "my.custom.registry/linkerd-io",
			},
			expectedOverrides: map[string]string{
				k8s.ProxyImageAnnotation:     "my.custom.registry/linkerd-io/proxy",
				k8s.ProxyInitImageAnnotation: "my.custom.registry/linkerd-io/proxy-init",
				k8s.DebugImageAnnotation:     "my.custom.registry/linkerd-io/debug",
			},
		},
		{
			description:       "no overrides",
			configOptions:     proxyConfigOptions{},
			expectedOverrides: map[string]string{},
		},
	}

	// Setup the registry used when "installing" linkerd
	customRegistryAtInstall := "custom.install.registry/linkerd-io"

	for _, tt := range tests {
		tt := tt // pin
		t.Run(tt.description, func(t *testing.T) {

			defaultConfig, err := testInstallValues()
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defaultConfig.Global.Proxy.Image.Name = customRegistryAtInstall + "/proxy"
			defaultConfig.Global.ProxyInit.Image.Name = customRegistryAtInstall + "/proxy-init"
			defaultConfig.DebugContainer.Image.Name = customRegistryAtInstall + "/debug"

			actualOverrides := map[string]string{}
			tt.configOptions.overrideConfigs(defaultConfig, actualOverrides)
			if len(tt.expectedOverrides) != len(actualOverrides) {
				t.Fatalf("expected %d annotation(s), but received %d", len(tt.expectedOverrides), len(actualOverrides))
			}
			for key, expected := range tt.expectedOverrides {
				actual := actualOverrides[key]
				if actual != expected {
					t.Fatalf("expected annotation %q with %q, but got %q", key, expected, actual)
				}
			}
		})
	}
}

func TestOverwriteRegistry(t *testing.T) {
	testCases := []struct {
		image    string
		registry string
		expected string
	}{
		{
			image:    "ghcr.io/linkerd/image",
			registry: "my.custom.registry",
			expected: "my.custom.registry/image",
		},
		{
			image:    "ghcr.io/linkerd/image",
			registry: "my.custom.registry/",
			expected: "my.custom.registry/image",
		},
		{
			image:    "my.custom.registry/image",
			registry: "my.custom.registry",
			expected: "my.custom.registry/image",
		},
		{
			image:    "my.custom.registry/image",
			registry: "ghcr.io/linkerd",
			expected: "ghcr.io/linkerd/image",
		},
		{
			image:    "",
			registry: "my.custom.registry",
			expected: "",
		},
		{
			image:    "ghcr.io/linkerd/image",
			registry: "",
			expected: "image",
		},
		{
			image:    "image",
			registry: "ghcr.io/linkerd",
			expected: "ghcr.io/linkerd/image",
		},
	}
	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			actual := overwriteRegistry(tc.image, tc.registry)
			if actual != tc.expected {
				t.Fatalf("expected %q, but got %q", tc.expected, actual)
			}
		})
	}
}
