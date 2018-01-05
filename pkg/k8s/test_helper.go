package k8s

import (
	"net/http"
	"net/url"

	"github.com/runconduit/conduit/pkg/healthcheck"
)

type MockKubeApi struct {
	SelfCheckResultsToReturn              []healthcheck.CheckResult
	UrlForNamespaceReceived               string
	UrlExtraPathStartingWithSlashReceived string
	UrlForUrlToReturn                     *url.URL
	NewClientClientToReturn               *http.Client
	ErrorToReturn                         error
}

func (m *MockKubeApi) UrlFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error) {
	m.UrlForNamespaceReceived = namespace
	m.UrlExtraPathStartingWithSlashReceived = extraPathStartingWithSlash
	return m.UrlForUrlToReturn, m.ErrorToReturn
}

func (m *MockKubeApi) NewClient() (*http.Client, error) {
	return m.NewClientClientToReturn, m.ErrorToReturn
}

func (m *MockKubeApi) SelfCheck() ([]healthcheck.CheckResult, error) {
	return m.SelfCheckResultsToReturn, m.ErrorToReturn
}

type MockKubectl struct {
	SelfCheckResultsToReturn []healthcheck.CheckResult
	ErrorToReturn            error
}

func (m *MockKubectl) Version() ([3]int, error) { return [3]int{}, nil }

func (m *MockKubectl) StartProxy(potentialErrorWhenStartingProxy chan error, port int) error {
	return nil
}

func (m *MockKubectl) UrlFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error) {
	return nil, nil
}

func (m *MockKubectl) ProxyPort() int { return -666 }

func (m *MockKubectl) SelfCheck() ([]healthcheck.CheckResult, error) {
	return m.SelfCheckResultsToReturn, m.ErrorToReturn
}
