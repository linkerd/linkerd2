package injector

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net/http"

	pem "github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
)

// WebhookServer is the webhook's HTTP server. It has an embedded webhook which
// mutate all the requests.
type WebhookServer struct {
	*http.Server
	*Webhook
}

// NewWebhookServer returns a new instance of the WebhookServer.
func NewWebhookServer(client kubernetes.Interface, resources *WebhookResources, addr, controllerNamespace, certFile, keyFile string, noInitContainer bool) (*WebhookServer, error) {
	c, err := tlsConfig(certFile, keyFile)
	if err != nil {
		return nil, err
	}

	server := &http.Server{
		Addr:      addr,
		TLSConfig: c,
	}

	webhook, err := NewWebhook(client, resources, controllerNamespace, noInitContainer)
	if err != nil {
		return nil, err
	}

	ws := &WebhookServer{server, webhook}
	ws.Handler = http.HandlerFunc(ws.serve)
	return ws, nil
}

func (w *WebhookServer) serve(res http.ResponseWriter, req *http.Request) {
	var (
		data []byte
		err  error
	)
	if req.Body != nil {
		data, err = ioutil.ReadAll(req.Body)
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if len(data) == 0 {
		return
	}

	response := w.Mutate(data)
	responseJSON, err := json.Marshal(response)
	if err != nil {
		http.Error(res, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := res.Write(responseJSON); err != nil {
		http.Error(res, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Shutdown initiates a graceful shutdown of the underlying HTTP server.
func (w *WebhookServer) Shutdown() error {
	return w.Server.Shutdown(context.Background())
}

func tlsConfig(certFile, keyFile string) (*tls.Config, error) {
	certBytes, err := ioutil.ReadFile(certFile)
	if err != nil {
		return nil, err
	}

	keyBytes, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}

	certPEM, err := pem.PEMEncodeCert(certBytes)
	if err != nil {
		return nil, err
	}
	log.Debugf("PEM-encoded certificate: %s\n", certPEM)

	keyPEM, err := pem.PEMEncodeKey(keyBytes, pem.KeyTypeECDSA)
	if err != nil {
		return nil, err
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}, nil
}
