package testutil

import (
	"bytes"
	"encoding/json"
	"errors"
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
	corev1 "k8s.io/api/core/v1"
)

// TestHelper provides helpers for running the linkerd integration tests.
type TestHelper struct {
	linkerd            string
	version            string
	namespace          string
	upgradeFromVersion string
	clusterDomain      string
	externalIssuer     bool
	httpClient         http.Client
	KubernetesHelper
	helm
}

type helm struct {
	path        string
	chart       string
	releaseName string
	tillerNs    string
}

// NewTestHelper creates a new instance of TestHelper for the current test run.
// The new TestHelper can be configured via command line flags.
func NewTestHelper() *TestHelper {
	exit := func(code int, msg string) {
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(code)
	}

	k8sContext := flag.String("k8s-context", "", "kubernetes context associated with the test cluster")
	linkerd := flag.String("linkerd", "", "path to the linkerd binary to test")
	namespace := flag.String("linkerd-namespace", "l5d-integration", "the namespace where linkerd is installed")
	helmPath := flag.String("helm-path", "target/helm", "path of the Helm binary")
	helmChart := flag.String("helm-chart", "charts/linkerd2", "path to linkerd2's Helm chart")
	helmReleaseName := flag.String("helm-release", "", "install linkerd via Helm using this release name")
	tillerNs := flag.String("tiller-ns", "kube-system", "namespace under which Tiller will be installed")
	upgradeFromVersion := flag.String("upgrade-from-version", "", "when specified, the upgrade test uses it as the base version of the upgrade")
	clusterDomain := flag.String("cluster-domain", "", "when specified, the install test uses a custom cluster domain")
	externalIssuer := flag.Bool("external-issuer", false, "when specified, the install test uses it to install linkerd with --identity-external-issuer=true")
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

	testHelper := &TestHelper{
		linkerd:            *linkerd,
		namespace:          *namespace,
		upgradeFromVersion: *upgradeFromVersion,
		helm: helm{
			path:        *helmPath,
			chart:       *helmChart,
			releaseName: *helmReleaseName,
			tillerNs:    *tillerNs,
		},
		clusterDomain:  *clusterDomain,
		externalIssuer: *externalIssuer,
	}

	version, _, err := testHelper.LinkerdRun("version", "--client", "--short")
	if err != nil {
		exit(1, "error getting linkerd version: "+err.Error())
	}
	testHelper.version = strings.TrimSpace(version)

	kubernetesHelper, err := NewKubernetesHelper(*k8sContext, testHelper.RetryFor)
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

// GetHelmReleaseName returns the name of the Linkerd installation Helm release
func (h *TestHelper) GetHelmReleaseName() string {
	return h.helm.releaseName
}

// ExternalIssuer determines whether linkerd should be installed with --identity-external-issuer
func (h *TestHelper) ExternalIssuer() bool {
	return h.externalIssuer
}

// UpgradeFromVersion returns the base version of the upgrade test.
func (h *TestHelper) UpgradeFromVersion() string {
	return h.upgradeFromVersion
}

// GetClusterDomain returns the custom cluster domain that needs to be used during linkerd installation
func (h *TestHelper) GetClusterDomain() string {
	return h.clusterDomain
}

// LinkerdRun executes a linkerd command appended with the --linkerd-namespace
// flag.
func (h *TestHelper) LinkerdRun(arg ...string) (string, string, error) {
	return h.PipeToLinkerdRun("", arg...)
}

// PipeToLinkerdRun executes a linkerd command appended with the
// --linkerd-namespace flag, and provides a string at Stdin.
func (h *TestHelper) PipeToLinkerdRun(stdin string, arg ...string) (string, string, error) {
	withParams := append([]string{"--linkerd-namespace", h.namespace, "--context=" + h.k8sContext}, arg...)
	return combinedOutput(stdin, h.linkerd, withParams...)
}

// LinkerdRunStream initiates a linkerd command appended with the
// --linkerd-namespace flag, and returns a Stream that can be used to read the
// command's output while it is still executing.
func (h *TestHelper) LinkerdRunStream(arg ...string) (*Stream, error) {
	withParams := append([]string{"--linkerd-namespace", h.namespace, "--context=" + h.k8sContext}, arg...)
	cmd := exec.Command(h.linkerd, withParams...)

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

// HelmRun runs the provided Helm subcommand, with the provided arguments
func (h *TestHelper) HelmRun(cmd string, arg ...string) (string, string, error) {
	withParams := append([]string{
		cmd,
		"--kube-context", h.k8sContext,
		"--tiller-namespace", h.helm.tillerNs,
		"--name", h.helm.releaseName,
		"--set", "Namespace=" + h.namespace,
		h.helm.chart,
	}, arg...)
	return combinedOutput("", h.helm.path, withParams...)
}

// ValidateOutput validates a string against the contents of a file in the
// test's testdata directory.
func (h *TestHelper) ValidateOutput(out, fixtureFile string) error {
	expected, err := ReadFile("testdata/" + fixtureFile)
	if err != nil {
		return err
	}

	if out != expected {
		return fmt.Errorf(
			"Expected:\n%s\nActual:\n%s", expected, out)
	}

	return nil
}

// CheckVersion validates the output of the "linkerd version" command.
func (h *TestHelper) CheckVersion(serverVersion string) error {
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
	err := h.RetryFor(time.Minute, func() error {
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

// ReadFile reads a file from disk and returns the contents as a string.
func ReadFile(file string) (string, error) {
	b, err := ioutil.ReadFile(file)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// combinedOutput executes a shell command and returns the output.
func combinedOutput(stdin string, name string, arg ...string) (string, string, error) {
	command := exec.Command(name, arg...)
	command.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	command.Stderr = &stderr

	stdout, err := command.Output()
	return string(stdout), stderr.String(), err
}

// RowStat is used to store the contents for a single row from the stat command
type RowStat struct {
	Name               string
	Status             string
	Meshed             string
	Success            string
	Rps                string
	P50Latency         string
	P95Latency         string
	P99Latency         string
	TCPOpenConnections string
}

// CheckRowCount checks that expectedRowCount rows have been returned
func CheckRowCount(out string, expectedRowCount int) ([]string, error) {
	out = strings.TrimSuffix(out, "\n")
	rows := strings.Split(out, "\n")
	if len(rows) < 2 {
		return nil, fmt.Errorf(
			"Error stripping header and trailing newline; full output:\n%s",
			strings.Join(rows, "\n"),
		)
	}
	rows = rows[1:] // strip header
	if len(rows) != expectedRowCount {
		return nil, fmt.Errorf(
			"Expected [%d] rows in stat output, got [%d]; full output:\n%s",
			expectedRowCount, len(rows), strings.Join(rows, "\n"))
	}

	return rows, nil
}

// ParseRows parses the output of linkerd stat into a map of resource names to RowStat objects
func ParseRows(out string, expectedRowCount, expectedColumnCount int) (map[string]*RowStat, error) {
	rows, err := CheckRowCount(out, expectedRowCount)
	if err != nil {
		return nil, err
	}

	rowStats := make(map[string]*RowStat)
	for _, row := range rows {
		fields := strings.Fields(row)

		if expectedColumnCount == 0 {
			expectedColumnCount = 8
		}
		if len(fields) != expectedColumnCount {
			return nil, fmt.Errorf(
				"Expected [%d] columns in stat output, got [%d]; full output:\n%s",
				expectedColumnCount, len(fields), row)
		}

		rowStats[fields[0]] = &RowStat{
			Name: fields[0],
		}

		i := 0
		if expectedColumnCount == 9 {
			rowStats[fields[0]].Status = fields[1]
			i = 1
		}
		rowStats[fields[0]].Meshed = fields[1+i]
		rowStats[fields[0]].Success = fields[2+i]
		rowStats[fields[0]].Rps = fields[3+i]
		rowStats[fields[0]].P50Latency = fields[4+i]
		rowStats[fields[0]].P95Latency = fields[5+i]
		rowStats[fields[0]].P99Latency = fields[6+i]
		rowStats[fields[0]].TCPOpenConnections = fields[7+i]
	}

	return rowStats, nil
}

// ParseEvents parses the output of kubectl events
func ParseEvents(out string) ([]*corev1.Event, error) {
	var list corev1.List
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("error unmarshaling list from `kubectl get events`: %s", err)
	}

	if len(list.Items) == 0 {
		return nil, errors.New("no events found")
	}

	var events []*corev1.Event
	for _, i := range list.Items {
		var e corev1.Event
		if err := json.Unmarshal(i.Raw, &e); err != nil {
			return nil, fmt.Errorf("error unmarshaling list event from `kubectl get events`: %s", err)
		}
		events = append(events, &e)
	}

	return events, nil
}
