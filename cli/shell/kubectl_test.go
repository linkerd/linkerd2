package shell

import (
	"testing"
	"fmt"
	"strings"
	"errors"
	"bufio"
	"time"
)

type mockShell struct {
	lastNameCalled string
	lastArgsCalled []string
	outputToReturn string
	errToReturn    error
}

func (sh *mockShell) lastFullCommand() string {
	return fmt.Sprintf("%s %s", sh.lastNameCalled, strings.Join(sh.lastArgsCalled, " "))
}

func (sh *mockShell) CombinedOutput(name string, arg ...string) (string, error) {
	sh.lastNameCalled = name
	sh.lastArgsCalled = arg

	return sh.outputToReturn, sh.errToReturn
}

func (sh *mockShell) AsyncStdout(name string, arg ...string) (*bufio.Reader, chan error) {
	sh.lastNameCalled = name
	sh.lastArgsCalled = arg
	e := make(chan error, 1)
	e <- sh.errToReturn
	return bufio.NewReader(strings.NewReader(sh.outputToReturn)), e
}

func (sh *mockShell) WaitForCharacter(charToWaitFor byte, outputReader *bufio.Reader, timeout time.Duration) (string, error) {
	return outputReader.ReadString(charToWaitFor)
}

func TestKubectlVersion(t *testing.T) {
	t.Run("Returns some Version as a smoke test", func(t *testing.T) {
		kctl := MakeKubectl(MakeUnixShell())
		_, err := kctl.Version()

		if err != nil {
			t.Fatalf("Error running command: %v", err)
		}
	})

	t.Run("Correctly parses a Version string", func(t *testing.T) {
		versions := map[string][3]int{
			"Client Version: v1.8.4": {1, 8, 4},
			"Client Version: v1.7.1": {1, 7, 1},
			"Client Version: v0.0.1": {0, 0, 1},
			"Client Version: v1.9.0-beta.2": {1,9,0},
		}

		shell := &mockShell{}
		kctl := MakeKubectl(shell)
		for k, expectedVersion := range versions {
			shell.outputToReturn = k
			actualVersion, err := kctl.Version()

			if err != nil {
				t.Fatalf("Error parsing string: %v", err)
			}

			if actualVersion != expectedVersion {
				t.Fatalf("Expecting %s to be parsed into %v but got %v", k, expectedVersion, actualVersion)
			}
		}
	})

	t.Run("Returns error if Version string looks broken", func(t *testing.T) {
		versions := []string{
			"",
			"Client Version: 1.8.4",
			"Client Version: 1.8.",
			"Client Version",
			"Client Version: Version.Info{Major:\"1\", Minor:\"8\", GitVersion:\"v1.8.4\", GitCommit:\"9befc2b8928a9426501d3bf62f72849d5cbcd5a3\", GitTreeState:\"clean\", BuildDate:\"2017-11-20T05:28:34Z\", GoVersion:\"go1.8.3\", Compiler:\"gc\", Platform:\"darwin/amd64\"}",
		}

		shell := &mockShell{}
		kctl := MakeKubectl(shell)
		for _, expectedVersion := range versions {
			shell.outputToReturn = expectedVersion
			_, err := kctl.Version()

			if err == nil {
				t.Fatalf("Expected error parsing string: %s", expectedVersion)
			}
		}
	})
}

