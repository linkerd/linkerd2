package k8s

import (
	"fmt"
	"testing"
)

func TestKubernetesApiUrlFor(t *testing.T) {
	const namespace = "some-namespace"
	const extraPath = "/some/extra/path"

	t.Run("Returns base config containing k8s endpoint listed in config.test", func(t *testing.T) {
		tests := []struct {
			server      string
			kubeContext string
		}{
			{"https://55.197.171.239", ""},
			{"https://162.128.50.11", "clusterTrailingSlash"},
		}

		for _, test := range tests {
			expected := fmt.Sprintf("%s/api/v1/namespaces/%s%s", test.server, namespace, extraPath)
			api, err := NewAPI("testdata/config.test", test.kubeContext)
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
		}
	})
}
