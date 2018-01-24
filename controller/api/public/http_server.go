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
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
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

type handler struct {
	grpcServer pb.ApiServer
}

func (h *handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	log.Debugf("Serving %s %s", req.Method, req.URL.Path)
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
	flushableWriter, err := newStreamingWriter(w)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}

	var protoRequest pb.TapRequest
	err = httpRequestToProto(req, &protoRequest)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}

	server := tapServer{w: flushableWriter, req: req}
	err = h.grpcServer.Tap(&protoRequest, server)
	if err != nil {
		writeErrorToHttpResponse(w, err)
		return
	}
}

type tapServer struct {
	w   flushableResponseWriter
	req *http.Request
}

func (s tapServer) Send(msg *common.TapEvent) error {
	err := writeProtoToHttpResponse(s.w, msg)
	if err != nil {
		writeErrorToHttpResponse(s.w, err)
		return err
	}

	s.w.Flush()
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

func withTelemetry(baseHandler *handler) http.HandlerFunc {
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "A counter for requests to the wrapped handler.",
		},
		[]string{"code", "method"},
	)
	prometheus.MustRegister(counter)
	return promhttp.InstrumentHandlerCounter(counter, baseHandler)
}

func NewServer(addr string, telemetryClient telemPb.TelemetryClient, tapClient tapPb.TapClient) *http.Server {
	baseHandler := &handler{
		grpcServer: newGrpcServer(telemetryClient, tapClient),
	}

	instrumentedHandler := withTelemetry(baseHandler)

	return &http.Server{
		Addr:    addr,
		Handler: instrumentedHandler,
	}
}
