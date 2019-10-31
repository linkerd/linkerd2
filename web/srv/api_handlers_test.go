package srv

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/controller/api/public"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type mockHealthChecker struct {
	results []*healthcheck.CheckResult
}

func (c *mockHealthChecker) RunChecks(observer healthcheck.CheckObserver) bool {
	for _, result := range c.results {
		observer(result)
	}
	return true
}

func TestHandleApiCheck(t *testing.T) {
	// Setup handler using a mock health checker
	mockResults := []*healthcheck.CheckResult{
		&healthcheck.CheckResult{
			Category:    healthcheck.LinkerdConfigChecks,
			Description: "check3-description",
			HintAnchor:  "check3-hint-anchor",
			Warning:     false,
			Err:         nil,
		},
		&healthcheck.CheckResult{
			Category:    healthcheck.LinkerdConfigChecks,
			Description: "check4-description-kubectl",
			HintAnchor:  "check4-hint-anchor",
			Warning:     true,
			Err:         nil,
		},
		&healthcheck.CheckResult{
			Category:    healthcheck.KubernetesAPIChecks,
			Description: "check1-description",
			HintAnchor:  "check1-hint-anchor",
			Warning:     false,
			Err:         nil,
		},
		&healthcheck.CheckResult{
			Category:    healthcheck.KubernetesAPIChecks,
			Description: "check2-description",
			HintAnchor:  "check2-hint-anchor",
			Warning:     true,
			Err:         errors.New("check2-error"),
		},
	}
	h := &handler{
		hc: &mockHealthChecker{
			results: mockResults,
		},
	}

	// Handle request recording the response
	req := httptest.NewRequest("GET", "/api/check", nil)
	w := httptest.NewRecorder()
	h.handleAPICheck(w, req, httprouter.Params{})
	resp := w.Result()
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)

	// Check we receive the headers and body expected
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	apiCheckOutputGolden, err := ioutil.ReadFile("testdata/api_check_output.json")
	require.NoError(t, err)
	apiCheckOutputGoldenCompact := &bytes.Buffer{}
	err = json.Compact(apiCheckOutputGoldenCompact, apiCheckOutputGolden)
	require.NoError(t, err)
	assert.Equal(t, apiCheckOutputGoldenCompact.Bytes(), body)
}
