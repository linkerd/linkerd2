package public

import (
	"context"
	"fmt"
	"net/http"

	healthcheckPb "github.com/linkerd/linkerd2/controller/gen/common/healthcheck"
	tapPb "github.com/linkerd/linkerd2/controller/gen/controller/tap"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	promApi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/metadata"
)

var (
	statSummaryPath   = fullURLPathFor("StatSummary")
	topRoutesPath     = fullURLPathFor("TopRoutes")
	versionPath       = fullURLPathFor("Version")
	listPodsPath      = fullURLPathFor("ListPods")
	listServicesPath  = fullURLPathFor("ListServices")
	tapByResourcePath = fullURLPathFor("TapByResource")
	selfCheckPath     = fullURLPathFor("SelfCheck")
)

type handler struct {
	grpcServer pb.ApiServer
}

func (h *handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	log.WithFields(log.Fields{
		"req.Method": req.Method, "req.URL": req.URL, "req.Form": req.Form,
	}).Debugf("Serving %s %s", req.Method, req.URL.Path)
	// Validate request method
	if req.Method != http.MethodPost {
		writeErrorToHTTPResponse(w, fmt.Errorf("POST required"))
		return
	}

	// Serve request
	switch req.URL.Path {
	case statSummaryPath:
		h.handleStatSummary(w, req)
	case topRoutesPath:
		h.handleTopRoutes(w, req)
	case versionPath:
		h.handleVersion(w, req)
	case listPodsPath:
		h.handleListPods(w, req)
	case listServicesPath:
		h.handleListServices(w, req)
	case tapByResourcePath:
		h.handleTapByResource(w, req)
	case selfCheckPath:
		h.handleSelfCheck(w, req)
	default:
		http.NotFound(w, req)
	}

}

func (h *handler) handleStatSummary(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.StatSummaryRequest

	err := httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.StatSummary(req.Context(), &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}
	err = writeProtoToHTTPResponse(w, rsp)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}
}

func (h *handler) handleTopRoutes(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.TopRoutesRequest

	err := httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.TopRoutes(req.Context(), &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}
	err = writeProtoToHTTPResponse(w, rsp)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}
}

func (h *handler) handleVersion(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.Empty
	err := httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.Version(req.Context(), &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}

	err = writeProtoToHTTPResponse(w, rsp)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}
}

func (h *handler) handleSelfCheck(w http.ResponseWriter, req *http.Request) {
	var protoRequest healthcheckPb.SelfCheckRequest
	err := httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.SelfCheck(req.Context(), &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}

	err = writeProtoToHTTPResponse(w, rsp)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}
}

func (h *handler) handleListPods(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.ListPodsRequest
	err := httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.ListPods(req.Context(), &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}

	err = writeProtoToHTTPResponse(w, rsp)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}
}

func (h *handler) handleListServices(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.ListServicesRequest

	err := httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.ListServices(req.Context(), &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}

	err = writeProtoToHTTPResponse(w, rsp)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}
}

func (h *handler) handleTapByResource(w http.ResponseWriter, req *http.Request) {
	flushableWriter, err := newStreamingWriter(w)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}

	var protoRequest pb.TapByResourceRequest
	err = httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}

	server := tapServer{w: flushableWriter, req: req}
	err = h.grpcServer.TapByResource(&protoRequest, server)
	if err != nil {
		writeErrorToHTTPResponse(w, err)
		return
	}
}

type tapServer struct {
	w   flushableResponseWriter
	req *http.Request
}

func (s tapServer) Send(msg *pb.TapEvent) error {
	err := writeProtoToHTTPResponse(s.w, msg)
	if err != nil {
		writeErrorToHTTPResponse(s.w, err)
		return err
	}

	s.w.Flush()
	return nil
}

// satisfy the pb.Api_TapServer interface
func (s tapServer) SetHeader(metadata.MD) error  { return nil }
func (s tapServer) SendHeader(metadata.MD) error { return nil }
func (s tapServer) SetTrailer(metadata.MD)       {}
func (s tapServer) Context() context.Context     { return s.req.Context() }
func (s tapServer) SendMsg(interface{}) error    { return nil }
func (s tapServer) RecvMsg(interface{}) error    { return nil }

func fullURLPathFor(method string) string {
	return apiRoot + apiPrefix + method
}

func NewServer(
	addr string,
	prometheusClient promApi.Client,
	tapClient tapPb.TapClient,
	k8sAPI *k8s.API,
	controllerNamespace string,
	ignoredNamespaces []string,
) *http.Server {
	baseHandler := &handler{
		grpcServer: newGrpcServer(
			promv1.NewAPI(prometheusClient),
			tapClient,
			k8sAPI,
			controllerNamespace,
			ignoredNamespaces,
		),
	}

	instrumentedHandler := prometheus.WithTelemetry(baseHandler)

	return &http.Server{
		Addr:    addr,
		Handler: instrumentedHandler,
	}
}
