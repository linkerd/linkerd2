package k8s

import (
	"testing"
)

func TestKubernetesApiUrlFor(t *testing.T) {
	const namespace = "some-namespace"
	const extraPath = "/some/extra/path"

	t.Run("Returns base config containing k8s endpoint listed in config.test", func(t *testing.T) {
		tests := []struct {
			kubeContext string
			expected    string
		}{
			{
				kubeContext: "",
				expected:    "https://55.197.171.239/api/v1/namespaces/some-namespace/some/extra/path",
			},
			{
				kubeContext: "clusterTrailingSlash",
				expected:    "https://162.128.50.11/api/v1/namespaces/some-namespace/some/extra/path",
			},
			{
				kubeContext: "clusterWithPath",
				expected:    "https://162.128.50.12/k8s/clusters/c-fhjws/api/v1/namespaces/some-namespace/some/extra/path",
			},
		}

		for _, test := range tests {
			api, err := NewAPI("testdata/config.test", test.kubeContext, 0)
			if err != nil {
				t.Fatalf("Unexpected error creating Kubernetes API: %+v", err)
			}
			actualURL, err := api.URLFor(namespace, extraPath)
			if err != nil {
				t.Fatalf("Unexpected error generating URL: %+v", err)
			}
			if actualURL.String() != test.expected {
				t.Fatalf("Expected generated URL to be [%s], but got [%s]", test.expected, actualURL.String())
			}
		}
	})
}
