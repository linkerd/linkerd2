package k8s

import (
	"net/http"
	"net/url"

	pb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
)

type MockKubeApi struct {
	SelfCheckResultsToReturn              []*pb.CheckResult
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

func (m *MockKubeApi) SelfCheck() []*pb.CheckResult {
	return m.SelfCheckResultsToReturn
}

type MockKubectl struct {
	SelfCheckResultsToReturn []*pb.CheckResult
}

func (m *MockKubectl) Version() ([3]int, error) { return [3]int{}, nil }

func (m *MockKubectl) StartProxy(potentialErrorWhenStartingProxy chan error, port int) error {
	return nil
}

func (m *MockKubectl) UrlFor(namespace string, extraPathStartingWithSlash string) (*url.URL, error) {
	return nil, nil
}

func (m *MockKubectl) ProxyPort() int { return -666 }

func (m *MockKubectl) SelfCheck() []*pb.CheckResult {
	return m.SelfCheckResultsToReturn
}
