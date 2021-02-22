package api

import (
	"fmt"
	"net/http"

	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	"github.com/linkerd/linkerd2/viz/metrics-api/client"
	pb "github.com/linkerd/linkerd2/viz/metrics-api/gen/viz"
	promApi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	log "github.com/sirupsen/logrus"
)

var (
	gatewaysPath     = fullURLPathFor("Gateways")
	statSummaryPath  = fullURLPathFor("StatSummary")
	topRoutesPath    = fullURLPathFor("TopRoutes")
	listPodsPath     = fullURLPathFor("ListPods")
	listServicesPath = fullURLPathFor("ListServices")
	selfCheckPath    = fullURLPathFor("SelfCheck")
	edgesPath        = fullURLPathFor("Edges")
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
	case gatewaysPath:
		h.handleGateways(w, req)
	case statSummaryPath:
		h.handleStatSummary(w, req)
	case topRoutesPath:
		h.handleTopRoutes(w, req)
	case listPodsPath:
		h.handleListPods(w, req)
	case listServicesPath:
		h.handleListServices(w, req)
	case selfCheckPath:
		h.handleSelfCheck(w, req)
	case edgesPath:
		h.handleEdges(w, req)
	default:
		http.NotFound(w, req)
	}

}

func (h *handler) handleGateways(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.GatewaysRequest

	err := protohttp.HTTPRequestToProto(req, &protoRequest)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.Gateways(req.Context(), &protoRequest)
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

func (h *handler) handleStatSummary(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.StatSummaryRequest

	err := protohttp.HTTPRequestToProto(req, &protoRequest)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.StatSummary(req.Context(), &protoRequest)
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

func (h *handler) handleEdges(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.EdgesRequest

	err := protohttp.HTTPRequestToProto(req, &protoRequest)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.Edges(req.Context(), &protoRequest)
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

func (h *handler) handleTopRoutes(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.TopRoutesRequest

	err := protohttp.HTTPRequestToProto(req, &protoRequest)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.TopRoutes(req.Context(), &protoRequest)
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

func (h *handler) handleSelfCheck(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.SelfCheckRequest
	err := protohttp.HTTPRequestToProto(req, &protoRequest)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.SelfCheck(req.Context(), &protoRequest)
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

func (h *handler) handleListPods(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.ListPodsRequest
	err := protohttp.HTTPRequestToProto(req, &protoRequest)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.ListPods(req.Context(), &protoRequest)
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

func (h *handler) handleListServices(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.ListServicesRequest

	err := protohttp.HTTPRequestToProto(req, &protoRequest)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.ListServices(req.Context(), &protoRequest)
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
	return client.APIRoot + client.APIPrefix + method
}

// NewServer creates a Public API HTTP server.
func NewServer(
	addr string,
	prometheusClient promApi.Client,
	k8sAPI *k8s.API,
	controllerNamespace string,
	clusterDomain string,
	ignoredNamespaces []string,
) *http.Server {

	var promAPI promv1.API
	if prometheusClient != nil {
		promAPI = promv1.NewAPI(prometheusClient)
	}

	grpcServer := newGrpcServer(
		promAPI,
		k8sAPI,
		controllerNamespace,
		clusterDomain,
		ignoredNamespaces,
	)
	baseHandler := &handler{
		grpcServer: grpcServer,
	}

	instrumentedHandler := prometheus.WithTelemetry(baseHandler)

	return &http.Server{
		Addr:    addr,
		Handler: instrumentedHandler,
	}
}
