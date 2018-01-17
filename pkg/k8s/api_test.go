package k8s

import (
	"fmt"
	"testing"

	"github.com/runconduit/conduit/pkg/shell"
)

func TestKubernetesApiUrlFor(t *testing.T) {
	const namespace = "some-namespace"
	const extraPath = "/some/extra/path"

	t.Run("Returns base config containing k8s endpoint listed in config.test", func(t *testing.T) {
		expected := fmt.Sprintf("https://55.197.171.239/api/v1/namespaces/%s%s", namespace, extraPath)
		shell := &shell.MockShell{}
		api, err := NewK8sAPI(shell, "testdata/config.test")
		if err != nil {
			t.Fatalf("Unexpected error starting proxy: %v", err)
		}
		actualUrl, err := api.UrlFor(namespace, extraPath)
		if err != nil {
			t.Fatalf("Unexpected error starting proxy: %v", err)
		}
		if actualUrl.String() != expected {
			t.Fatalf("Expected generated URL to be [%s], but got [%s]", expected, actualUrl.String())
		}
	})
}
