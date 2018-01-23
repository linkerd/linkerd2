package k8s

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"log"

	healthCheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	"github.com/runconduit/conduit/pkg/shell"
	"k8s.io/client-go/rest"
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

func TestKubernetesAPIHealthCheck(t *testing.T) {

	t.Run("Returns CheckStatus_OK on HTTP 200 Response from conduit dashboard", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		api := kubernetesApi{
			Config: &rest.Config{
				Host: ts.URL,
			},
		}
		defer ts.Close()
		actual := api.checkDashboardAccess(ts.Client())
		assert(t, actual.Status == healthCheckPb.CheckStatus_OK, "Expected check status to return ok on HTTP 200 response")
	})

	t.Run("Returns CheckStatus_FAIL on HTTP Response other than 200 from conduit dashboard", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
		}))
		api := kubernetesApi{
			Config: &rest.Config{
				Host: ts.URL,
			},
		}
		defer ts.Close()
		actual := api.checkDashboardAccess(ts.Client())
		assert(t, actual.Status == healthCheckPb.CheckStatus_FAIL, "Expected check status to return FAIL")
	})

}
func assert(t *testing.T, condition bool, msg string) {
	if !condition {
		log.Printf("Failed: %s", msg)
		t.Fail()
	}
	fmt.Println(msg)

}
