package k8s

import (
	"testing"

	"github.com/runconduit/conduit/pkg/shell"
	"fmt"
	"net/url"
)

type TestFixture struct{
	testInput string
	expectedOutput string
}
func TestKubernetesApiUrlFor(t *testing.T) {
	const namespace = "some-namespace"
	const extraPath = "/some/extra/path"
	testData := []TestFixture{
		{
			testInput: "https://35.184.231.31",
			expectedOutput:  fmt.Sprintf("https://35.184.231.31/api/v1/namespaces/%s%s", namespace, extraPath),
		},
		{
			testInput: "35.184.231.31",
			expectedOutput:  fmt.Sprintf("https://35.184.231.31/api/v1/namespaces/%s%s", namespace, extraPath),
		},
		{
			testInput: "http://35.184.231.31",
			expectedOutput:  fmt.Sprintf("http://35.184.231.31/api/v1/namespaces/%s%s", namespace, extraPath),
		},
	}

	t.Run("Returns URL from base URL overridden in construction", func(t *testing.T) {
		for _, data := range testData {
			actualUrl := generateURL(data, t, namespace, extraPath)
			if actualUrl.String() != data.expectedOutput {
				t.Fatalf("Expected generated URL to be [%s], but got [%s]", data.expectedOutput, actualUrl.String())
			}
		}
	})

	t.Run("Returns unmodified URL from base URL overridden in construction", func(t *testing.T){
		/* NewK8sAPI is agnostic to the scheme used for the override API. It is only contains code that conveniently
		adds the default scheme used to connect with the k8s API (https). Supplying a URL with a scheme other than https
		should return an unmodified override URL */
		testData := []TestFixture {
			{
				testInput: "htp://35.184.231.31",
				expectedOutput:  fmt.Sprintf("htp://35.184.231.31/api/v1/namespaces/%s%s", namespace, extraPath),
			},
			{
				testInput: "tcp://35.184.231.31",
				expectedOutput:  fmt.Sprintf("tcp://35.184.231.31/api/v1/namespaces/%s%s", namespace, extraPath),
			},
			{
				testInput: "htttpp://35.184.231.31",
				expectedOutput:  fmt.Sprintf("htttpp://35.184.231.31/api/v1/namespaces/%s%s", namespace, extraPath),
			},
		}
		for _, data := range testData {
			actualUrl := generateURL(data, t, namespace, extraPath)
			if actualUrl.String() != data.expectedOutput {
				t.Fatalf("Expected generated URL to be [%s], but got [%s]", data.expectedOutput, actualUrl.String())
			}

		}
	})

	t.Run("Return error on malformed URL from base URL during API construction", func(t *testing.T) {
		testData := []string{"http://**&^^.(&(^"}
		for _, data := range testData {
			_, err := NewK8sAPI(shell.NewUnixShell(), "testdata/config.test", data)
			if err == nil {
				t.Fatalf("Expected error from proxy on malformed URL: %s", data)
			}
		}
	})
}
func generateURL(data TestFixture, t *testing.T, namespace string, extraPath string) *url.URL {
	api, err := NewK8sAPI(shell.NewUnixShell(), "testdata/config.test", data.testInput)
	if err != nil {
		t.Fatalf("Unexpected error starting proxy: %v", err)
	}
	actualUrl, err := api.UrlFor(namespace, extraPath)
	if err != nil {
		t.Fatalf("Unexpected error starting proxy: %v", err)
	}
	return actualUrl
}
