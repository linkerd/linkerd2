package srv

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-test/deep"
	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/pkg/healthcheck"
	k8sPkg "github.com/linkerd/linkerd2/pkg/k8s"
	vizApi "github.com/linkerd/linkerd2/viz/metrics-api"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	tappkg "github.com/linkerd/linkerd2/viz/tap/pkg"
	"k8s.io/client-go/rest"
)

type mockHealthChecker struct {
	results []*healthcheck.CheckResult
}

func (c *mockHealthChecker) RunChecks(observer healthcheck.CheckObserver) (bool, bool) {
	for _, result := range c.results {
		observer(result)
	}
	return true, false
}

func TestHandleApiCheck(t *testing.T) {
	// Setup handler using a mock health checker
	mockResults := []*healthcheck.CheckResult{
		{
			Category:    healthcheck.LinkerdConfigChecks,
			Description: "check3-description",
			HintURL:     healthcheck.DefaultHintBaseURL + "check3-hint-anchor",
			Warning:     false,
			Err:         nil,
		},
		{
			Category:    healthcheck.LinkerdConfigChecks,
			Description: "check4-description-kubectl",
			HintURL:     healthcheck.DefaultHintBaseURL + "check4-hint-anchor",
			Warning:     true,
			Err:         nil,
		},
		{
			Category:    healthcheck.KubernetesAPIChecks,
			Description: "check1-description",
			HintURL:     healthcheck.DefaultHintBaseURL + "check1-hint-anchor",
			Warning:     false,
			Err:         nil,
		},
		{
			Category:    healthcheck.KubernetesAPIChecks,
			Description: "check2-description",
			HintURL:     healthcheck.DefaultHintBaseURL + "check2-hint-anchor",
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("not expecting error reading response body but got: %v", err)
	}

	// Check we receive the headers and body expected
	expectedHeaders := http.Header{
		"Content-Type": []string{"application/json"},
	}
	if diff := deep.Equal(resp.Header, expectedHeaders); diff != nil {
		t.Errorf("Unexpected header: %v", diff)
	}
	apiCheckOutputGolden, err := os.ReadFile("testdata/api_check_output.json")
	if err != nil {
		t.Fatalf("not expecting error reading api check output golden file but got: %v", err)
	}
	apiCheckOutputGoldenCompact := &bytes.Buffer{}
	err = json.Compact(apiCheckOutputGoldenCompact, apiCheckOutputGolden)
	if err != nil {
		t.Fatalf("not expecting error compacting api check output golden file but got: %v", err)
	}
	if !bytes.Equal(body, apiCheckOutputGoldenCompact.Bytes()) {
		t.Errorf("expecting response body to be\n %s\n but got\n %s", apiCheckOutputGoldenCompact.Bytes(), body)
	}
}

func TestHandleApiGateway(t *testing.T) {
	mockAPIClient := &vizApi.MockAPIClient{
		GatewaysResponseToReturn: &pb.GatewaysResponse{
			Response: &pb.GatewaysResponse_Ok_{
				Ok: &pb.GatewaysResponse_Ok{
					GatewaysTable: &pb.GatewaysTable{
						Rows: []*pb.GatewaysTable_Row{
							{
								Namespace:   "test_namespace",
								Name:        "test_gateway",
								ClusterName: "multi_cluster",
								Alive:       true,
							},
						},
					},
				},
			},
		},
	}
	server := FakeServer()

	handler := &handler{
		render:    server.RenderTemplate,
		apiClient: mockAPIClient,
	}

	t.Run("Returns expected gateway response", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/gateways", nil)
		handler.handleAPIGateways(recorder, req, httprouter.Params{})
		resp := recorder.Result()
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("not expecting error reading response body but got: %v", err)
		}

		if recorder.Code != http.StatusOK {
			t.Errorf("Incorrect StatusCode: %+v", recorder.Code)
			t.Errorf("Expected              %+v", http.StatusOK)
		}

		header := http.Header{
			"Content-Type": []string{"application/json"},
		}
		if diff := deep.Equal(recorder.Header(), header); diff != nil {
			t.Errorf("Unexpected header: %v", diff)
		}

		apiGatewayOutputGolden, err := os.ReadFile("testdata/api_gateway_output.json")
		if err != nil {
			t.Fatalf("not expecting error reading api check output golden file but got: %v", err)
		}
		apiGatewayOutputGoldenCompact := &bytes.Buffer{}
		err = json.Compact(apiGatewayOutputGoldenCompact, apiGatewayOutputGolden)
		if err != nil {
			t.Fatalf("not expecting error compacting api check output golden file but got: %v", err)
		}
		bodyCompact := &bytes.Buffer{}
		err = json.Compact(bodyCompact, body)
		if err != nil {
			t.Fatalf("failed to compact response body: %s", err)
		}
		if !bytes.Equal(bodyCompact.Bytes(), apiGatewayOutputGoldenCompact.Bytes()) {
			t.Errorf("expecting response body to be\n %s\n but got\n %s", apiGatewayOutputGoldenCompact.Bytes(), bodyCompact.Bytes())
		}
	})

	t.Run("Returns error when invalid timeWindow is passed", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/gateways?window=1t", nil)
		handler.handleAPIGateways(recorder, req, httprouter.Params{})
		resp := recorder.Result()
		defer resp.Body.Close()
		_, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("not expecting error reading response body but got: %v", err)
		}
		if recorder.Code == http.StatusOK {
			t.Errorf("Incorrect StatusCode: %+v", recorder.Code)
			t.Errorf("Expected              %+v", http.StatusInternalServerError)
		}
	})
}

// A tap request that the Kubernetes API refuses with 403 should close the
// websocket with a policy-violation frame pointing at the tap RBAC docs,
// rather than with a generic internal server error.
func TestHandleApiTapForbidden(t *testing.T) {
	k8sSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","message":"tap is not allowed","reason":"Forbidden","code":403}`))
	}))
	defer k8sSrv.Close()

	h := &handler{
		k8sAPI: &k8sPkg.KubernetesAPI{Config: &rest.Config{Host: k8sSrv.URL}},
	}

	webSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.handleAPITap(w, r, httprouter.Params{})
	}))
	defer webSrv.Close()

	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(webSrv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("failed to open the tap websocket: %s", err)
	}
	defer conn.Close()
	defer resp.Body.Close()

	params, err := json.Marshal(tappkg.TapRequestParams{Resource: "deploy/foo", Namespace: "default"})
	if err != nil {
		t.Fatalf("failed to encode the tap request: %s", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, params); err != nil {
		t.Fatalf("failed to send the tap request: %s", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatalf("failed to set a read deadline: %s", err)
	}
	_, _, err = conn.ReadMessage()

	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("expected a websocket close frame, got %v", err)
	}
	if closeErr.Code != websocket.ClosePolicyViolation {
		t.Errorf("expected close code %d for a forbidden tap, got %d (%q)", websocket.ClosePolicyViolation, closeErr.Code, closeErr.Text)
	}
	if !strings.Contains(closeErr.Text, tappkg.TapRbacURL) {
		t.Errorf("expected the close message to mention %s, got %q", tappkg.TapRbacURL, closeErr.Text)
	}
}
