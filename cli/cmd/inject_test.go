package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/cmd"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/testutil"
)

type testCase struct {
	inputFileName          string
	goldenFileName         string
	reportFileName         string
	injectProxy            bool
	testInjectConfig       *linkerd2.Values
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
		overrideAnnotations: getOverrideAnnotations(tc.testInjectConfig, defaultConfig()),
		enableDebugSidecar:  tc.enableDebugSidecarFlag,
		allowNsInject:       true,
	}

	if exitCode := uninjectAndInject([]io.Reader{read}, report, output, transformer); exitCode != 0 {
		t.Errorf("Unexpected error injecting YAML: %v", report)
	}
	if err := testDataDiffer.DiffTestYAML(tc.goldenFileName, output.String()); err != nil {
		t.Error(err)
	}

	reportFileName := mkFilename(tc.reportFileName, verbose)
	testDataDiffer.DiffTestdata(t, reportFileName, report.String())
}

func defaultConfig() *linkerd2.Values {
	defaultConfig, err := testInstallValues()
	if err != nil {
		log.Fatalf("Unexpected error: %v", err)
	}
	defaultConfig.LinkerdVersion = "test-inject-control-plane-version"
	defaultConfig.Proxy.Image.Version = "test-inject-proxy-version"
	defaultConfig.DebugContainer.Image.Version = "test-inject-debug-version"

	return defaultConfig
}

