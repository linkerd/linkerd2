package k8s

import (
	"net/http"
	"net/url"

	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
)

type MockKubeApi struct {
	SelfCheckResultsToReturn              []*healthcheckPb.CheckResult
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

func (m *MockKubeApi) SelfCheck() []*healthcheckPb.CheckResult {
	return m.SelfCheckResultsToReturn
}

type MockKubectl struct {
	SelfCheckResultsToReturn []*healthcheckPb.CheckResult
}

func (m *MockKubectl) Version() ([3]int, error) { return [3]int{}, nil }

func (m *MockKubectl) SelfCheck() []*healthcheckPb.CheckResult {
	return m.SelfCheckResultsToReturn
}
