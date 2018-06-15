package testutil

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TestHelper provides helpers for running the conduit integration tests.
type TestHelper struct {
	conduit    string
	version    string
	namespace  string
	httpClient http.Client
	KubernetesHelper
}

// NewTestHelper creates a new instance of TestHelper for the current test run.
// The new TestHelper can be configured via command line flags.
func NewTestHelper() *TestHelper {
	exit := func(code int, msg string) {
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(code)
	}

	conduit := flag.String("conduit", "", "path to the conduit binary to test")
	namespace := flag.String("conduit-namespace", "conduit", "the namespace where conduit is installed")
	runTests := flag.Bool("integration-tests", false, "must be provided to run the integration tests")
	flag.Parse()

	if !*runTests {
		exit(0, "integration tests not enabled: enable with -integration-tests")
	}

	if *conduit == "" {
		exit(1, "-conduit flag is required")
	}

	if !filepath.IsAbs(*conduit) {
		exit(1, "-conduit path must be absolute")
	}

	_, err := os.Stat(*conduit)
	if err != nil {
		exit(1, "-conduit binary does not exist")
	}

	testHelper := &TestHelper{
		conduit:   *conduit,
		namespace: *namespace,
	}

	version, err := testHelper.ConduitRun("version", "--client", "--short")
	if err != nil {
		exit(1, "error getting conduit version")
	}
	testHelper.version = strings.TrimSpace(version)

	kubernetesHelper, err := NewKubernetesHelper()
	if err != nil {
		exit(1, "error creating kubernetes helper: "+err.Error())
	}
	testHelper.KubernetesHelper = *kubernetesHelper

	testHelper.httpClient = http.Client{
		Timeout: 10 * time.Second,
	}

	return testHelper
}

// GetVersion returns the version of conduit to test. This version corresponds
// to the client version of the conduit binary provided via the -conduit command
// line flag.
func (h *TestHelper) GetVersion() string {
	return h.version
}

// GetConduitNamespace returns the namespace where conduit is installed. Set the
// namespace using the -conduit-namespace command line flag.
func (h *TestHelper) GetConduitNamespace() string {
	return h.namespace
}

// GetTestNamespace returns the namespace for the given test. The test namespace
// is prefixed with the conduit namespace.
func (h *TestHelper) GetTestNamespace(testName string) string {
	return h.namespace + "-" + testName
}

// CombinedOutput executes a shell command and returns the output.
func (h *TestHelper) CombinedOutput(name string, arg ...string) (string, error) {
	command := exec.Command(name, arg...)
	bytes, err := command.CombinedOutput()
	if err != nil {
		return string(bytes), err
	}

	return string(bytes), nil
}

// ConduitRun executes a conduit command appended with the --conduit-namespace
// flag.
func (h *TestHelper) ConduitRun(arg ...string) (string, error) {
	withNamespace := append(arg, "--conduit-namespace", h.namespace)
	return h.CombinedOutput(h.conduit, withNamespace...)
}

// ConduitRunStream initiates a conduit command appended with the
// --conduit-namespace flag, and returns a Stream that can be used to read the
// command's output while it is still executing.
func (h *TestHelper) ConduitRunStream(arg ...string) (*Stream, error) {
	withNamespace := append(arg, "--conduit-namespace", h.namespace)
	cmd := exec.Command(h.conduit, withNamespace...)

	cmdReader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	time.Sleep(500 * time.Millisecond)

	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return nil, fmt.Errorf("Process exited: %s", cmd.ProcessState)
	}

	return &Stream{cmd: cmd, out: cmdReader}, nil
}

// ValidateOutput validates a string against the contents of a file in the
// test's testdata directory.
func (h *TestHelper) ValidateOutput(out, fixtureFile string) error {
	b, err := ioutil.ReadFile("testdata/" + fixtureFile)
	if err != nil {
		return err
	}
	expected := string(b)

	if out != expected {
		return fmt.Errorf(
			"Expected:\n%s\nActual:\n%s", expected, out)
	}

	return nil
}

// CheckVersion validates the the output of the "conduit version" command.
func (h *TestHelper) CheckVersion(serverVersion string) error {
	out, err := h.ConduitRun("version")
	if err != nil {
		return fmt.Errorf("Unexpected error: %s\n%s", err.Error(), out)
	}
	if !strings.Contains(out, fmt.Sprintf("Client version: %s", h.version)) {
		return fmt.Errorf("Expected client version [%s], got:\n%s", h.version, out)
	}
	if !strings.Contains(out, fmt.Sprintf("Server version: %s", serverVersion)) {
		return fmt.Errorf("Expected server version [%s], got:\n%s", serverVersion, out)
	}
	return nil
}

// RetryFor retries a given function every second until the function returns
// without an error, or a timeout is reached. If the timeout is reached, it
// returns the last error received from the function.
func (h *TestHelper) RetryFor(timeout time.Duration, fn func() error) error {
	err := fn()
	if err == nil {
		return nil
	}

	timeoutAfter := time.After(timeout)
	retryAfter := time.Tick(time.Second)

	for {
		select {
		case <-timeoutAfter:
			return err
		case <-retryAfter:
			err = fn()
			if err == nil {
				return nil
			}
		}
	}
}

// HTTPGetURL sends a GET request to the given URL. It returns the response body
// in the event of a successful 200 response. In the event of a non-200
// response, it returns an error. It retries requests for up to 30 seconds,
// giving pods time to start.
func (h *TestHelper) HTTPGetURL(url string) (string, error) {
	var body string
	err := h.RetryFor(30*time.Second, func() error {
		resp, err := h.httpClient.Get(url)
		if err != nil {
			return err
		}

		defer resp.Body.Close()
		bytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("Error reading response body: %v", err)
		}
		body = string(bytes)

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GET request to [%s] returned status [%d]\n%s", url, resp.StatusCode, body)
		}

		return nil
	})

	return body, err
}
