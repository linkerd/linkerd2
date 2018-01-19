package k8s

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	"github.com/runconduit/conduit/pkg/healthcheck"
	"github.com/runconduit/conduit/pkg/shell"
)

type Kubectl interface {
	Version() (*VersionInfo, error)
	StartProxy(potentialErrorWhenStartingProxy chan error, port int) error
	UrlFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error)
	ProxyPort() int
	healthcheck.StatusChecker
}

type kubectl struct {
	sh        shell.Shell
	proxyPort int
}

type VersionInfo struct {
	Client [3]int
	Server [3]int
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

func (v *VersionInfo) ToVersionString() [2]string {
	var versions [2]string
	prettyPrintArray := func(arr [3]int) string {
		versionString := ""
		for i, version := range arr {

			versionString = versionString + strconv.Itoa(version)
			if i < len(arr)-1 {
				versionString = versionString + "."
			}
		}
		return "v" + versionString
	}

	versions[0] = prettyPrintArray(v.Client)
	versions[1] = prettyPrintArray(v.Server)
	return versions
}

func (kctl *kubectl) ProxyPort() int {
	return kctl.proxyPort
}

func (kctl *kubectl) ProxyHost() string {
	return "127.0.0.1"
}

func (kctl *kubectl) ProxyScheme() string {
	return "http"
}

func (kctl *kubectl) Version() (*VersionInfo, error) {
	var versionStruct = &VersionInfo{}
	var clientPrefix = "Client Version: v"
	var serverPrefix = "Server Version: v"

	bytes, err := kctl.sh.CombinedOutput("kubectl", "version", "--short")
	versionString := string(bytes)
	if err != nil {
		return versionStruct, fmt.Errorf("error running kubectl Version. Output: %s Error: %v", versionString, err)
	}

	clientAndServerVersionStrings := strings.Split(versionString, "\n")
	for _, ver := range clientAndServerVersionStrings {
		if strings.HasPrefix(ver, clientPrefix) {
			version, err := extractVersionNumber(ver, clientPrefix)
			if err != nil {
				return nil, err
			}
			versionStruct.Client = version

		} else if strings.HasPrefix(ver, serverPrefix) {
			version, err := extractVersionNumber(ver, serverPrefix)
			if err != nil {
				return nil, err
			}
			versionStruct.Server = version
		}
	}
	return versionStruct, nil
}

func extractVersionNumber(versionString string, prefix string) ([3]int, error) {
	var version [3]int
	justTheVersionString := strings.TrimPrefix(versionString, prefix)
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
		return fmt.Errorf("kubectl proxy already running on port [%d]", kctl.ProxyPort())
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

func (kctl *kubectl) SelfCheck() []*healthcheckPb.CheckResult {
	return []*healthcheckPb.CheckResult{
		kctl.checkKubectlOnPath(),
		kctl.checkKubectlVersion(),
		kctl.checkKubectlApiAccess(),
	}
}

func (kctl *kubectl) checkKubectlOnPath() *healthcheckPb.CheckResult {
	checkResult := &healthcheckPb.CheckResult{
		Status:           healthcheckPb.CheckStatus_OK,
		SubsystemName:    KubectlSubsystemName,
		CheckDescription: KubectlIsInstalledCheckDescription,
	}

	_, err := kctl.sh.CombinedOutput("kubectl", "config")
	if err != nil {
		checkResult.Status = healthcheckPb.CheckStatus_ERROR
		checkResult.FriendlyMessageToUser = fmt.Sprintf("Could not run the command `kubectl`. The error message is: [%s] and the current $PATH is: %s", err.Error(), kctl.sh.Path())
		return checkResult
	}

	return checkResult
}

func (kctl *kubectl) checkKubectlVersion() *healthcheckPb.CheckResult {
	checkResult := &healthcheckPb.CheckResult{
		Status:           healthcheckPb.CheckStatus_OK,
		SubsystemName:    KubectlSubsystemName,
		CheckDescription: KubectlVersionCheckDescription,
	}

	actualVersion, err := kctl.Version()
	if err != nil {
		checkResult.Status = healthcheckPb.CheckStatus_ERROR
		checkResult.FriendlyMessageToUser = fmt.Sprintf("Error getting version from kubectl. The error message is: [%s].", err.Error())
		return checkResult
	}
	clientVersion := actualVersion.Client

	if !isCompatibleVersion(minimumKubectlVersionExpected, clientVersion) {
		checkResult.Status = healthcheckPb.CheckStatus_FAIL
		checkResult.FriendlyMessageToUser = fmt.Sprintf("Kubectl is on version [%d.%d.%d], but version [%d.%d.%d] or more recent is required.",
			clientVersion[0], clientVersion[1], clientVersion[2],
			minimumKubectlVersionExpected[0], minimumKubectlVersionExpected[1], minimumKubectlVersionExpected[2])
		return checkResult
	}

	return checkResult
}

func (kctl *kubectl) checkKubectlApiAccess() *healthcheckPb.CheckResult {
	kubectlApiAccessCheck := &healthcheckPb.CheckResult{
		Status:           healthcheckPb.CheckStatus_OK,
		SubsystemName:    KubectlSubsystemName,
		CheckDescription: KubectlConnectivityCheckDescription,
	}

	output, err := kctl.sh.CombinedOutput("kubectl", "get", "pods")
	if err != nil {
		kubectlApiAccessCheck.Status = healthcheckPb.CheckStatus_FAIL
		kubectlApiAccessCheck.FriendlyMessageToUser = output
		return kubectlApiAccessCheck
	}

	kubectlApiAccessCheck.Status = healthcheckPb.CheckStatus_OK
	return kubectlApiAccessCheck
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

func NewKubectl(shell shell.Shell) (Kubectl, error) {

	kubectl := &kubectl{
		sh:        shell,
		proxyPort: portWhenProxyNotRunning,
	}

	actualVersion, err := kubectl.Version()
	clientVersion := actualVersion.Client

	if err != nil {
		return nil, err
	}

	if !isCompatibleVersion(minimumKubectlVersionExpected, clientVersion) {
		return nil, fmt.Errorf(
			"kubectl is on version [%d.%d.%d], but version [%d.%d.%d] or more recent is required",
			clientVersion[0], clientVersion[1], clientVersion[2],
			minimumKubectlVersionExpected[0], minimumKubectlVersionExpected[1], minimumKubectlVersionExpected[2])
	}

	return kubectl, nil

}