func TestUninjectAndInject(t *testing.T) {
	defaultValues := defaultConfig()

	overrideConfig := defaultConfig()
	overrideConfig.Proxy.Image.Version = "override"

	proxyResourceConfig := defaultConfig()
	proxyResourceConfig.Proxy.Resources = &linkerd2.Resources{
		CPU: linkerd2.Constraints{
			Request: "110m",
			Limit:   "160m",
		},
		Memory: linkerd2.Constraints{
			Request: "100Mi",
			Limit:   "150Mi",
		},
	}

	cniEnabledConfig := defaultConfig()
	cniEnabledConfig.CNIEnabled = true

	opaquePortsConfig := defaultConfig()
	opaquePortsConfig.Proxy.OpaquePorts = "3000,5000-6000,mysql"

	ingressConfig := defaultConfig()
	ingressConfig.Proxy.IsIngress = true

	proxyIgnorePortsConfig := defaultConfig()
	proxyIgnorePortsConfig.ProxyInit.IgnoreInboundPorts = "22,8100-8102"
	proxyIgnorePortsConfig.ProxyInit.IgnoreOutboundPorts = "5432"

	testCases := []testCase{
		{
			inputFileName:    "inject_emojivoto_deployment.input.yml",
			goldenFileName:   "inject_emojivoto_deployment.golden.yml",
			reportFileName:   "inject_emojivoto_deployment.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:  "inject_emojivoto_deployment.input.yml",
			goldenFileName: "inject_emojivoto_deployment_overridden_noinject.golden.yml",
			reportFileName: "inject_emojivoto_deployment.report",
			injectProxy:    false,
			testInjectConfig: func() *linkerd2.Values {
				values := defaultConfig()
				values.Proxy.Ports.Admin = 1234
				return values
			}(),
		},
		{
			inputFileName:  "inject_emojivoto_deployment.input.yml",
			goldenFileName: "inject_emojivoto_deployment_overridden.golden.yml",
			reportFileName: "inject_emojivoto_deployment.report",
			injectProxy:    true,
			testInjectConfig: func() *linkerd2.Values {
				values := defaultConfig()
				values.Proxy.Ports.Admin = 1234
				return values
			}(),
		},
		{
			inputFileName:  "inject_emojivoto_deployment.input.yml",
			goldenFileName: "inject_emojivoto_deployment_access_log.golden.yml",
			reportFileName: "inject_emojivoto_deployment.report",
			injectProxy:    true,
			testInjectConfig: func() *linkerd2.Values {
				values := defaultConfig()
				values.Proxy.AccessLog = "apache"
				return values
			}(),
		},
		{
			inputFileName:    "inject_emojivoto_list.input.yml",
			goldenFileName:   "inject_emojivoto_list.golden.yml",
			reportFileName:   "inject_emojivoto_list.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_emojivoto_deployment_hostNetwork_false.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_hostNetwork_false.golden.yml",
			reportFileName:   "inject_emojivoto_deployment_hostNetwork_false.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_emojivoto_deployment_capabilities.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_capabilities.golden.yml",
			reportFileName:   "inject_emojivoto_deployment.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_emojivoto_deployment_injectDisabled.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_injectDisabled.input.yml",
			reportFileName:   "inject_emojivoto_deployment_injectDisabled.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_emojivoto_deployment_controller_name.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_controller_name.golden.yml",
			reportFileName:   "inject_emojivoto_deployment_controller_name.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_emojivoto_statefulset.input.yml",
			goldenFileName:   "inject_emojivoto_statefulset.golden.yml",
			reportFileName:   "inject_emojivoto_statefulset.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_emojivoto_cronjob.input.yml",
			goldenFileName:   "inject_emojivoto_cronjob.golden.yml",
			reportFileName:   "inject_emojivoto_cronjob.report",
			injectProxy:      false,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_emojivoto_cronjob_nometa.input.yml",
			goldenFileName:   "inject_emojivoto_cronjob_nometa.golden.yml",
			reportFileName:   "inject_emojivoto_cronjob.report",
			injectProxy:      false,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_emojivoto_pod.input.yml",
			goldenFileName:   "inject_emojivoto_pod.golden.yml",
			reportFileName:   "inject_emojivoto_pod.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
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
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_emojivoto_already_injected.input.yml",
			goldenFileName:   "inject_emojivoto_already_injected.golden.yml",
			reportFileName:   "inject_emojivoto_already_injected.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_contour.input.yml",
			goldenFileName:   "inject_contour.golden.yml",
			reportFileName:   "inject_contour.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_emojivoto_deployment_empty_resources.input.yml",
			goldenFileName:   "inject_emojivoto_deployment_empty_resources.golden.yml",
			reportFileName:   "inject_emojivoto_deployment_empty_resources.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
		},
		{
			inputFileName:    "inject_emojivoto_list_empty_resources.input.yml",
			goldenFileName:   "inject_emojivoto_list_empty_resources.golden.yml",
			reportFileName:   "inject_emojivoto_list_empty_resources.report",
			injectProxy:      true,
			testInjectConfig: defaultValues,
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
			testInjectConfig:       defaultValues,
			enableDebugSidecarFlag: true,
		},
		{
			inputFileName:          "inject_tap_deployment.input.yml",
			goldenFileName:         "inject_tap_deployment_debug.golden.yml",
			reportFileName:         "inject_tap_deployment_debug.report",
			injectProxy:            true,
			testInjectConfig:       defaultValues,
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
			goldenFileName:   "inject_emojivoto_deployment_opaque_ports.golden.yml",
			reportFileName:   "inject_emojivoto_deployment_opaque_ports.report",
			injectProxy:      true,
			testInjectConfig: opaquePortsConfig,
		},
		{
			inputFileName:    "inject_emojivoto_pod.input.yml",
			goldenFileName:   "inject_emojivoto_pod_ingress.golden.yml",
			reportFileName:   "inject_emojivoto_pod_ingress.report",
			injectProxy:      true,
			testInjectConfig: ingressConfig,
		},
		{
			inputFileName:  "inject_emojivoto_deployment.input.yml",
			goldenFileName: "inject_emojivoto_deployment_default_inbound_policy.golden.yml",
			reportFileName: "inject_emojivoto_deployment_default_inbound_policy.golden.report",
			injectProxy:    false,
			testInjectConfig: func() *linkerd2.Values {
				values := defaultConfig()
				values.Proxy.DefaultInboundPolicy = k8s.AllAuthenticated
				return values
			}(),
		},
		{
			inputFileName:  "inject_emojivoto_pod.input.yml",
			goldenFileName: "inject_emojivoto_pod_default_inbound_policy.golden.yml",
			reportFileName: "inject_emojivoto_pod_default_inbound_policy.golden.report",
			injectProxy:    false,
			testInjectConfig: func() *linkerd2.Values {
				values := defaultConfig()
				values.Proxy.DefaultInboundPolicy = k8s.AllAuthenticated
				return values
			}(),
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
	values               *linkerd2.Values
}

func testInjectCmd(t *testing.T, tc injectCmd) {
	testConfig := tc.values
	if testConfig == nil {
		var err error
		testConfig, err = testInstallValues()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	}
	testConfig.Proxy.Image.Version = "testinjectversion"

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
		testDataDiffer.DiffTestdata(t, tc.stdOutGoldenFileName, outBuffer.String())
	} else if outBuffer.Len() != 0 {
		t.Fatalf("Expected no standard output, but got: %s", outBuffer)
	}

	stdErrGoldenFileName := mkFilename(tc.stdErrGoldenFileName, verbose)
	testDataDiffer.DiffTestdata(t, stdErrGoldenFileName, errBuffer.String())
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
			exitCode:             0,
			injectProxy:          true,
		},
		{
			inputFileName:        "inject_emojivoto_deployment_automountServiceAccountToken_false.input.yml",
			stdOutGoldenFileName: "inject_emojivoto_deployment_automountServiceAccountToken_false_volumeProjection_disabled.golden.yml",
			stdErrGoldenFileName: "inject_emojivoto_deployment_automountServiceAccountToken_false_volumeProjection_disabled.golden.stderr",
			exitCode:             1,
			injectProxy:          false,
			values: func() *linkerd2.Values {
				values, _ := testInstallValues()
				values.Identity.ServiceAccountTokenProjection = false
				return values
			}(),
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
	if err := testDataDiffer.DiffTestYAML(tc.expectedFile, actual.String()); err != nil {
		t.Error(err)
	}

	stdErrFile := mkFilename(tc.stdErrFile, verbose)
	testDataDiffer.DiffTestdata(t, stdErrFile, errBuf.String())
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
	if err := testDataDiffer.DiffTestYAML(expectedFile, actual.String()); err != nil {
		t.Error(err)
	}

	stdErrFileName := mkFilename(filepath.Join(expectedFolder, "injected_nginx_redis.stderr"), verbose)
	testDataDiffer.DiffTestdata(t, stdErrFileName, errBuf.String())
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

func TestToURL(t *testing.T) {
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
		_, ok := toURL(url)
		if ok != expectedValue {
			t.Errorf("Result mismatch for %s. expected %v, but got %v", url, expectedValue, ok)
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
		data  = []byte(testutil.ReadTestdata("inject_gettest_deployment.bad.input.yml"))
		file1 = filepath.Join(tmpFolderRoot, "root.txt")
		file2 = filepath.Join(tmpFolderData, "data.txt")
	)
	if err := os.WriteFile(file1, data, 0600); err != nil {
		t.Fatal("Unexpected error: ", err)
	}
	if err := os.WriteFile(file2, data, 0600); err != nil {
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

func TestProxyConfigurationAnnotations(t *testing.T) {
	baseValues, err := linkerd2.NewValues()
	if err != nil {
		t.Fatal(err)
	}
	values, err := baseValues.DeepCopy()
	if err != nil {
		t.Fatal(err)
	}
	values.ProxyInit.IgnoreInboundPorts = "8500-8505"
	values.ProxyInit.IgnoreOutboundPorts = "3306"
	values.Proxy.Ports.Admin = 1234
	values.Proxy.Ports.Control = 4191
	values.Proxy.Ports.Inbound = 4144
	values.Proxy.Ports.Outbound = 4141
	values.Proxy.UID = 999
	values.Proxy.LogLevel = "debug"
	values.Proxy.LogFormat = "cool"
	values.Proxy.EnableExternalProfiles = true
	values.Proxy.Resources.CPU.Request = "10m"
	values.Proxy.Resources.CPU.Limit = "100m"
	values.Proxy.Resources.Memory.Request = "10Mi"
	values.Proxy.Resources.Memory.Limit = "50Mi"
	values.Proxy.WaitBeforeExitSeconds = 10
	values.Proxy.Await = false
	values.Proxy.AccessLog = "apache"
	values.Proxy.ShutdownGracePeriod = "60s"

	expectedOverrides := map[string]string{
		k8s.ProxyIgnoreInboundPortsAnnotation:  "8500-8505",
		k8s.ProxyIgnoreOutboundPortsAnnotation: "3306",
		k8s.ProxyAdminPortAnnotation:           "1234",
		k8s.ProxyControlPortAnnotation:         "4191",
		k8s.ProxyInboundPortAnnotation:         "4144",
		k8s.ProxyOutboundPortAnnotation:        "4141",
		k8s.ProxyUIDAnnotation:                 "999",
		k8s.ProxyLogLevelAnnotation:            "debug",
		k8s.ProxyLogFormatAnnotation:           "cool",

		k8s.ProxyEnableExternalProfilesAnnotation: "true",
		k8s.ProxyCPURequestAnnotation:             "10m",
		k8s.ProxyCPULimitAnnotation:               "100m",
		k8s.ProxyMemoryRequestAnnotation:          "10Mi",
		k8s.ProxyMemoryLimitAnnotation:            "50Mi",
		k8s.ProxyWaitBeforeExitSecondsAnnotation:  "10",
		k8s.ProxyAwait:                            "disabled",
		k8s.ProxyAccessLogAnnotation:              "apache",
		k8s.ProxyShutdownGracePeriodAnnotation:    "60s",
	}

	overrides := getOverrideAnnotations(values, baseValues)

	diffOverrides(t, expectedOverrides, overrides)
}

func TestProxyImageAnnotations(t *testing.T) {
	baseValues, err := linkerd2.NewValues()
	if err != nil {
		t.Fatal(err)
	}
	values, err := baseValues.DeepCopy()
	if err != nil {
		t.Fatal(err)
	}
	values.Proxy.Image = &linkerd2.Image{
		Name:       "my.registry/linkerd/proxy",
		Version:    "test-proxy-version",
		PullPolicy: "Always",
	}

	expectedOverrides := map[string]string{
		k8s.ProxyImageAnnotation:           "my.registry/linkerd/proxy",
		k8s.ProxyVersionOverrideAnnotation: "test-proxy-version",
		k8s.ProxyImagePullPolicyAnnotation: "Always",
	}

	overrides := getOverrideAnnotations(values, baseValues)

	diffOverrides(t, expectedOverrides, overrides)
}

func TestProxyInitImageAnnotations(t *testing.T) {
	baseValues, err := linkerd2.NewValues()
	if err != nil {
		t.Fatal(err)
	}
	values, err := baseValues.DeepCopy()
	if err != nil {
		t.Fatal(err)
	}
	values.ProxyInit.Image = &linkerd2.Image{
		Name:    "my.registry/linkerd/proxy-init",
		Version: "test-proxy-init-version",
	}

	expectedOverrides := map[string]string{
		k8s.ProxyInitImageAnnotation:        "my.registry/linkerd/proxy-init",
		k8s.ProxyInitImageVersionAnnotation: "test-proxy-init-version",
	}

	overrides := getOverrideAnnotations(values, baseValues)

	diffOverrides(t, expectedOverrides, overrides)
}

func TestNoAnnotations(t *testing.T) {
	baseValues, err := linkerd2.NewValues()
	if err != nil {
		t.Fatal(err)
	}
	values, err := baseValues.DeepCopy()
	if err != nil {
		t.Fatal(err)
	}

	expectedOverrides := map[string]string{}

	overrides := getOverrideAnnotations(values, baseValues)

	diffOverrides(t, expectedOverrides, overrides)
}

func TestOverwriteRegistry(t *testing.T) {
	testCases := []struct {
		image    string
		registry string
		expected string
	}{
		{
			image:    "cr.l5d.io/linkerd/image",
			registry: "my.custom.registry",
			expected: "my.custom.registry/image",
		},
		{
			image:    "cr.l5d.io/linkerd/image",
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
			registry: "cr.l5d.io/linkerd",
			expected: "cr.l5d.io/linkerd/image",
		},
		{
			image:    "",
			registry: "my.custom.registry",
			expected: "",
		},
		{
			image:    "cr.l5d.io/linkerd/image",
			registry: "",
			expected: "image",
		},
		{
			image:    "image",
			registry: "cr.l5d.io/linkerd",
			expected: "cr.l5d.io/linkerd/image",
		},
	}
	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			actual := cmd.RegistryOverride(tc.image, tc.registry)
			if actual != tc.expected {
				t.Fatalf("expected %q, but got %q", tc.expected, actual)
			}
		})
	}
}

func diffOverrides(t *testing.T, expectedOverrides map[string]string, actualOverrides map[string]string) {
	if len(expectedOverrides) != len(actualOverrides) {
		t.Fatalf("expected annotations:\n%s\nbut received:\n%s", expectedOverrides, actualOverrides)
	}
	for key, expected := range expectedOverrides {
		actual := actualOverrides[key]
		if actual != expected {
			t.Fatalf("expected annotation %q with %q, but got %q", key, expected, actual)
		}
	}
}
