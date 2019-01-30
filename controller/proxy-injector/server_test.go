package injector

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	log "github.com/sirupsen/logrus"
)

var (
	testServer           *WebhookServer
	testWebhookResources *WebhookResources
)

func init() {
	// create a webhook which uses its fake client to seed the sidecar configmap
	fakeClient, err := fake.NewClient("")
	if err != nil {
		panic(err)
	}

	testWebhookResources = &WebhookResources{
		FileProxySpec:                fake.FileProxySpec,
		FileProxyInitSpec:            fake.FileProxyInitSpec,
		FileTLSTrustAnchorVolumeSpec: fake.FileTLSTrustAnchorVolumeSpec,
		FileTLSIdentityVolumeSpec:    fake.FileTLSIdentityVolumeSpec,
	}
	webhook, err := NewWebhook(fakeClient, testWebhookResources, fake.DefaultControllerNamespace, false)
	if err != nil {
		panic(err)
	}
	log.SetOutput(ioutil.Discard)
	factory = fake.NewFactory()

	factory = fake.NewFactory()
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
	certFile, err := factory.CertFile()
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}
	defer os.Remove(certFile)

	keyFile, err := factory.PrivateKey()
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}
	defer os.Remove(keyFile)

	var (
		addr       = ":7070"
		kubeconfig = ""
	)
	fakeClient, err := fake.NewClient(kubeconfig)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	server, err := NewWebhookServer(fakeClient, testWebhookResources, addr, fake.DefaultControllerNamespace, certFile, keyFile, false)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	if server.Addr != fmt.Sprintf("%s", addr) {
		t.Errorf("Expected server address to be :%q", addr)
	}
}
