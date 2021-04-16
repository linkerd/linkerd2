package public

import (
	"fmt"
	"net/http"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	log "github.com/sirupsen/logrus"
)

type handler struct {
	grpcServer Server
}

func (h *handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	log.WithFields(log.Fields{
		"req.Method": req.Method, "req.URL": req.URL, "req.Form": req.Form,
	}).Debugf("Serving %s %s", req.Method, req.URL.Path)
	// Validate request method
	if req.Method != http.MethodPost {
		protohttp.WriteErrorToHTTPResponse(w, fmt.Errorf("POST required"))
		return
	}

	// Serve request
	switch req.URL.Path {
	default:
		http.NotFound(w, req)
	}

}

// NewServer creates a Public API HTTP server.
func NewServer(
	addr string,
	k8sAPI *k8s.API,
	controllerNamespace string,
	clusterDomain string,
) *http.Server {

	baseHandler := &handler{
		grpcServer: newGrpcServer(
			k8sAPI,
			controllerNamespace,
			clusterDomain,
		),
	}

	instrumentedHandler := prometheus.WithTelemetry(baseHandler)

	return &http.Server{
		Addr:    addr,
		Handler: instrumentedHandler,
	}
}
