package webhook

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/linkerd/linkerd2/controller/k8s"
	pkgTls "github.com/linkerd/linkerd2/pkg/tls"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type handlerFunc func(*k8s.API, *admissionv1beta1.AdmissionRequest) (*admissionv1beta1.AdmissionResponse, error)

// Server describes the https server implementing the webhook
type Server struct {
	*http.Server
	api     *k8s.API
	handler handlerFunc
}

// NewServer returns a new instance of Server
func NewServer(api *k8s.API, addr string, cred *pkgTls.Cred, handler handlerFunc) (*Server, error) {
	var (
		certPEM = cred.EncodePEM()
		keyPEM  = cred.EncodePrivateKeyPEM()
	)

	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, err
	}

	server := &http.Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
	}

	s := &Server{server, api, handler}
	s.Handler = http.HandlerFunc(s.serve)
	return s, nil
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

	response := s.processReq(data)
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

func (s *Server) processReq(data []byte) *admissionv1beta1.AdmissionReview {
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

	admissionResponse, err := s.handler(s.api, admissionReview.Request)
	if err != nil {
		log.Error("failed to inject sidecar. Reason: ", err)
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
