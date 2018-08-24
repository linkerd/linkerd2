package injector

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	log "github.com/sirupsen/logrus"
)

var testServer *WebhookServer

func init() {
	// create a webhook which uses its fake client to seed the sidecar configmap
	fakeClient, err := fake.NewClient("")
	if err != nil {
		panic(err)
	}

	webhook, err = NewWebhook(fakeClient, fake.DefaultControllerNamespace)
	if err != nil {
		panic(err)
	}
	webhook.logger.Out = ioutil.Discard
	factory = fake.NewFactory()

	// create a fake namespace
	namespace, err := factory.Namespace("namespace-linkerd.yaml")
	if err != nil {
		panic(err)
	}
	if _, err := webhook.k8sAPI.Client.CoreV1().Namespaces().Create(namespace); err != nil {
		panic(err)
	}

	// create a fake sidecar spec config map
	configMap, err := factory.ConfigMap("config-map-sidecar.yaml")
	if err != nil {
		panic(err)
	}
	if _, err := webhook.k8sAPI.Client.CoreV1().ConfigMaps(namespace.ObjectMeta.GetName()).Create(configMap); err != nil {
		panic(err)
	}

	factory = fake.NewFactory()

	logger := log.New()
	logger.Out = ioutil.Discard
	testServer = &WebhookServer{nil, webhook, logger}
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

func TestHandleRequestError(t *testing.T) {
	var (
		errMsg   = "Some test error"
		recorder = httptest.NewRecorder()
		err      = fmt.Errorf(errMsg)
	)

	testServer.handleRequestError(recorder, err, http.StatusInternalServerError)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("HTTP response status mismatch. Expected: %d. Actual: %d", http.StatusInternalServerError, recorder.Code)
	}

	if strings.TrimSpace(recorder.Body.String()) != errMsg {
		t.Errorf("HTTP response body mismatch. Expected: %q. Actual: %q", errMsg, recorder.Body.String())
	}
}

func TestShutdown(t *testing.T) {
	server := &http.Server{Addr: ":0"}
	testServer := WebhookServer{server, nil, nil}

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
		port       = "7070"
		kubeconfig = ""
	)
	fakeClient, err := fake.NewClient(kubeconfig)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	server, err := NewWebhookServer(port, certFile, keyFile, fake.DefaultControllerNamespace, fakeClient)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	if server.Addr != fmt.Sprintf(":%s", port) {
		t.Errorf("Expected server address to be :%q", port)
	}
}

func TestServerSetLogLevel(t *testing.T) {
	testServer.SetLogLevel(log.DebugLevel)
	expected := log.DebugLevel

	if actual := testServer.Logger.Level; actual != expected {
		t.Errorf("Server log level mismatch. Expected: %q. Actual: %q", expected, actual)
	}

	if actual := testServer.logger.Level; actual != expected {
		t.Errorf("Webhook log level mismatch. Expected: %q. Actual: %q", expected, actual)
	}

}
