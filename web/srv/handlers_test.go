package srv

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	helpers "github.com/linkerd/linkerd2/pkg/profiles"
	"sigs.k8s.io/yaml"
)

const releaseVersion = "0.3.3"

func TestHandleIndex(t *testing.T) {
	server := FakeServer()

	handler := &handler{
		render:  server.RenderTemplate,
		version: releaseVersion,
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

	actualBody := recorder.Body.String()

	expectedSubstrings := []string{
		"<div class=\"main\" id=\"main\"",
		"data-release-version=\"" + releaseVersion + "\"",
		"data-controller-namespace=\"\"",
		"data-uuid=\"\"",
	}
	for _, expectedSubstring := range expectedSubstrings {
		if !strings.Contains(actualBody, expectedSubstring) {
			t.Fatalf("Expected string [%s] to be present in [%s]", expectedSubstring, actualBody)
		}
	}
}

func TestHandleConfigDownload(t *testing.T) {
	server := FakeServer()

	handler := &handler{
		render:              server.RenderTemplate,
		controllerNamespace: "linkerd",
		clusterDomain:       "mycluster.local",
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/profiles/new?service=authors&namespace=booksns", nil)

	handler.handleProfileDownload(recorder, req, httprouter.Params{})

	if recorder.Code != http.StatusOK {
		t.Errorf("Incorrect StatusCode: %+v", recorder.Code)
		t.Errorf("Expected              %+v", http.StatusOK)
	}

	header := http.Header{
		"Content-Type": []string{
			"text/yaml",
		},
		"Content-Disposition": []string{
			"attachment; filename=authors-profile.yml",
		},
	}
	if !reflect.DeepEqual(recorder.Header(), header) {
		t.Errorf("Incorrect headers: %+v", recorder.Header())
		t.Errorf("Expected:          %+v", header)
	}

	var serviceProfile v1alpha2.ServiceProfile
	err := yaml.Unmarshal(recorder.Body.Bytes(), &serviceProfile)
	if err != nil {
		t.Fatalf("Error parsing service profile: %v", err)
	}

	expectedServiceProfile := helpers.GenServiceProfile("authors", "booksns", "mycluster.local")

	err = helpers.ServiceProfileYamlEquals(serviceProfile, expectedServiceProfile)
	if err != nil {
		t.Fatalf("ServiceProfiles are not equal: %v", err)
	}
}
