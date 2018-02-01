package srv

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/runconduit/conduit/controller/api/public"
	pb "github.com/runconduit/conduit/controller/gen/public"
)

func TestHandleIndex(t *testing.T) {
	mockApiClient := &public.MockConduitApiClient{
		VersionInfoToReturn: &pb.VersionInfo{
			GoVersion:      "the best one",
			BuildDate:      "never",
			ReleaseVersion: "0.3.3",
		},
	}

	server := FakeServer()

	handler := &handler{
		render:    server.RenderTemplate,
		apiClient: mockApiClient,
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	handler.handleIndex(recorder, req, httprouter.Params{})

	if recorder.Code != http.StatusOK {
		t.Errorf("Incorrect StatusCode: %+v", recorder.Code)
		t.Errorf("Expected              %+v", http.StatusOK)
	}

	header := http.Header{
		"Content-Type": []string{"text/html"},
	}
	if !reflect.DeepEqual(recorder.Header(), header) {
		t.Errorf("Incorrect headers: %+v", recorder.Header())
		t.Errorf("Expected:          %+v", header)
	}

	expectedVersionDiv := "<div class=\"main\" id=\"main\" data-release-version=\"0.3.3\" data-go-version=\"the best one\" data-uuid=\"\">"

	actualBody := recorder.Body.String()

	if !strings.Contains(actualBody, expectedVersionDiv) {
		t.Fatalf("Expected string [%s] to be present in [%s]", expectedVersionDiv, actualBody)
	}
}
