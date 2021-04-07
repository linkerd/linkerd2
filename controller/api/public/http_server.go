package public

import (
	"context"
	"fmt"
	"net/http"

	"github.com/golang/protobuf/proto"
	destinationPb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/metadata"
)

var (
	destGetPath = fullURLPathFor("DestinationGet")
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
	case destGetPath:
		h.handleDestGet(w, req)
	default:
		http.NotFound(w, req)
	}

}

func (h *handler) handleDestGet(w http.ResponseWriter, req *http.Request) {
	flushableWriter, err := protohttp.NewStreamingWriter(w)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	var protoRequest destinationPb.GetDestination
	err = protohttp.HTTPRequestToProto(req, &protoRequest)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	server := destinationServer{streamServer{w: flushableWriter, req: req}}
	err = h.grpcServer.Get(&protoRequest, server)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}
}

type streamServer struct {
	w   protohttp.FlushableResponseWriter
	req *http.Request
}

// satisfy the ServerStream interface
func (s streamServer) SetHeader(metadata.MD) error  { return nil }
func (s streamServer) SendHeader(metadata.MD) error { return nil }
func (s streamServer) SetTrailer(metadata.MD)       {}
func (s streamServer) Context() context.Context     { return s.req.Context() }
func (s streamServer) SendMsg(interface{}) error    { return nil }
func (s streamServer) RecvMsg(interface{}) error    { return nil }

func (s streamServer) Send(msg proto.Message) error {
	err := protohttp.WriteProtoToHTTPResponse(s.w, msg)
	if err != nil {
		protohttp.WriteErrorToHTTPResponse(s.w, err)
		return err
	}

	s.w.Flush()
	return nil
}

type destinationServer struct {
	streamServer
}

func (s destinationServer) Send(msg *destinationPb.Update) error {
	return s.streamServer.Send(msg)
}

func fullURLPathFor(method string) string {
	return apiRoot + apiPrefix + method
}

// NewServer creates a Public API HTTP server.
func NewServer(
	addr string,
	destinationClient destinationPb.DestinationClient,
	k8sAPI *k8s.API,
	controllerNamespace string,
	clusterDomain string,
) *http.Server {

	baseHandler := &handler{
		grpcServer: newGrpcServer(
			destinationClient,
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
