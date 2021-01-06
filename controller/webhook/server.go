package webhook

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"sync/atomic"

	"github.com/linkerd/linkerd2/controller/k8s"
	pkgk8s "github.com/linkerd/linkerd2/pkg/k8s"
	pkgTls "github.com/linkerd/linkerd2/pkg/tls"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/yaml"
)

// Handler is the signature for the functions that ultimately deal with
// the admission request
type Handler func(
	context.Context,
	*k8s.API,
	*admissionv1beta1.AdmissionRequest,
	record.EventRecorder,
) (*admissionv1beta1.AdmissionResponse, error)

// Server describes the https server implementing the webhook
type Server struct {
	*http.Server
	api       *k8s.API
	handler   Handler
	certValue *atomic.Value
	recorder  record.EventRecorder
}

// NewServer returns a new instance of Server
func NewServer(
	ctx context.Context,
	api *k8s.API,
	addr, certPath string,
	handler Handler,
	component string,
) (*Server, error) {
	updateEvent := make(chan struct{})
	errEvent := make(chan error)
	watcher := pkgTls.NewFsCredsWatcher(certPath, updateEvent, errEvent).
		WithFilePaths(pkgk8s.MountPathTLSCrtPEM, pkgk8s.MountPathTLSKeyPEM)
	go func() {
		if err := watcher.StartWatching(ctx); err != nil {
			log.Fatalf("Failed to start creds watcher: %s", err)
		}
	}()

	server := &http.Server{
		Addr:      addr,
		TLSConfig: &tls.Config{},
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		// In order to send events to all namespaces, we need to use an empty string here
		// re: client-go's event_expansion.go CreateWithEventNamespace()
		Interface: api.Client.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: component})

	s := getConfiguredServer(server, api, handler, recorder)
	if err := watcher.UpdateCert(s.certValue); err != nil {
		log.Fatalf("Failed to initialized certificate: %s", err)
	}

	log := logrus.WithFields(logrus.Fields{
		"component": "proxy-injector",
		"addr":      addr,
	})

	go watcher.ProcessEvents(log, s.certValue, updateEvent, errEvent)

	return s, nil
}

func getConfiguredServer(
	httpServer *http.Server,
	api *k8s.API,
	handler Handler,
	recorder record.EventRecorder,
) *Server {
	var emptyCert atomic.Value
	s := &Server{httpServer, api, handler, &emptyCert, recorder}
	s.Handler = http.HandlerFunc(s.serve)
	httpServer.TLSConfig.GetCertificate = s.getCertificate
	return s
}

// Start starts the https server
func (s *Server) Start() {
	log.Infof("listening at %s", s.Server.Addr)
	if err := s.ListenAndServeTLS("", ""); err != nil {
		if err == http.ErrServerClosed {
			return
		}
		log.Fatal(err)
	}
}

// getCertificate provides the TLS server with the current cert
func (s *Server) getCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return s.certValue.Load().(*tls.Certificate), nil
}

func (s *Server) serve(res http.ResponseWriter, req *http.Request) {
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
		log.Warn("received empty payload")
		return
	}

	response := s.processReq(req.Context(), data)
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

func (s *Server) processReq(ctx context.Context, data []byte) *admissionv1beta1.AdmissionReview {
	admissionReview, err := decode(data)
	if err != nil {
		log.Errorf("failed to decode data. Reason: %s", err)
		admissionReview.Response = &admissionv1beta1.AdmissionResponse{
			UID:     admissionReview.Request.UID,
			Allowed: false,
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
		return admissionReview
	}
	log.Infof("received admission review request %s", admissionReview.Request.UID)
	log.Debugf("admission request: %+v", admissionReview.Request)

	admissionResponse, err := s.handler(ctx, s.api, admissionReview.Request, s.recorder)
	if err != nil {
		log.Error("failed to run webhook handler. Reason: ", err)
		admissionReview.Response = &admissionv1beta1.AdmissionResponse{
			UID:     admissionReview.Request.UID,
			Allowed: false,
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
		return admissionReview
	}
	admissionReview.Response = admissionResponse

	return admissionReview
}

// Shutdown initiates a graceful shutdown of the underlying HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.Server.Shutdown(ctx)
}

func decode(data []byte) (*admissionv1beta1.AdmissionReview, error) {
	var admissionReview admissionv1beta1.AdmissionReview
	err := yaml.Unmarshal(data, &admissionReview)
	return &admissionReview, err
}
