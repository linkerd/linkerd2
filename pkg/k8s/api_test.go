package k8s

import (
	"testing"

	"github.com/runconduit/conduit/pkg/shell"
	"fmt"
)

func TestKubernetesApiUrlFor(t *testing.T) {
	const namespace = "some-namespace"
	const extraPath = "/some/extra/path"
	testData := []struct {
		testInput string
		expected  string
	}{
		{
			testInput: "https://35.184.231.31",
			expected:  fmt.Sprintf("https://35.184.231.31/api/v1/namespaces/%s%s", namespace, extraPath),
		},
		{
			testInput: "35.184.231.31",
			expected:  fmt.Sprintf("https://35.184.231.31/api/v1/namespaces/%s%s", namespace, extraPath),
		},
		{
			testInput: "http://35.184.231.31",
			expected:  fmt.Sprintf("http://35.184.231.31/api/v1/namespaces/%s%s", namespace, extraPath),
		},
	}

	t.Run("Returns URL from base URL overridden in construction", func(t *testing.T) {
		for _, data := range testData {
			api, err := NewK8sAPI(shell.NewUnixShell(), "testdata/config.test", data.testInput)
			if err != nil {
				t.Fatalf("Unexpected error starting proxy: %v", err)
			}
			actualUrl, err := api.UrlFor(namespace, extraPath)
			if err != nil {
				t.Fatalf("Unexpected error starting proxy: %v", err)
			}
			if actualUrl.String() != data.expected {
				t.Fatalf("Expected generated URL to be [%s], but got [%s]", data.expected, actualUrl.String())
			}
		}
	})
}
