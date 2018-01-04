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
	errorToReturn                         error
}

func (m *MockKubeApi) UrlFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error) {
	m.UrlForNamespaceReceived = namespace
	m.UrlExtraPathStartingWithSlashReceived = extraPathStartingWithSlash
	return m.UrlForUrlToReturn, m.errorToReturn
}

func (m *MockKubeApi) NewClient() (*http.Client, error) {
	return m.NewClientClientToReturn, m.errorToReturn
}

func (m *MockKubeApi) SelfCheck() ([]healthcheck.CheckResult, error) {
	return m.SelfCheckResultsToReturn, m.errorToReturn
}
