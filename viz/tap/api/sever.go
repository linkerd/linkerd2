package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/controller/k8s"
	pkgk8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	pkgTls "github.com/linkerd/linkerd2/pkg/tls"
	pb "github.com/linkerd/linkerd2/viz/tap/gen/tap"
	"github.com/prometheus/common/log"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Server holds the underlying http server and its config
type Server struct {
	*http.Server
	listener     net.Listener
	router       *httprouter.Router
	allowedNames []string
	certValue    *atomic.Value
	log          *logrus.Entry
}

// NewServer creates a new server that implements the Tap APIService.
func NewServer(
	ctx context.Context,
	addr string,
	k8sAPI *k8s.API,
	grpcTapServer pb.TapServer,
	disableCommonNames bool,
) (*Server, error) {
	updateEvent := make(chan struct{})
	errEvent := make(chan error)
	watcher := pkgTls.NewFsCredsWatcher(pkgk8s.MountPathTLSBase, updateEvent, errEvent).
		WithFilePaths(pkgk8s.MountPathTLSCrtPEM, pkgk8s.MountPathTLSKeyPEM)
	go func() {
		if err := watcher.StartWatching(ctx); err != nil {
			log.Fatalf("Failed to start creds watcher: %s", err)
		}
	}()

	clientCAPem, allowedNames, usernameHeader, groupHeader, err := serverAuth(ctx, k8sAPI)
	if err != nil {
		return nil, err
	}

	// for development
	if disableCommonNames {
		allowedNames = []string{}
	}

	log := logrus.WithFields(logrus.Fields{
		"component": "tap",
		"addr":      addr,
	})

	clientCertPool := x509.NewCertPool()
	clientCertPool.AppendCertsFromPEM([]byte(clientCAPem))

	httpServer := &http.Server{
		Addr: addr,
		TLSConfig: &tls.Config{
			ClientAuth: tls.VerifyClientCertIfGiven,
			ClientCAs:  clientCertPool,
		},
	}

	var emptyCert atomic.Value
	h := &handler{
		k8sAPI:         k8sAPI,
		usernameHeader: usernameHeader,
		groupHeader:    groupHeader,
		grpcTapServer:  grpcTapServer,
		log:            log,
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("net.Listen failed with: %s", err)
	}

	s := &Server{
		Server:       httpServer,
		listener:     lis,
		router:       initRouter(h),
		allowedNames: allowedNames,
		certValue:    &emptyCert,
		log:          log,
	}
	s.Handler = prometheus.WithTelemetry(s)
	httpServer.TLSConfig.GetCertificate = s.getCertificate

	if err := watcher.UpdateCert(s.certValue); err != nil {
		return nil, fmt.Errorf("Failed to initialized certificate: %s", err)
	}

	go watcher.ProcessEvents(log, s.certValue, updateEvent, errEvent)

	return s, nil
}

// Start starts the https server
func (a *Server) Start(ctx context.Context) {
	a.log.Infof("starting tap API server on %s", a.Server.Addr)
	if err := a.ServeTLS(a.listener, "", ""); err != nil {
		if err == http.ErrServerClosed {
			return
		}
		a.log.Fatal(err)
	}
}

func (a *Server) getCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return a.certValue.Load().(*tls.Certificate), nil
}

// ServeHTTP handles all routes for the Server.
func (a *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	a.log.Debugf("ServeHTTP(): %+v", req)
	if err := a.validate(req); err != nil {
		a.log.Debug(err)
		renderJSONError(w, err, http.StatusBadRequest)
	} else {
		a.router.ServeHTTP(w, req)
	}
}

// validate ensures that the request should be honored returning an error otherwise.
func (a *Server) validate(req *http.Request) error {
	// if `requestheader-allowed-names` was empty, allow any CN
	if len(a.allowedNames) > 0 {
		for _, cn := range a.allowedNames {
			for _, clientCert := range req.TLS.PeerCertificates {
				// Check Common Name and Subject Alternate Name(s)
				if cn == clientCert.Subject.CommonName || isSubjectAlternateName(clientCert, cn) {
					return nil
				}
			}
		}
		// Build the set of certificate names for the error message
		clientNames := []string{}
		for _, clientCert := range req.TLS.PeerCertificates {
			clientNames = append(clientNames, clientCert.Subject.CommonName)
		}
		return fmt.Errorf("no valid CN found. allowed names: %s, client names: %s", a.allowedNames, clientNames)
	}
	return nil
}

// serverAuth parses the relevant data out of a ConfigMap to enable client TLS
// authentication.
// kubectl -n kube-system get cm/extension-apiserver-authentication
// accessible via the extension-apiserver-authentication-reader role
func serverAuth(ctx context.Context, k8sAPI *k8s.API) (string, []string, string, string, error) {

	cm, err := k8sAPI.Client.CoreV1().
		ConfigMaps(metav1.NamespaceSystem).
		Get(ctx, pkgk8s.ExtensionAPIServerAuthenticationConfigMapName, metav1.GetOptions{})

	if err != nil {
		return "", nil, "", "", fmt.Errorf("failed to load [%s] config: %s", pkgk8s.ExtensionAPIServerAuthenticationConfigMapName, err)
	}

	clientCAPem, ok := cm.Data[pkgk8s.ExtensionAPIServerAuthenticationRequestHeaderClientCAFileKey]

	if !ok {
		return "", nil, "", "", fmt.Errorf("no client CA cert available for apiextension-server")
	}

	allowedNames, err := deserializeStrings(cm.Data["requestheader-allowed-names"])
	if err != nil {
		return "", nil, "", "", err
	}

	usernameHeaders, err := deserializeStrings(cm.Data["requestheader-username-headers"])
	if err != nil {
		return "", nil, "", "", err
	}
	usernameHeader := ""
	if len(usernameHeaders) > 0 {
		usernameHeader = usernameHeaders[0]
	}

	groupHeaders, err := deserializeStrings(cm.Data["requestheader-group-headers"])
	if err != nil {
		return "", nil, "", "", err
	}
	groupHeader := ""
	if len(groupHeaders) > 0 {
		groupHeader = groupHeaders[0]
	}

	return clientCAPem, allowedNames, usernameHeader, groupHeader, nil
}

// copied from https://github.com/kubernetes/apiserver/blob/781c3cd1b3dc5b6f79c68ab0d16fe544600421ef/pkg/server/options/authentication.go#L360
func deserializeStrings(in string) ([]string, error) {
	if in == "" {
		return nil, nil
	}
	var ret []string
	if err := json.Unmarshal([]byte(in), &ret); err != nil {
		return nil, err
	}
	return ret, nil
}

// isSubjectAlternateName checks all applicable fields within the certificate for a match to the provided name.
// See https://tools.ietf.org/html/rfc5280#section-4.2.1.6 for information about Subject Alternate Name.
func isSubjectAlternateName(cert *x509.Certificate, name string) bool {
	for _, dnsName := range cert.DNSNames {
		if dnsName == name {
			return true
		}
	}
	for _, emailAddress := range cert.EmailAddresses {
		if emailAddress == name {
			return true
		}
	}
	for _, ip := range cert.IPAddresses {
		if ip.String() == name {
			return true
		}
	}
	for _, url := range cert.URIs {
		if url.String() == name {
			return true
		}
	}
	return false
}
