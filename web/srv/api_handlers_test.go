package srv

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/julienschmidt/httprouter"
)

func TestHandleApiVersion(t *testing.T) {
	var mockApiClient MockApiClient
	server := FakeServer()

	handler := &handler{
		render:    server.RenderTemplate,
		apiClient: mockApiClient,
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/version", nil)
	handler.handleApiVersion(recorder, req, httprouter.Params{})

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
	expectedVersionJson := "{\"version\":{\"goVersion\":\"the best one\",\"buildDate\":\"never\",\"releaseVersion\":\"0.3.3\"}}"

	if !strings.Contains(jsonResult, expectedVersionJson) {
		t.Errorf("incorrect api result")
		t.Errorf("Got: %+v", jsonResult)
		t.Errorf("Expected to find: %+v", expectedVersionJson)
	}
}

func TestHandleApiMetrics(t *testing.T) {
	var mockApiClient MockApiClient
	server := FakeServer()

	handler := &handler{
		render:    server.RenderTemplate,
		apiClient: mockApiClient,
	}

	// test that it returns an empty metrics response
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/metrics", nil)
	req.Form = url.Values{
		"target": []string{"hello"},
		"metric": []string{"requests"},
	}
	handler.handleApiMetrics(recorder, req, httprouter.Params{})
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
	expectedJson := "{\"metrics\":[]}"

	if !strings.Contains(jsonResult, expectedJson) {
		t.Errorf("incorrect api result")
		t.Errorf("Got: %+v", jsonResult)
		t.Errorf("Expected to find: %+v", expectedJson)
	}
}
