package tap

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/controller/gen/controller/tap"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/plugin/ochttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type apiServer struct {
	router       *httprouter.Router
	allowedNames []string
	log          *logrus.Entry
}

// NewAPIServer creates a new server that implements the Tap APIService.
func NewAPIServer(
	addr string,
	cert tls.Certificate,
	k8sAPI *k8s.API,
	grpcTapServer tap.TapServer,
	disableCommonNames bool,
) (*http.Server, net.Listener, error) {
	clientCAPem, allowedNames, usernameHeader, groupHeader, err := apiServerAuth(k8sAPI)
	if err != nil {
		return nil, nil, err
	}

	// for development
	if disableCommonNames {
		allowedNames = []string{}
	}

	log := logrus.WithFields(logrus.Fields{
		"component": "apiserver",
		"addr":      addr,
	})

	h := &handler{
		k8sAPI:         k8sAPI,
		usernameHeader: usernameHeader,
		groupHeader:    groupHeader,
		grpcTapServer:  grpcTapServer,
		log:            log,
	}

	router := initRouter(h)

	server := &apiServer{
		router:       router,
		allowedNames: allowedNames,
		log:          log,
	}

	clientCertPool := x509.NewCertPool()
	clientCertPool.AppendCertsFromPEM([]byte(clientCAPem))

	wrappedServer := prometheus.WithTelemetry(server)

	s := &http.Server{
		Addr:    addr,
		Handler: &ochttp.Handler{Handler: wrappedServer},
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.VerifyClientCertIfGiven,
			ClientCAs:    clientCertPool,
		},
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("net.Listen failed with: %s", err)
	}

	return s, lis, nil
}

// ServeHTTP handles all routes for the APIServer.
func (a *apiServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	a.log.Debugf("ServeHTTP(): %+v", req)

	// if `requestheader-allowed-names` was empty, allow any CN
	if len(a.allowedNames) > 0 {
		validCN := ""
		clientNames := []string{}
		for _, cn := range a.allowedNames {
			for _, clientCert := range req.TLS.PeerCertificates {
				clientNames = append(clientNames, clientCert.Subject.CommonName)
				if cn == clientCert.Subject.CommonName {
					validCN = clientCert.Subject.CommonName
					break
				}
			}
			if validCN != "" {
				break
			}
		}
		if validCN == "" {
			err := fmt.Errorf("no valid CN found. allowed names: %s, client names: %s", a.allowedNames, clientNames)
			a.log.Debug(err)
			renderJSONError(w, err, http.StatusBadRequest)
			return
		}
	}

	a.router.ServeHTTP(w, req)
}

// apiServerAuth parses the relevant data out of a ConfigMap to enable client
// TLS authentication.
// kubectl -n kube-system get cm/extension-apiserver-authentication
// accessible via the extension-apiserver-authentication-reader role
func apiServerAuth(k8sAPI *k8s.API) (string, []string, string, string, error) {
	cmName := "extension-apiserver-authentication"

	cm, err := k8sAPI.Client.CoreV1().
		ConfigMaps("kube-system").
		Get(cmName, metav1.GetOptions{})
	if err != nil {
		return "", nil, "", "", fmt.Errorf("failed to load [%s] config: %s", cmName, err)
	}
	clientCAPem, ok := cm.Data["requestheader-client-ca-file"]
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
	if len(in) == 0 {
		return nil, nil
	}
	var ret []string
	if err := json.Unmarshal([]byte(in), &ret); err != nil {
		return nil, err
	}
	return ret, nil
}
