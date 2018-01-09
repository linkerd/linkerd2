package k8s

import (
	"testing"

	"github.com/runconduit/conduit/pkg/shell"
)

func TestKubernetesApiUrlFor(t *testing.T) {
	const namespace = "some-namespace"
	const extraPath = "/some/extra/path"

	t.Run("Returns URL from base URL overridden in construction", func(t *testing.T) {
		testUrl := "https://35.184.231.31"
		expectedUrlString := "https://35.184.231.31/api/v1/namespaces/some-namespace/some/extra/path"
		generateUrlTest(t, namespace, extraPath, testUrl, expectedUrlString)
	})

	t.Run("Returns URL from base URL with no https prefix in URL override", func(t *testing.T) {
		testUrl := "35.184.231.31"
		expectedUrlString := "https://35.184.231.31/api/v1/namespaces/some-namespace/some/extra/path"
		generateUrlTest(t, namespace, extraPath, testUrl, expectedUrlString)
	})

	t.Run("Returns URL from base URL with with http prefix in URL override", func(t *testing.T) {
		testUrl := "http://35.184.231.31"
		expectedUrlString := "http://35.184.231.31/api/v1/namespaces/some-namespace/some/extra/path"
		generateUrlTest(t, namespace, extraPath, testUrl, expectedUrlString)
	})
}

func generateUrlTest(t *testing.T, namespace string, extraPath string, testUrl string,  expectedUrlString string) {
	api, err := NewK8sAPI(shell.NewUnixShell(), "testdata/config.test", testUrl)
	if err != nil {
		t.Fatalf("Unexpected error starting proxy: %v", err)
	}
	actualUrl, err := api.UrlFor(namespace, extraPath)
	if err != nil {
		t.Fatalf("Unexpected error starting proxy: %v", err)
	}
	if actualUrl.String() != expectedUrlString {
		t.Fatalf("Expected generated URL to be [%s], but got [%s]", expectedUrlString, actualUrl.String())
	}
}
