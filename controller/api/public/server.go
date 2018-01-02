package public

import (
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/runconduit/conduit/pkg/conduit"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	common "github.com/runconduit/conduit/controller/gen/common"
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
		serverMarshalError(w, req, fmt.Errorf("POST required"), http.StatusMethodNotAllowed)
		return
	}

	// Validate request content type
	switch req.Header.Get("Content-Type") {
	case "", conduit.ProtobufContentType, conduit.JsonContentType:
	default:
		serverMarshalError(w, req, fmt.Errorf("unsupported Content-Type"), http.StatusUnsupportedMediaType)
		return
	}

	// Serve request
	switch req.URL.Path {
	case conduit.ApiRoot + conduit.ApiPrefix + "Stat":
		h.handleStat(w, req)
	case conduit.ApiRoot + conduit.ApiPrefix + "Version":
		h.handleVersion(w, req)
	case conduit.ApiRoot + conduit.ApiPrefix + "ListPods":
		h.handleListPods(w, req)
	case conduit.ApiRoot + conduit.ApiPrefix + "Tap":
		h.handleTap(w, req)
	default:
		http.NotFound(w, req)
	}
}

func (h *handler) handleStat(w http.ResponseWriter, req *http.Request) {
	var metricRequest pb.MetricRequest
	err := serverUnmarshal(req, &metricRequest)
	if err != nil {
		serverMarshalError(w, req, err, http.StatusBadRequest)
		return
	}

	rsp, err := h.grpcServer.Stat(req.Context(), &metricRequest)
	if err != nil {
		serverMarshalError(w, req, err, http.StatusInternalServerError)
		return
	}

	err = serverMarshal(w, req, rsp)
	if err != nil {
		serverMarshalError(w, req, err, http.StatusInternalServerError)
		return
	}
}

func (h *handler) handleVersion(w http.ResponseWriter, req *http.Request) {
	var emptyRequest pb.Empty
	err := serverUnmarshal(req, &emptyRequest)
	if err != nil {
		serverMarshalError(w, req, err, http.StatusBadRequest)
		return
	}

	rsp, err := h.grpcServer.Version(req.Context(), &emptyRequest)
	if err != nil {
		serverMarshalError(w, req, err, http.StatusInternalServerError)
		return
	}

	err = serverMarshal(w, req, rsp)
	if err != nil {
		serverMarshalError(w, req, err, http.StatusInternalServerError)
		return
	}
}

func (h *handler) handleListPods(w http.ResponseWriter, req *http.Request) {
	var emptyRequest pb.Empty
	err := serverUnmarshal(req, &emptyRequest)
	if err != nil {
		serverMarshalError(w, req, err, http.StatusBadRequest)
		return
	}

	rsp, err := h.grpcServer.ListPods(req.Context(), &emptyRequest)
	if err != nil {
		serverMarshalError(w, req, err, http.StatusInternalServerError)
		return
	}

	err = serverMarshal(w, req, rsp)
	if err != nil {
		serverMarshalError(w, req, err, http.StatusInternalServerError)
		return
	}
}

func (h *handler) handleTap(w http.ResponseWriter, req *http.Request) {
	var tapRequest pb.TapRequest
	err := serverUnmarshal(req, &tapRequest)
	if err != nil {
		serverMarshalError(w, req, err, http.StatusBadRequest)
		return
	}

	if _, ok := w.(http.Flusher); !ok {
		serverMarshalError(w, req, fmt.Errorf("streaming not supported"), http.StatusBadRequest)
		return
	}

	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	server := tapServer{w: w, req: req}
	err = h.grpcServer.Tap(&tapRequest, server)
	if err != nil {
		serverMarshalError(w, req, err, http.StatusInternalServerError)
		return
	}
}

func serverUnmarshal(req *http.Request, msg proto.Message) error {
	switch req.Header.Get("Content-Type") {
	case "", conduit.ProtobufContentType:
		bytes, err := ioutil.ReadAll(req.Body)
		if err != nil {
			return err
		}
		return proto.Unmarshal(bytes, msg)
	case conduit.JsonContentType:
		return jsonUnmarshaler.Unmarshal(req.Body, msg)
	}
	return nil
}

func serverMarshal(w http.ResponseWriter, req *http.Request, msg proto.Message) error {
	switch req.Header.Get("Content-Type") {
	case "", conduit.ProtobufContentType:
		bytes, err := proto.Marshal(msg)
		if err != nil {
			return err
		}
		byteSize := make([]byte, 4)
		binary.LittleEndian.PutUint32(byteSize, uint32(len(bytes)))
		_, err = w.Write(append(byteSize, bytes...))
		return err

	case conduit.JsonContentType:
		str, err := jsonMarshaler.MarshalToString(msg)
		if err != nil {
			return err
		}
		_, err = w.Write(append([]byte(str), '\n'))
		return err
	}

	return nil
}

func serverMarshalError(w http.ResponseWriter, req *http.Request, err error, code int) error {
	switch req.Header.Get("Content-Type") {
	case "", conduit.ProtobufContentType:
		w.Header().Set(conduit.ErrorHeader, http.StatusText(code))
	case conduit.JsonContentType:
		w.WriteHeader(code)
	}

	return serverMarshal(w, req, &pb.ApiError{Error: err.Error()})
}

func (s tapServer) Send(msg *common.TapEvent) error {
	err := serverMarshal(s.w, s.req, msg)
	if err != nil {
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
