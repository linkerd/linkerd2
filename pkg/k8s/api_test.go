package k8s

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/runconduit/conduit/pkg/shell"
)

func TestKubernetesApiUrlFor(t *testing.T) {
	const namespace = "some-namespace"
	const extraPath = "/some/extra/path"

	t.Run("Returns URL from base URL overridden in construction", func(t *testing.T) {
		testData := map[string]string{
			"https://35.184.231.31": fmt.Sprintf("https://35.184.231.31/api/v1/namespaces/%s%s", namespace, extraPath),
			"http://35.184.231.31":  fmt.Sprintf("http://35.184.231.31/api/v1/namespaces/%s%s", namespace, extraPath),
		}
		for testInput, expected := range testData {
			actualUrl := generateURL(testInput, t, namespace, extraPath)
			if actualUrl.String() != expected {
				t.Fatalf("Expected generated URL to be [%s], but got [%s]", expected, actualUrl.String())
			}
		}
	})
}

func generateURL(hostPort string, t *testing.T, namespace string, extraPath string) *url.URL {
	api, err := NewK8sAPI(shell.NewUnixShell(), "testdata/config.test", hostPort)
	if err != nil {
		t.Fatalf("Unexpected error starting proxy: %v", err)
	}
	actualUrl, err := api.UrlFor(namespace, extraPath)
	if err != nil {
		t.Fatalf("Unexpected error starting proxy: %v", err)
	}
	return actualUrl
}
