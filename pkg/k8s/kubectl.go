package k8s

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/runconduit/conduit/pkg/healthcheck"

	"github.com/runconduit/conduit/pkg/shell"
)

type Kubectl interface {
	Version() ([3]int, error)
	StartProxy(potentialErrorWhenStartingProxy chan error, port int) error
	UrlFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error)
	ProxyPort() int
	healthcheck.StatusChecker
}

type kubectl struct {
	sh        shell.Shell
	proxyPort int
}

const (
	KubernetesDeployments               = "deployments"
	KubernetesPods                      = "pods"
	kubectlDefaultProxyPort             = 8001
	kubectlDefaultTimeout               = 10 * time.Second
	portWhenProxyNotRunning             = -1
	KubectlSubsystemName                = "kubectl"
	KubectlIsInstalledCheckDescription  = "is in $PATH"
	KubectlVersionCheckDescription      = "has compatible version"
	KubectlConnectivityCheckDescription = "can talk to Kubernetes cluster"
	//As per https://github.com/kubernetes/kubernetes/commit/0daee3ad2238de7bb356d1b4368b0733a3497a3a#diff-595bfea7ed0dd0171e1f339a1f8bfcb6R155
	magicCharacterThatIndicatesProxyIsRunning = '\n'
)

var minimumKubectlVersionExpected = [3]int{1, 8, 0}

func (kctl *kubectl) ProxyPort() int {
	return kctl.proxyPort
}

func (kctl *kubectl) ProxyHost() string {
	return "127.0.0.1"
}

func (kctl *kubectl) ProxyScheme() string {
	return "http"
}

func (kctl *kubectl) Version() ([3]int, error) {
	var version [3]int
	bytes, err := kctl.sh.CombinedOutput("kubectl", "version", "--client", "--short")
	versionString := string(bytes)
	if err != nil {
		return version, fmt.Errorf("error running kubectl Version. Output: %s Error: %v", versionString, err)
	}

	justTheVersionString := strings.TrimPrefix(versionString, "Client Version: v")
	justTheMajorMinorRevisionNumbers := strings.Split(justTheVersionString, "-")[0]
	split := strings.Split(justTheMajorMinorRevisionNumbers, ".")

	if len(split) < 3 {
		return version, fmt.Errorf("unknown Version string format from Kubectl: [%s] not enough segments: %s", versionString, split)
	}

	for i, segment := range split {
		v, err := strconv.Atoi(strings.TrimSpace(segment))
		if err != nil {
			return version, fmt.Errorf("unknown Version string format from Kubectl: [%s], not an integer: [%s]", versionString, segment)
		}
		version[i] = v
	}

	return version, nil
}

func (kctl *kubectl) StartProxy(potentialErrorWhenStartingProxy chan error, port int) error {
	fmt.Printf("Running `kubectl proxy -p %d`\n", port)

	if kctl.ProxyPort() != portWhenProxyNotRunning {
		return fmt.Errorf("kubectl proxy already running on port [%d]", kctl.ProxyPort)
	}

	output, err := kctl.sh.AsyncStdout(potentialErrorWhenStartingProxy, "kubectl", "proxy", "-p", strconv.Itoa(port))

	kubectlOutput, err := kctl.sh.WaitForCharacter(magicCharacterThatIndicatesProxyIsRunning, output, kubectlDefaultTimeout)

	fmt.Println(kubectlOutput)
	if err != nil {
		return fmt.Errorf("error waiting for kubectl to start the proxy. kubectl returned [%s], error: %v", kubectlOutput, err)
	}

	kctl.proxyPort = kubectlDefaultProxyPort
	return nil
}

func (kctl *kubectl) UrlFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error) {
	if kctl.ProxyPort() == portWhenProxyNotRunning {
		return nil, errors.New("proxy needs to be started before generating URLs")
	}

	schemeHostAndPort := fmt.Sprintf("%s://%s:%d", kctl.ProxyScheme(), kctl.ProxyHost(), kctl.ProxyPort())

	return generateKubernetesApiBaseUrlFor(schemeHostAndPort, namespace, extraPathStartingWithSlash)
}

