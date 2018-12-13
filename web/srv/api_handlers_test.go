package srv

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/controller/api/public"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
)

func TestHandleApiVersion(t *testing.T) {
	mockAPIClient := &public.MockAPIClient{
		VersionInfoToReturn: &pb.VersionInfo{
			GoVersion:      "the best one",
			BuildDate:      "never",
			ReleaseVersion: "0.3.3",
		},
	}
	server := FakeServer()

	handler := &handler{
		render:    server.RenderTemplate,
		apiClient: mockAPIClient,
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/version", nil)
	handler.handleAPIVersion(recorder, req, httprouter.Params{})

	if recorder.Code != http.StatusOK {
		t.Errorf("Incorrect StatusCode: %+v", recorder.Code)
		t.Errorf("Expected              %+v", http.StatusOK)
	}

	header := http.Header{
		"Content-Type": []string{"application/json"},
	}
	if !reflect.DeepEqual(recorder.Header(), header) {
		t.Errorf("Incorrect headers: %+v", recorder.Header())
		t.Errorf("Expected:          %+v", header)
	}

	jsonResult := recorder.Body.String()
	expectedVersionJSON := "{\"version\":{\"goVersion\":\"the best one\",\"buildDate\":\"never\",\"releaseVersion\":\"0.3.3\"}}"

	if !strings.Contains(jsonResult, expectedVersionJSON) {
		t.Errorf("incorrect api result")
		t.Errorf("Got: %+v", jsonResult)
		t.Errorf("Expected to find: %+v", expectedVersionJSON)
	}
}
