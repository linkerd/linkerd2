package dashboard

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	"github.com/runconduit/conduit/pkg/k8s"
	log "github.com/sirupsen/logrus"
)

func TestKubernetesAPIHealthCheck(t *testing.T) {

	t.Run("Returns CheckStatus_OK on HTTP 200 Response from conduit dashboard", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		mockKubeApi := k8s.MockKubeApi{NewClientClientToReturn: ts.Client()}
		handler, err := NewDashboardHandler("")
		if err != nil {
			log.Fatalf("Failed to instantiate a new DashboardHandler: %v", err)
		}
		defer ts.Close()
		actual := handler.checkDashboardAccess(mockKubeApi.NewClientClientToReturn)
		assert(t, actual.Status == healthcheckPb.CheckStatus_OK, "Expected check status to return ok on HTTP 200 response")
	})

	t.Run("Returns CheckStatus_FAIL on HTTP Response other than 200 from conduit dashboard", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
		}))
		handler, err := NewDashboardHandler("")
		if err != nil {
			log.Fatalf("Failed to instantiate a new DashboardHandler: %v", err)
		}
		defer ts.Close()
		actual := handler.checkDashboardAccess(ts.Client())
		assert(t, actual.Status == healthcheckPb.CheckStatus_FAIL, "Expected check status to return FAIL")
	})

}
func assert(t *testing.T, condition bool, msg string) {
	if !condition {
		log.Printf("Failed: %s", msg)
		t.Fail()
	}
	fmt.Println(msg)

}
