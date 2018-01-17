package public

import (
	"fmt"
	"net/http"

	"github.com/golang/protobuf/jsonpb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	common "github.com/runconduit/conduit/controller/gen/common"
	healthcheckPb "github.com/runconduit/conduit/controller/gen/common/healthcheck"
	tapPb "github.com/runconduit/conduit/controller/gen/controller/tap"
	telemPb "github.com/runconduit/conduit/controller/gen/controller/telemetry"
	pb "github.com/runconduit/conduit/controller/gen/public"
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

type (
	handler struct {
		grpcServer pb.ApiServer
	}

	tapServer struct {
		w   http.ResponseWriter
		req *http.Request
	}
)

var (
	jsonMarshaler   = jsonpb.Marshaler{EmitDefaults: true}
	jsonUnmarshaler = jsonpb.Unmarshaler{}
	statPath        = fullUrlPathFor("Stat")
	versionPath     = fullUrlPathFor("Version")
	listPodsPath    = fullUrlPathFor("ListPods")
	tapPath         = fullUrlPathFor("Tap")
	selfCheckPath   = fullUrlPathFor("SelfCheck")
)

func NewServer(addr string, telemetryClient telemPb.TelemetryClient, tapClient tapPb.TapClient) *http.Server {
	var baseHandler http.Handler
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "A counter for requests to the wrapped handler.",
		},
		[]string{"code", "method"},
	)
	prometheus.MustRegister(counter)

	baseHandler = &handler{
		grpcServer: newGrpcServer(telemetryClient, tapClient),
	}
	instrumentedHandler := promhttp.InstrumentHandlerCounter(counter, baseHandler)

	return &http.Server{
		Addr:    addr,
		Handler: instrumentedHandler,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Validate request method
	if req.Method != http.MethodPost {
		writeErrorToHttpResponse(w, fmt.Errorf("POST required"))
		return
	}

	// Serve request
	switch req.URL.Path {
	case statPath:
		h.handleStat(w, req)
	case versionPath:
		h.handleVersion(w, req)
	case listPodsPath:
		h.handleListPods(w, req)
	case tapPath:
		h.handleTap(w, req)
	case selfCheckPath:
		h.handleSelfCheck(w, req)
	default:
		http.NotFound(w, req)
	}
}

func (h *handler) handleStat(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.MetricRequest
	err := httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.Stat(req.Context(), &protoRequest)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}

	err = writeProtoToHttpResponse(w, rsp)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}
}

func (h *handler) handleVersion(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.Empty
	err := httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.Version(req.Context(), &protoRequest)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}

	err = writeProtoToHttpResponse(w, rsp)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}
}

func (h *handler) handleSelfCheck(w http.ResponseWriter, req *http.Request) {
	var protoRequest healthcheckPb.SelfCheckRequest
	err := httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.SelfCheck(req.Context(), &protoRequest)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}

	err = writeProtoToHttpResponse(w, rsp)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}
}

func (h *handler) handleListPods(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.Empty
	err := httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}

	rsp, err := h.grpcServer.ListPods(req.Context(), &protoRequest)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}

	err = writeProtoToHttpResponse(w, rsp)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}
}

func (h *handler) handleTap(w http.ResponseWriter, req *http.Request) {
	var protoRequest pb.TapRequest
	err := httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}

	if _, ok := w.(http.Flusher); !ok {
		writeErrorToHttpResponse(w, fmt.Errorf("streaming not supported"))
		return
	}

	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	server := tapServer{w: w, req: req}
	err = h.grpcServer.Tap(&protoRequest, server)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}
}

func (s tapServer) Send(msg *common.TapEvent) error {
	err := writeProtoToHttpResponse(s.w, msg)
	if err != nil {
		writeErrorToHttpResponse(s.w, err)
		return err
	}

	s.w.(http.Flusher).Flush()
	return nil
}

// satisfy the pb.Api_TapServer interface
func (s tapServer) SetHeader(metadata.MD) error  { return nil }
func (s tapServer) SendHeader(metadata.MD) error { return nil }
func (s tapServer) SetTrailer(metadata.MD)       { return }
func (s tapServer) Context() context.Context     { return s.req.Context() }
func (s tapServer) SendMsg(interface{}) error    { return nil }
func (s tapServer) RecvMsg(interface{}) error    { return nil }

func fullUrlPathFor(method string) string {
	return ApiRoot + ApiPrefix + method
}
