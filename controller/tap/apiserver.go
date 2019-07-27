package tap

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"
	pb "github.com/linkerd/linkerd2/controller/gen/controller/tap"
	"github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/prometheus"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type apiServer struct {
	router         *httprouter.Router
	k8sAPI         *k8s.API
	allowedNames   []string
	usernameHeader string
	groupHeader    string
	apiResList     []byte
	tapClient      pb.TapClient
	log            *logrus.Entry
}

// TODO: share with api_handlers.go
type jsonError struct {
	Error string `json:"error"`
}

var gvk = &schema.GroupVersionKind{
	Group:   "tap.linkerd.io",
	Version: "v1alpha1",
	Kind:    "Tap",
}

var resources = []struct {
	name       string
	namespaced bool
}{
	{"namespaces", false},
	{"pods", true},
	{"replicationcontrollers", true},
	{"services", true},
	{"daemonsets", true},
	{"deployments", true},
	{"replicasets", true},
	{"statefulsets", true},
	{"jobs", true},
}

// NewAPIServer creates a new server that implements the Tap APIService.
func NewAPIServer(addr string, cert tls.Certificate, k8sAPI *k8s.API, client pb.TapClient) (*http.Server, net.Listener, error) {
	apiResList, err := apiResourceList()
	if err != nil {
		return nil, nil, err
	}

	clientCAPem, allowedNames, usernameHeader, groupHeader, err := apiServerAuth(k8sAPI)
	if err != nil {
		return nil, nil, err
	}

	server := &apiServer{
		router:         &httprouter.Router{},
		k8sAPI:         k8sAPI,
		apiResList:     apiResList,
		allowedNames:   allowedNames,
		usernameHeader: usernameHeader,
		groupHeader:    groupHeader,
		tapClient:      client,
		log: logrus.WithFields(logrus.Fields{
			"component": "apiserver",
			"addr":      addr,
		}),
	}

	server.router.GET("/apis/"+gvk.GroupVersion().String(), server.handleAPIResourceList)

	for _, res := range resources {
		route := ""
		if !res.namespaced {
			route = fmt.Sprintf("/apis/%s/watch/%s/:namespace/tap", gvk.GroupVersion().String(), res.name)
		} else {
			route = fmt.Sprintf("/apis/%s/watch/namespaces/:namespace/%s/:name/tap", gvk.GroupVersion().String(), res.name)
		}
		server.router.POST(route, server.handleTap)
	}

	clientCertPool := x509.NewCertPool()
	clientCertPool.AppendCertsFromPEM([]byte(clientCAPem))

	wrappedServer := prometheus.WithTelemetry(server)

	s := &http.Server{
		Addr:    addr,
		Handler: wrappedServer,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.VerifyClientCertIfGiven,
			ClientCAs:    clientCertPool,
		},
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		server.log.Fatalf("net.Listen failed with: %s", err)
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

// /apis/tap.linkerd.io/v1alpha1
// this is required for `kubectl api-resources` to work
func (a *apiServer) handleAPIResourceList(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(a.apiResList)
}

// /apis/tap.linkerd.io/v1alpha1/watch/namespaces/:namespace/tap
// /apis/tap.linkerd.io/v1alpha1/watch/namespaces/:namespace/:resource/:name/tap
func (a *apiServer) handleTap(w http.ResponseWriter, req *http.Request, p httprouter.Params) {
	namespace := p.ByName("namespace")
	name := p.ByName("name")
	resource := ""

	path := strings.Split(req.URL.Path, "/")
	if len(path) == 8 {
		resource = path[5]
	} else if len(path) == 10 {
		resource = path[7]
	} else {
		err := fmt.Errorf("invalid path: %s", req.URL.Path)
		a.log.Error(err)
		renderJSONError(w, err, http.StatusBadRequest)
		return
	}

	a.log.Debugf("SubjectAccessReview: namespace: %s, resource: %s, name: %s, user: %s, group: %s",
		namespace, resource, name, req.Header.Get(a.usernameHeader), req.Header[a.groupHeader],
	)

	err := pkgK8s.ResourceAuthzForUser(
		a.k8sAPI.Client,
		namespace,
		"watch",
		gvk.Group,
		gvk.Version,
		resource,
		"tap",
		name,
		req.Header.Get(a.usernameHeader),
		req.Header[a.groupHeader],
	)
	if err != nil {
		err = fmt.Errorf("SubjectAccessReview failed with: %s", err)
		a.log.Error(err)
		renderJSONError(w, err, http.StatusInternalServerError)
		return
	}

	tapReq := public.TapByResourceRequest{}
	err = protohttp.HTTPRequestToProto(req, &tapReq)
	if err != nil {
		err = fmt.Errorf("Error decoding Tap Request proto: %s", err)
		a.log.Error(err)
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	url := protohttp.TapReqToURL(&tapReq)
	if url != req.URL.Path {
		err = fmt.Errorf("tap request body did not match APIServer URL: %+v != %+v", url, req.URL.Path)
		a.log.Error(err)
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	flushableWriter, err := protohttp.NewStreamingWriter(w)
	if err != nil {
		a.log.Error(err)
		protohttp.WriteErrorToHTTPResponse(w, err)
		return
	}

	client, err := a.tapClient.TapByResource(req.Context(), &tapReq)
	if err != nil {
		a.log.Error(err)
		protohttp.WriteErrorToHTTPResponse(flushableWriter, err)
		return
	}

	for {
		select {
		case <-req.Context().Done():
			a.log.Debug("Received Done context in Tap Stream")
			return
		default:
			event, err := client.Recv()
			if err != nil {
				a.log.Errorf("Error receiving from tap client: %s", err)
				protohttp.WriteErrorToHTTPResponse(flushableWriter, err)
				return
			}
			err = protohttp.WriteProtoToHTTPResponse(flushableWriter, event)
			if err != nil {
				a.log.Errorf("Error writing proto to HTTP Response: %s", err)
				protohttp.WriteErrorToHTTPResponse(flushableWriter, err)
				return
			}
			flushableWriter.Flush()
		}
	}
}

// TODO: share with api_handlers.go
func renderJSONError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	rsp, _ := json.Marshal(jsonError{Error: err.Error()})
	w.WriteHeader(status)
	w.Write(rsp)
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

func apiResourceList() ([]byte, error) {
	resList := metav1.APIResourceList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "APIResourceList",
			APIVersion: "v1",
		},
		GroupVersion: gvk.GroupVersion().String(),
		APIResources: []metav1.APIResource{},
	}

	for _, res := range resources {
		resList.APIResources = append(resList.APIResources,
			metav1.APIResource{
				Name:       fmt.Sprintf("%s/tap", res.name),
				Namespaced: res.namespaced,
				Kind:       gvk.Kind,
				Group:      "tap",
				Verbs:      metav1.Verbs{"watch"},
			})
	}

	return json.MarshalIndent(resList, "", "  ")
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