func (kctl *kubectl) SelfCheck() ([]healthcheck.CheckResult, error) {

	kubectlOnPathCheck := healthcheck.CheckResult{
		SubsystemName:    KubectlSubsystemName,
		CheckDescription: KubectlIsInstalledCheckDescription,
		Status:           healthcheck.CheckError,
	}
	_, err := kctl.sh.CombinedOutput("kubectl", "config")
	if err != nil {
		kubectlOnPathCheck.Status = healthcheck.CheckFailed
		kubectlOnPathCheck.NextSteps = fmt.Sprintf("Could not run the command `kubectl`. The error message is: [%s] and the current $PATH is: %s", err.Error(), kctl.sh.Path())
	} else {
		kubectlOnPathCheck.Status = healthcheck.CheckOk
	}

	kubectlVersionCheck := healthcheck.CheckResult{
		SubsystemName:    KubectlSubsystemName,
		CheckDescription: KubectlVersionCheckDescription,
		Status:           healthcheck.CheckError,
	}

	actualVersion, err := kctl.Version()
	if err != nil {
		kubectlVersionCheck.Status = healthcheck.CheckError
		kubectlVersionCheck.NextSteps = fmt.Sprintf("Error getting version from kubectl. The error message is: [%s].", err.Error())
	} else {
		if isCompatibleVersion(minimumKubectlVersionExpected, actualVersion) {
			kubectlVersionCheck.Status = healthcheck.CheckOk
		} else {
			kubectlVersionCheck.Status = healthcheck.CheckFailed
			kubectlVersionCheck.NextSteps = fmt.Sprintf("Kubectl is on version [%d.%d.%d], but version [%d.%d.%d] or more recent is required.",
				actualVersion[0], actualVersion[1], actualVersion[2],
				minimumKubectlVersionExpected[0], minimumKubectlVersionExpected[1], minimumKubectlVersionExpected[2])
		}
	}

	kubectlApiAccessCheck := healthcheck.CheckResult{
		SubsystemName:    KubectlSubsystemName,
		CheckDescription: KubectlConnectivityCheckDescription,
		Status:           healthcheck.CheckError,
	}
	output, err := kctl.sh.CombinedOutput("kubectl", "get", "pods")
	if err != nil {
		kubectlApiAccessCheck.Status = healthcheck.CheckFailed
		kubectlApiAccessCheck.NextSteps = output
	} else {
		kubectlApiAccessCheck.Status = healthcheck.CheckOk
	}

	results := []healthcheck.CheckResult{
		kubectlOnPathCheck,
		kubectlVersionCheck,
		kubectlApiAccessCheck,
	}
	return results, nil
}

func isCompatibleVersion(minimalRequirementVersion [3]int, actualVersion [3]int) bool {
	if minimalRequirementVersion[0] < actualVersion[0] {
		return true
	}

	if (minimalRequirementVersion[0] == actualVersion[0]) && minimalRequirementVersion[1] <= actualVersion[1] {
		return true
	}

	if (minimalRequirementVersion[0] == actualVersion[0]) && (minimalRequirementVersion[1] == actualVersion[1]) && (minimalRequirementVersion[2] <= actualVersion[2]) {
		return true
	}

	return false
}

//CanonicalKubernetesNameFromFriendlyName returns a canonical name from common shorthands used in command line tools.
// This works based on https://github.com/kubernetes/kubernetes/blob/63ffb1995b292be0a1e9ebde6216b83fc79dd988/pkg/kubectl/kubectl.go#L39
func CanonicalKubernetesNameFromFriendlyName(friendlyName string) (string, error) {
	switch friendlyName {
	case "deploy", "deployment", "deployments":
		return KubernetesDeployments, nil
	case "po", "pod", "pods":
		return KubernetesPods, nil
	}

	return "", fmt.Errorf("cannot find Kubernetes canonical name from friendly name [%s]", friendlyName)
}

func MakeKubectl(shell shell.Shell) (Kubectl, error) {

	kubectl := &kubectl{
		sh:        shell,
		proxyPort: portWhenProxyNotRunning,
	}

	actualVersion, err := kubectl.Version()

	if err != nil {
		return nil, err
	}

	if !isCompatibleVersion(minimumKubectlVersionExpected, actualVersion) {
		return nil, fmt.Errorf(
			"kubectl is on version [%d.%d.%d], but version [%d.%d.%d] or more recent is required",
			actualVersion[0], actualVersion[1], actualVersion[2],
			minimumKubectlVersionExpected[0], minimumKubectlVersionExpected[1], minimumKubectlVersionExpected[2])
	}

	return kubectl, nil

}
