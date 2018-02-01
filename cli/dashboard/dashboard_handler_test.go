package dashboard

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/runconduit/conduit/controller/gen/common/healthcheck"
	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
)

func TestIsDashboardAvailable(t *testing.T) {

	t.Run("Returns CheckStatus_OK on HTTP 200 Response from conduit dashboard", func(t *testing.T) {
		mockDashboardServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

		testUrl, err := url.Parse(mockDashboardServer.URL)
		if err != nil {
			t.Errorf("unable to generate test url: %v", err)
		}

		mockKubeApi := k8s.MockKubeApi{NewClientClientToReturn: mockDashboardServer.Client(), UrlForUrlToReturn: testUrl}
		dashboardHandler := NewDashboardHandler(&k8s.MockKubectl{}, &mockKubeApi)
		defer mockDashboardServer.Close()
		actual := dashboardHandler.IsDashboardAvailable()
		assert(t, actual == true, "Expected IsDashboardAvailable to be true")
	})

	t.Run("Returns CheckStatus_OK on HTTP 200 Response from conduit dashboard", func(t *testing.T) {
		mockDashboardServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))

		testUrl, err := url.Parse(mockDashboardServer.URL)
		if err != nil {
			t.Errorf("unable to generate test url: %v", err)
		}

		mockKubeApi := k8s.MockKubeApi{NewClientClientToReturn: mockDashboardServer.Client(), UrlForUrlToReturn: testUrl}
		handler := NewDashboardHandler(&k8s.MockKubectl{}, &mockKubeApi)
		defer mockDashboardServer.Close()
		actual := handler.IsDashboardAvailable()
		assert(t, actual == false, "Expected IsDashboardAvailable to be false on HTTP 500 request from dashboard web server")
	})
}

func TestCheckDashboardAccess(t *testing.T) {
	t.Run("Return CheckStatus_ERROR if kubeApi failes to generate URL", func(t *testing.T) {
		mockDashboardServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
		mockKubeApi := k8s.MockKubeApi{ErrorToReturn: errors.New("expected")}
		handler := NewDashboardHandler(&k8s.MockKubectl{}, &mockKubeApi)
		handler.kubeApi = &mockKubeApi
		actual := handler.checkDashboardAccess(mockDashboardServer.Client())
		assert(t,
			actual.Status == conduit_common_healthcheck.CheckStatus_ERROR,
			"Expected checkDashboardAccess to return CheckStatus_ERROR on malformed kubeApi Url")
	})
}
func assert(t *testing.T, condition bool, msg string) {
	if !condition {
		log.Printf("Failed: %s", msg)
		t.Fail()
	}
	fmt.Println(msg)

}
