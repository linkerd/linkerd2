package testutil

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// TestHelper provides helpers for running the linkerd integration tests.
type TestHelper struct {
	linkerd    string
	version    string
	namespace  string
	tls        bool
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

	linkerd := flag.String("linkerd", "", "path to the linkerd binary to test")
	namespace := flag.String("linkerd-namespace", "linkerd", "the namespace where linkerd is installed")
	tls := flag.Bool("enable-tls", false, "enable TLS in tests")
	runTests := flag.Bool("integration-tests", false, "must be provided to run the integration tests")
	verbose := flag.Bool("verbose", false, "turn on debug logging")
	flag.Parse()

	if !*runTests {
		exit(0, "integration tests not enabled: enable with -integration-tests")
	}

	if *linkerd == "" {
		exit(1, "-linkerd flag is required")
	}

	if !filepath.IsAbs(*linkerd) {
		exit(1, "-linkerd path must be absolute")
	}

	_, err := os.Stat(*linkerd)
	if err != nil {
		exit(1, "-linkerd binary does not exist")
	}

	if *verbose {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.PanicLevel)
	}

	ns := *namespace
	if *tls {
		ns += "-tls"
	}

	testHelper := &TestHelper{
		linkerd:   *linkerd,
		namespace: ns,
		tls:       *tls,
	}

	version, _, err := testHelper.LinkerdRun("version", "--client", "--short")
	if err != nil {
		exit(1, "error getting linkerd version: "+err.Error())
	}
	testHelper.version = strings.TrimSpace(version)

	kubernetesHelper, err := NewKubernetesHelper(testHelper.RetryFor)
	if err != nil {
		exit(1, "error creating kubernetes helper: "+err.Error())
	}
	testHelper.KubernetesHelper = *kubernetesHelper

	testHelper.httpClient = http.Client{
		Timeout: 10 * time.Second,
	}

	return testHelper
}

// GetVersion returns the version of linkerd to test. This version corresponds
// to the client version of the linkerd binary provided via the -linkerd command
// line flag.
func (h *TestHelper) GetVersion() string {
	return h.version
}

// GetLinkerdNamespace returns the namespace where linkerd is installed. Set the
// namespace using the -linkerd-namespace command line flag.
func (h *TestHelper) GetLinkerdNamespace() string {
	return h.namespace
}

// GetTestNamespace returns the namespace for the given test. The test namespace
// is prefixed with the linkerd namespace.
func (h *TestHelper) GetTestNamespace(testName string) string {
	return h.namespace + "-" + testName
}

// TLS returns whether or not TLS is enabled for the given test.
func (h *TestHelper) TLS() bool {
	return h.tls
}

// CombinedOutput executes a shell command and returns the output.
func (h *TestHelper) CombinedOutput(name string, arg ...string) (string, string, error) {
	command := exec.Command(name, arg...)
	var stderr bytes.Buffer
	command.Stderr = &stderr

	stdout, err := command.Output()
	return string(stdout), stderr.String(), err
}

// LinkerdRun executes a linkerd command appended with the --linkerd-namespace
// flag.
func (h *TestHelper) LinkerdRun(arg ...string) (string, string, error) {
	withNamespace := append(arg, "--linkerd-namespace", h.namespace)
	return h.CombinedOutput(h.linkerd, withNamespace...)
}

// LinkerdRunStream initiates a linkerd command appended with the
// --linkerd-namespace flag, and returns a Stream that can be used to read the
// command's output while it is still executing.
func (h *TestHelper) LinkerdRunStream(arg ...string) (*Stream, error) {
	withNamespace := append(arg, "--linkerd-namespace", h.namespace)
	cmd := exec.Command(h.linkerd, withNamespace...)

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

// CheckVersion validates the the output of the "linkerd version" command.
func (h *TestHelper) CheckVersion(serverVersion string) error {
	err := h.RetryFor(func() error {
		out, _, err := h.LinkerdRun("version")
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
	})
	return err
}

// RetryFor retries a given function every second until the function returns
// without an error, or a timeout is reached. If the timeout is reached, it
// returns the last error received from the function.
func (h *TestHelper) RetryFor(fn func() error) error {
	timeout := 2 * time.Minute
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
	err := h.RetryFor(func() error {
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
