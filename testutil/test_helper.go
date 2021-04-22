package testutil

import (
	"bytes"
	"context"
	"encoding/base64"
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
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

// TestHelper provides helpers for running the linkerd integration tests.
type TestHelper struct {
	linkerd            string
	version            string
	namespace          string
	vizNamespace       string
	upgradeFromVersion string
	clusterDomain      string
	externalIssuer     bool
	externalPrometheus bool
	multicluster       bool
	uninstall          bool
	cni                bool
	calico             bool
	certsPath          string
	httpClient         http.Client
	KubernetesHelper
	helm
	installedExtensions []string
}

type helm struct {
	path                    string
	chart                   string
	multiclusterChart       string
	vizChart                string
	vizStableChart          string
	stableChart             string
	releaseName             string
	multiclusterReleaseName string
	upgradeFromVersion      string
}

// DeploySpec is used to hold information about what deploys we should verify during testing
type DeploySpec struct {
	Namespace string
	Replicas  int
}

// Service is used to hold information about a Service we should verify during testing
type Service struct {
	Namespace string
	Name      string
}

// LinkerdDeployReplicasEdge is a map containing the number of replicas for each Deployment and the main
// container name, in the current code-base
var LinkerdDeployReplicasEdge = map[string]DeploySpec{
	"linkerd-destination":    {"linkerd", 1},
	"tap":                    {"linkerd-viz", 1},
	"grafana":                {"linkerd-viz", 1},
	"linkerd-identity":       {"linkerd", 1},
	"web":                    {"linkerd-viz", 1},
	"linkerd-proxy-injector": {"linkerd", 1},
}

// LinkerdDeployReplicasStable is a map containing the number of replicas for each Deployment and the main
// container name. Override whenever edge deviates from stable.
var LinkerdDeployReplicasStable = LinkerdDeployReplicasEdge

// NewGenericTestHelper returns a new *TestHelper from the options provided as function parameters.
// This helper was created to be able to reuse this package without hard restrictions
// as seen in `NewTestHelper()` which is primarily used with integration tests
// See - https://github.com/linkerd/linkerd2/issues/4530
func NewGenericTestHelper(
	linkerd,
	version,
	namespace,
	vizNamespace,
	upgradeFromVersion,
	clusterDomain,
	helmPath,
	helmChart,
	helmStableChart,
	helmReleaseName,
	helmMulticlusterReleaseName,
	helmMulticlusterChart string,
	externalIssuer,
	externalPrometheus,
	multicluster,
	cni,
	calico,
	uninstall bool,
	httpClient http.Client,
	kubernetesHelper KubernetesHelper,
) *TestHelper {
	return &TestHelper{
		linkerd:            linkerd,
		version:            version,
		namespace:          namespace,
		vizNamespace:       vizNamespace,
		upgradeFromVersion: upgradeFromVersion,
		helm: helm{
			path:                    helmPath,
			chart:                   helmChart,
			multiclusterChart:       helmMulticlusterChart,
			multiclusterReleaseName: helmMulticlusterReleaseName,
			stableChart:             helmStableChart,
			releaseName:             helmReleaseName,
			upgradeFromVersion:      upgradeFromVersion,
		},
		clusterDomain:      clusterDomain,
		externalIssuer:     externalIssuer,
		externalPrometheus: externalPrometheus,
		uninstall:          uninstall,
		cni:                cni,
		calico:             calico,
		httpClient:         httpClient,
		multicluster:       multicluster,
		KubernetesHelper:   kubernetesHelper,
	}
}

// MulticlusterDeployReplicas is a map containing the number of replicas for each Deployment and the main
// container name for multicluster components
var MulticlusterDeployReplicas = map[string]DeploySpec{
	"linkerd-gateway": {"linkerd-multicluster", 1},
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
	namespace := flag.String("linkerd-namespace", "linkerd", "the namespace where linkerd is installed")
	vizNamespace := flag.String("viz-namespace", "linkerd-viz", "the namespace where linkerd viz extension is installed")
	multicluster := flag.Bool("multicluster", false, "when specified the multicluster install functionality is tested")
	helmPath := flag.String("helm-path", "target/helm", "path of the Helm binary")
	helmChart := flag.String("helm-chart", "charts/linkerd2", "path to linkerd2's Helm chart")
	multiclusterHelmChart := flag.String("multicluster-helm-chart", "charts/linkerd-multicluster", "path to linkerd2's multicluster Helm chart")
	vizHelmChart := flag.String("viz-helm-chart", "charts/linkerd-viz", "path to linkerd2's viz extension Helm chart")
	vizHelmStableChart := flag.String("viz-helm-stable-chart", "charts/linkerd-viz", "path to linkerd2's viz extension stable Helm chart")
	helmStableChart := flag.String("helm-stable-chart", "linkerd/linkerd2", "path to linkerd2's stable Helm chart")
	helmReleaseName := flag.String("helm-release", "", "install linkerd via Helm using this release name")
	multiclusterHelmReleaseName := flag.String("multicluster-helm-release", "", "install linkerd multicluster via Helm using this release name")
	upgradeFromVersion := flag.String("upgrade-from-version", "", "when specified, the upgrade test uses it as the base version of the upgrade")
	clusterDomain := flag.String("cluster-domain", "cluster.local", "when specified, the install test uses a custom cluster domain")
	externalIssuer := flag.Bool("external-issuer", false, "when specified, the install test uses it to install linkerd with --identity-external-issuer=true")
	externalPrometheus := flag.Bool("external-prometheus", false, "when specified, the install test uses an external prometheus")
	runTests := flag.Bool("integration-tests", false, "must be provided to run the integration tests")
	verbose := flag.Bool("verbose", false, "turn on debug logging")
	upgradeHelmFromVersion := flag.String("upgrade-helm-from-version", "", "Indicate a version of the Linkerd helm chart from which the helm installation is being upgraded")
	uninstall := flag.Bool("uninstall", false, "whether to run the 'linkerd uninstall' integration test")
	cni := flag.Bool("cni", false, "whether to install linkerd with CNI enabled")
	calico := flag.Bool("calico", false, "whether to install calico CNI plugin")
	certsPath := flag.String("certs-path", "", "if non-empty, 'linkerd install' will use the files ca.crt, issuer.crt and issuer.key under this path in its --identity-* flags")
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
		vizNamespace:       *vizNamespace,
		upgradeFromVersion: *upgradeFromVersion,
		multicluster:       *multicluster,
		helm: helm{
			path:                    *helmPath,
			chart:                   *helmChart,
			multiclusterChart:       *multiclusterHelmChart,
			vizChart:                *vizHelmChart,
			vizStableChart:          *vizHelmStableChart,
			stableChart:             *helmStableChart,
			releaseName:             *helmReleaseName,
			multiclusterReleaseName: *multiclusterHelmReleaseName,
			upgradeFromVersion:      *upgradeHelmFromVersion,
		},
		clusterDomain:      *clusterDomain,
		externalIssuer:     *externalIssuer,
		externalPrometheus: *externalPrometheus,
		cni:                *cni,
		calico:             *calico,
		uninstall:          *uninstall,
		certsPath:          *certsPath,
	}

	version, err := testHelper.LinkerdRun("version", "--client", "--short")
	if err != nil {
		exit(1, fmt.Sprintf("error getting linkerd version: %s", err.Error()))
	}
	testHelper.version = strings.TrimSpace(version)

	kubernetesHelper, err := NewKubernetesHelper(*k8sContext, testHelper.RetryFor)
	if err != nil {
		exit(1, fmt.Sprintf("error creating kubernetes helper: %s", err.Error()))
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

// GetVizNamespace returns the namespace where linkerd Viz Extension is installed. Set the
// namespace using the -linkerd-namespace command line flag.
func (h *TestHelper) GetVizNamespace() string {
	return h.vizNamespace
}

// GetMulticlusterNamespace returns the namespace where multicluster
// components are installed.
func (h *TestHelper) GetMulticlusterNamespace() string {
	return fmt.Sprintf("%s-multicluster", h.GetLinkerdNamespace())
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

// GetMulticlusterHelmReleaseName returns the name of the Linkerd multicluster installation Helm release
func (h *TestHelper) GetMulticlusterHelmReleaseName() string {
	return h.helm.multiclusterReleaseName
}

// GetHelmChart returns the path to the Linkerd Helm chart
func (h *TestHelper) GetHelmChart() string {
	return h.helm.chart
}

// GetMulticlusterHelmChart returns the path to the Linkerd multicluster Helm chart
func (h *TestHelper) GetMulticlusterHelmChart() string {
	return h.helm.multiclusterChart
}

// GetLinkerdVizHelmChart returns the path to the Linkerd viz Helm chart
func (h *TestHelper) GetLinkerdVizHelmChart() string {
	return h.helm.vizChart
}

// GetLinkerdVizHelmStableChart returns the path to the Linkerd viz Helm
// stable chart
func (h *TestHelper) GetLinkerdVizHelmStableChart() string {
	return h.helm.vizStableChart
}

// GetHelmStableChart returns the path to the Linkerd Helm stable chart
func (h *TestHelper) GetHelmStableChart() string {
	return h.helm.stableChart
}

// UpgradeHelmFromVersion returns the version from which Linkerd should be upgraded with Helm
func (h *TestHelper) UpgradeHelmFromVersion() string {
	return h.helm.upgradeFromVersion
}

// ExternalIssuer determines whether linkerd should be installed with --identity-external-issuer
func (h *TestHelper) ExternalIssuer() bool {
	return h.externalIssuer
}

// ExternalPrometheus determines whether linkerd should be installed with --set prometheusUrl
func (h *TestHelper) ExternalPrometheus() bool {
	return h.externalPrometheus
}

// Multicluster determines whether multicluster components should be installed
func (h *TestHelper) Multicluster() bool {
	return h.multicluster
}

// Uninstall determines whether the "linkerd uninstall" integration test should be run
func (h *TestHelper) Uninstall() bool {
	return h.uninstall
}

// CertsPath returns the path for the ca.cert, issuer.crt and issuer.key files that `linkerd install`
// will use in its --identity-* flags
func (h *TestHelper) CertsPath() string {
	return h.certsPath
}

// UpgradeFromVersion returns the base version of the upgrade test.
func (h *TestHelper) UpgradeFromVersion() string {
	return h.upgradeFromVersion
}

// GetClusterDomain returns the custom cluster domain that needs to be used during linkerd installation
func (h *TestHelper) GetClusterDomain() string {
	return h.clusterDomain
}

// CNI determines whether CNI should be enabled
func (h *TestHelper) CNI() bool {
	return h.cni
}

// Calico determines whether Calico CNI plug-in is enabled
func (h *TestHelper) Calico() bool {
	return h.calico
}

// AddInstalledExtension adds an extension name to installedExtensions to
// track the currently installed linkerd extensions.
func (h *TestHelper) AddInstalledExtension(extensionName string) {
	h.installedExtensions = append(h.installedExtensions, extensionName)
}

// GetInstalledExtensions gets a list currently installed extensions
// in a test run.
func (h *TestHelper) GetInstalledExtensions() []string {
	return h.installedExtensions
}

// CreateTLSSecret creates a TLS Kubernetes secret
func (h *TestHelper) CreateTLSSecret(name, root, cert, key string) error {
	secret := fmt.Sprintf(`
apiVersion: v1
data:
    ca.crt: %s
    tls.crt: %s
    tls.key: %s
kind: Secret
metadata:
    name: %s
type: kubernetes.io/tls`, base64.StdEncoding.EncodeToString([]byte(root)), base64.StdEncoding.EncodeToString([]byte(cert)), base64.StdEncoding.EncodeToString([]byte(key)), name)

	_, err := h.KubectlApply(secret, h.GetLinkerdNamespace())
	return err
}

// LinkerdRun executes a linkerd command returning its stdout.
func (h *TestHelper) LinkerdRun(arg ...string) (string, error) {
	out, stderr, err := h.PipeToLinkerdRun("", arg...)
	if err != nil {
		return out, fmt.Errorf("command failed: linkerd %s\n%s\n%s", strings.Join(arg, " "), err, stderr)
	}
	return out, nil
}

// PipeToLinkerdRun executes a linkerd command appended with the
// --linkerd-namespace flag, and provides a string at Stdin.
func (h *TestHelper) PipeToLinkerdRun(stdin string, arg ...string) (string, string, error) {
	withParams := append([]string{"--linkerd-namespace", h.namespace, "--context=" + h.k8sContext}, arg...)
	return combinedOutput(stdin, h.linkerd, withParams...)
}

// HelmRun executes a helm command appended with the --context
func (h *TestHelper) HelmRun(arg ...string) (string, string, error) {
	return h.PipeToHelmRun("", arg...)
}

// PipeToHelmRun executes a Helm command appended with the
// --context flag, and provides a string at Stdin.
func (h *TestHelper) PipeToHelmRun(stdin string, arg ...string) (string, string, error) {
	withParams := append([]string{"--kube-context=" + h.k8sContext}, arg...)
	return combinedOutput(stdin, h.helm.path, withParams...)
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

// KubectlStream initiates a kubectl command appended with the
// --namespace flag, and returns a Stream that can be used to read the
// command's output while it is still executing.
func (h *TestHelper) KubectlStream(arg ...string) (*Stream, error) {

	withContext := append([]string{"--namespace", h.namespace, "--context=" + h.k8sContext}, arg...)
	cmd := exec.Command("kubectl", withContext...)

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

// HelmUpgrade runs the helm upgrade subcommand, with the provided arguments
func (h *TestHelper) HelmUpgrade(chart string, arg ...string) (string, string, error) {
	withParams := append([]string{
		"upgrade",
		h.helm.releaseName,
		"--kube-context", h.k8sContext,
		"--set", "namespace=" + h.namespace,
		chart,
	}, arg...)
	return combinedOutput("", h.helm.path, withParams...)
}

// HelmInstall runs the helm install subcommand, with the provided arguments
func (h *TestHelper) HelmInstall(chart string, arg ...string) (string, string, error) {
	withParams := append([]string{
		"install",
		h.helm.releaseName,
		chart,
		"--kube-context", h.k8sContext,
		"--set", "namespace=" + h.namespace,
	}, arg...)
	return combinedOutput("", h.helm.path, withParams...)
}

// HelmCmdPlain runs a helm subcommand, with the provided arguments and no defaults
func (h *TestHelper) HelmCmdPlain(cmd, chart, releaseName string, arg ...string) (string, string, error) {
	withParams := append([]string{
		cmd,
		releaseName,
		chart,
		"--kube-context", h.k8sContext,
	}, arg...)

	return combinedOutput("", h.helm.path, withParams...)
}

// HelmInstallMulticluster runs the helm install subcommand for multicluster, with the provided arguments
func (h *TestHelper) HelmInstallMulticluster(chart string, arg ...string) (string, string, error) {
	withParams := append([]string{
		"install",
		h.helm.multiclusterReleaseName,
		chart,
		"--kube-context", h.k8sContext,
		"--set", "namespace=" + h.GetMulticlusterNamespace(),
		"--set", "linkerdNamespace=" + h.GetLinkerdNamespace(),
	}, arg...)
	return combinedOutput("", h.helm.path, withParams...)
}

// HelmUninstallMulticluster runs the helm delete subcommand for multicluster
func (h *TestHelper) HelmUninstallMulticluster(chart string) (string, string, error) {
	withParams := []string{
		"delete",
		h.helm.multiclusterReleaseName,
		"--kube-context", h.k8sContext,
	}
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
	out, err := h.LinkerdRun("version")
	if err != nil {
		return err
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

// WithDataPlaneNamespace is used to create a test namespace that is deleted before the function returns
func (h *TestHelper) WithDataPlaneNamespace(ctx context.Context, testName string, annotations map[string]string, t *testing.T, test func(t *testing.T, ns string)) {
	prefixedNs := h.GetTestNamespace(testName)
	if err := h.CreateDataPlaneNamespaceIfNotExists(ctx, prefixedNs, annotations); err != nil {
		AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", prefixedNs),
			"failed to create %s namespace: %s", prefixedNs, err)
	}
	test(t, prefixedNs)
	if err := h.DeleteNamespaceIfExists(ctx, prefixedNs); err != nil {
		AnnotatedFatalf(t, fmt.Sprintf("failed to delete %s namespace", prefixedNs),
			"failed to delete %s namespace: %s", prefixedNs, err)
	}
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
