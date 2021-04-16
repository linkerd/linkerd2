package public

import (
	"fmt"
	"net/http"

	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	log "github.com/sirupsen/logrus"
)

var (
	versionPath = fullURLPathFor("Version")
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
	case versionPath:
		h.handleVersion(w, req)
	default:
		http.NotFound(w, req)
	}

}

func (h *handler) handleVersion(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.Empty
	err := protohttp.HTTPRequestToProto(req, &protoRequest)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.Version(req.Context(), &protoRequest)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	err = protohttp.WriteProtoToHTTPResponse(w, rsp)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}
}

func fullURLPathFor(method string) string {
	return apiRoot + apiPrefix + method
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
