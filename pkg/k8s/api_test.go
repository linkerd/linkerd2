package k8s

import (
	"fmt"
	"testing"
)

func TestKubernetesApiUrlFor(t *testing.T) {
	const namespace = "some-namespace"
	const extraPath = "/some/extra/path"

	t.Run("Returns base config containing k8s endpoint listed in config.test", func(t *testing.T) {
		expected := fmt.Sprintf("https://55.197.171.239/api/v1/namespaces/%s%s", namespace, extraPath)
		api, err := NewAPI("testdata/config.test", "")
		if err != nil {
			t.Fatalf("Unexpected error creating Kubernetes API: %+v", err)
		}
		actualURL, err := api.URLFor(namespace, extraPath)
		if err != nil {
			t.Fatalf("Unexpected error generating URL: %+v", err)
		}
		if actualURL.String() != expected {
			t.Fatalf("Expected generated URL to be [%s], but got [%s]", expected, actualURL.String())
		}
	})
}
