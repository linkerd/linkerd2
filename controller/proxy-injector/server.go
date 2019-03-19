package injector

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	pkgTls "github.com/linkerd/linkerd2/pkg/tls"
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
func NewWebhookServer(client kubernetes.Interface, addr, controllerNamespace string, noInitContainer bool, rootCA *pkgTls.CA) (*WebhookServer, error) {
	c, err := tlsConfig(rootCA, controllerNamespace)
	if err != nil {
		return nil, err
	}

	server := &http.Server{
		Addr:      addr,
		TLSConfig: c,
	}

	webhook, err := NewWebhook(client, controllerNamespace, noInitContainer)
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

func tlsConfig(rootCA *pkgTls.CA, controllerNamespace string) (*tls.Config, error) {
	// must use the service short name in this TLS identity as the k8s api server
	// looks for the webhook at <svc_name>.<namespace>.svc, without the cluster
	// domain.
	dnsName := fmt.Sprintf("linkerd-proxy-injector.%s.svc", controllerNamespace)
	cred, err := rootCA.GenerateEndEntityCred(dnsName)
	if err != nil {
		return nil, err
	}

	certPEM := cred.EncodePEM()
	log.Debugf("PEM-encoded certificate: %s\n", certPEM)

	keyPEM := cred.EncodePrivateKeyPEM()
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}, nil
}
