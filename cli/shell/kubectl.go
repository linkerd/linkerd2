package shell

import (
	"fmt"
	"errors"
	"strings"
	"strconv"
	"time"
)

type Kubectl interface {
	Version() ([3]int, error)
	StartProxy(port int) (chan error, error)
	UrlFor(namespace string, extraPathStartingWithSlash string) (string,error)
	ProxyPort() int
}

type kubectl struct {
	sh        Shell
	proxyPort int
}

const (
	kubectlDefaultProxyPort                   = 8001
	kubectlDefaultTimeout                     = 10 * time.Second
	portWhenProxyNotRunning                   = -1
	magicCharacterThatIndicatesProxyIsRunning = '\n'
)

func (kctl *kubectl) ProxyPort() int {
	return kctl.proxyPort
}

func (kctl *kubectl) Version() ([3]int, error) {
	var version [3]int
	bytes, err := kctl.sh.CombinedOutput("kubectl", "version", "--client", "--short")
	versionString := string(bytes)
	if err != nil {
		return [3]int{}, errors.New(fmt.Sprintf("Error running kubectl Version. Output: %s Error: %v", versionString, err))
	}

	if err != nil {
		return version, err
	}

	split := strings.Split(strings.TrimPrefix(versionString, "Client Version: v"), ".")

	if len(split) != 3 {
		return version, errors.New(fmt.Sprintf("Unknown Version string format from Kubectl: [%s] not enough segments: %s", versionString, split))
	}

	for i, segment := range split {
		v, err := strconv.Atoi(strings.TrimSpace(segment))
		if err != nil {
			return version, errors.New(fmt.Sprintf("Unknown Version string format from Kubectl: [%s], not an integer: [%s]", versionString, segment))
		}
		version[i] = v
	}

	return version, nil
}

func (kctl *kubectl) StartProxy(port int) (chan error, error) {
	fmt.Printf("Running `kubectl proxy %d`\n", port)

	if kctl.ProxyPort() != portWhenProxyNotRunning {
		return nil, errors.New(fmt.Sprintf("Kubectl proxy already running on port [%d]", kctl.ProxyPort))
	}
	output, errorReturnedByProcess := kctl.sh.AsyncStdout("kubectl", "proxy", "-p", strconv.Itoa(port))

	kubectlOutput, err :=kctl.sh.WaitForCharacter(magicCharacterThatIndicatesProxyIsRunning, output, kubectlDefaultTimeout)

	fmt.Println(kubectlOutput)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Error waiting for kubectl to start the proxy. kubetl returned [%s], error: %v", kubectlOutput, err))
	}

	kctl.proxyPort = kubectlDefaultProxyPort
	return errorReturnedByProcess, nil
}

func (kctl *kubectl) UrlFor(namespace string, extraPathStartingWithSlash string) (string,error) {
	if kctl.ProxyPort() == portWhenProxyNotRunning {
		return "", errors.New("proxy needs to be started before generating URLs")
	}

	url := fmt.Sprintf("http://%s:%d/api/v1/namespaces/%s%s", "127.0.0.1", kctl.ProxyPort(), namespace, extraPathStartingWithSlash)
	return url, nil
}

func MakeKubectl(shell Shell) Kubectl {
	return &kubectl{
		sh:        shell,
		proxyPort: portWhenProxyNotRunning,
	}
}