func TestKubectlProxy(t *testing.T) {
	t.Run("Starts a proxy when no previous proxy was running", func(t *testing.T) {
		shell := &mockShell{}
		shell.outputToReturn = fmt.Sprintf("Starting to serve on 127.0.0.1:8001%c", magicCharacterThatIndicatesProxyIsRunning)
		kctl := MakeKubectl(shell)

		_, err := kctl.StartProxy(kubectlDefaultProxyPort)

		if err != nil {
			t.Fatalf("Unexpected error starting proxy: %v", err)
		}

		if kctl.ProxyPort() != kubectlDefaultProxyPort {
			t.Fatalf("Expecting proxy to be running on [%d] but it's on [%d]", kubectlDefaultProxyPort, kctl.ProxyPort)
		}

		if shell.lastFullCommand() != "kubectl proxy -p 8001" {
			t.Fatalf("Expecting kubectl to send correct command to Shell, sent [%s]", shell.lastFullCommand())
		}
	})

	t.Run("Returns error if there was already a proxy running, keeps old proxy running", func(t *testing.T) {
		shell := &mockShell{}
		shell.outputToReturn = fmt.Sprintf("Starting to serve on 127.0.0.1:8001%c", magicCharacterThatIndicatesProxyIsRunning)
		kctl := MakeKubectl(shell)

		_, err := kctl.StartProxy(kubectlDefaultProxyPort)

		if err != nil {
			t.Fatalf("Unexpected error starting proxy: %v", err)
		}

		_, err = kctl.StartProxy(kubectlDefaultProxyPort)

		if err == nil {
			t.Fatalf("Expected error trying to start proxy again, got nothing")
		}

		if kctl.ProxyPort() != kubectlDefaultProxyPort {
			t.Fatalf("Expected proxy to keep running on port [%d] but got [%d]", kubectlDefaultProxyPort, kctl.ProxyPort)
		}
	})

	t.Run("Returns error if a proxy had already been started by some other process", func(t *testing.T) {
		shell := &mockShell{}
		shell.errToReturn = errors.New("F1213 17:30:50.272013   39247 proxy.go:153] listen tcp 127.0.0.1:8001: bind: address already in use")
		kctl := MakeKubectl(shell)

		_, err := kctl.StartProxy(kubectlDefaultProxyPort)

		if err == nil {
			t.Fatalf("Expected error trying to start proxy again, got nothing")
		}
	})

	t.Run("Returns error if cannot detect that proxy has been started", func(t *testing.T) {
		shell := &mockShell{}
		shell.outputToReturn = "ANY STRING THAT DOEST CONTAIN THE MAGIC CHARACTER WE ARE LOOKING FOR"
		kctl := MakeKubectl(shell)

		_, err := kctl.StartProxy(kubectlDefaultProxyPort)

		if err == nil {
			t.Fatalf("Expected error trying to start proxy again, got nothing")
		}
	})
}

func TestUrlFor(t *testing.T) {
	t.Run("Generates expected URL if proxy is running", func(t *testing.T) {
		shell := &mockShell{}
		shell.outputToReturn = fmt.Sprintf("Starting to serve on 127.0.0.1:8001%c", magicCharacterThatIndicatesProxyIsRunning)
		kctl := MakeKubectl(shell)

		_, err := kctl.StartProxy(kubectlDefaultProxyPort)
		if err != nil {
			t.Fatalf("Unexpected error starting proxy: %v", err)
		}

		expectedNamespace := "expected-namespace"
		expectedPath := "/expected/path:for/desired/endpoint"
		expectedUrl := fmt.Sprintf("http://127.0.0.1:%d/api/v1/namespaces/%s%s", kubectlDefaultProxyPort, expectedNamespace, expectedPath)

		actualUrl, err := kctl.UrlFor(expectedNamespace, expectedPath)
		if err != nil {
			t.Fatalf("Unexpected error generating URL: %v", err)
		}

		if actualUrl != expectedUrl {
			t.Fatalf("Expected generated URL to be [%s] but was [%s]", expectedUrl, actualUrl)
		}
	})

	t.Run("Returns error if proxy isn't running", func(t *testing.T) {
		shell := &mockShell{}
		shell.outputToReturn = fmt.Sprintf("Starting to serve on 127.0.0.1:8001%c", magicCharacterThatIndicatesProxyIsRunning)
		kctl := MakeKubectl(shell)

		_, err := kctl.UrlFor("someNamespace", "/somePath")
		if err == nil {
			t.Fatalf("Expected error getting URL before starting proxy, got nothing")
		}
	})
}
