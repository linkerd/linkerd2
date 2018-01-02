package k8s

import (
	"testing"

	"github.com/runconduit/conduit/cli/shell"
)

func TestKubernetesApiUrlFor(t *testing.T) {
	t.Run("Returns URL from base URL overridden in construction", func(t *testing.T) {
		namespace := "some-namespace"
		extraPath := "/some/extra/path"
		expectedUrlString := "https://35.184.231.31/api/v1/namespaces/some-namespace/some/extra/path"

		api, err := NewK8sAPi(shell.NewUnixShell(), "config.test", "https://35.184.231.31")
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
	})
}
