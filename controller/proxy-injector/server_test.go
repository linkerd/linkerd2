package injector

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	"github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
)

var (
	testServer *WebhookServer
)

func init() {
	// create a webhook which uses its fake client to seed the sidecar configmap
	k8sAPI, err := k8s.NewFakeAPI()
	if err != nil {
		panic(err)
	}

	webhook, err := NewWebhook(k8sAPI, fake.DefaultControllerNamespace, false)
	if err != nil {
		panic(err)
	}
	log.SetOutput(ioutil.Discard)
	factory = fake.NewFactory(filepath.Join("fake", "data"))

	testServer = &WebhookServer{nil, webhook}
}

func TestServe(t *testing.T) {
	t.Run("with empty http request body", func(t *testing.T) {
		in := bytes.NewReader(nil)
		request := httptest.NewRequest(http.MethodGet, "/", in)

		recorder := httptest.NewRecorder()
		testServer.serve(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Errorf("HTTP response status mismatch. Expected: %d. Actual: %d", http.StatusOK, recorder.Code)
		}

		if reflect.DeepEqual(recorder.Body.Bytes(), []byte("")) {
			t.Errorf("Content mismatch. Expected HTTP response body to be empty %v", recorder.Body.Bytes())
		}
	})
}

func TestShutdown(t *testing.T) {
	server := &http.Server{Addr: ":0"}
	testServer := WebhookServer{server, nil}

	go func() {
		if err := testServer.ListenAndServe(); err != nil {
			if err != http.ErrServerClosed {
				t.Errorf("Expected server to be gracefully shutdown with error: %q", http.ErrServerClosed)
			}
		}
	}()

	if err := testServer.Shutdown(); err != nil {
		t.Fatal("Unexpected error: ", err)
	}
}

func TestNewWebhookServer(t *testing.T) {
	rootCA, err := tls.GenerateRootCAWithDefaults("Test CA")
	if err != nil {
		log.Fatalf("failed to create root CA: %s", err)
	}

	addr := ":7070"
	k8sAPI, err := k8s.NewFakeAPI()
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}
	server, err := NewWebhookServer(k8sAPI, addr, fake.DefaultControllerNamespace, false, rootCA)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	if server.Addr != addr {
		t.Errorf("Expected server address to be :%q", addr)
	}
}
