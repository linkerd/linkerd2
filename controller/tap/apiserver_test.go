package tap

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/sirupsen/logrus"
)

func TestNewAPIServer(t *testing.T) {
	expectations := []struct {
		k8sRes []string
		err    error
	}{
		{
			err: errors.New("failed to load [extension-apiserver-authentication] config: configmaps \"extension-apiserver-authentication\" not found"),
		},
		{
			err: nil,
			k8sRes: []string{`
apiVersion: v1
kind: ConfigMap
metadata:
  name: extension-apiserver-authentication
  namespace: kube-system
data:
  client-ca-file: 'client-ca-file'
  requestheader-allowed-names: '["name1", "name2"]'
  requestheader-client-ca-file: 'requestheader-client-ca-file'
  requestheader-extra-headers-prefix: '["X-Remote-Extra-"]'
  requestheader-group-headers: '["X-Remote-Group"]'
  requestheader-username-headers: '["X-Remote-User"]'
`,
			},
		},
	}

	for i, exp := range expectations {
		exp := exp // pin

		t.Run(fmt.Sprintf("%d returns a configured API Server", i), func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(exp.k8sRes...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			_, _, err = NewAPIServer("localhost:0", tls.Certificate{}, k8sAPI, nil)
			if !reflect.DeepEqual(err, exp.err) {
				t.Errorf("NewAPIServer returned unexpected error: %s, expected: %s", err, exp.err)
			}
		})
	}
}

func TestAPIServerAuth(t *testing.T) {
	expectations := []struct {
		k8sRes         []string
		clientCAPem    string
		allowedNames   []string
		usernameHeader string
		groupHeader    string
		err            error
	}{
		{
			err: errors.New("failed to load [extension-apiserver-authentication] config: configmaps \"extension-apiserver-authentication\" not found"),
		},
		{
			k8sRes: []string{`
apiVersion: v1
kind: ConfigMap
metadata:
  name: extension-apiserver-authentication
  namespace: kube-system
data:
  client-ca-file: 'client-ca-file'
  requestheader-allowed-names: '["name1", "name2"]'
  requestheader-client-ca-file: 'requestheader-client-ca-file'
  requestheader-extra-headers-prefix: '["X-Remote-Extra-"]'
  requestheader-group-headers: '["X-Remote-Group"]'
  requestheader-username-headers: '["X-Remote-User"]'
`,
			},
			clientCAPem:    "requestheader-client-ca-file",
			allowedNames:   []string{"name1", "name2"},
			usernameHeader: "X-Remote-User",
			groupHeader:    "X-Remote-Group",
			err:            nil,
		},
	}

	for i, exp := range expectations {
		exp := exp // pin

		t.Run(fmt.Sprintf("%d parses the apiServerAuth ConfigMap", i), func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI(exp.k8sRes...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			clientCAPem, allowedNames, usernameHeader, groupHeader, err := apiServerAuth(k8sAPI)
			if !reflect.DeepEqual(err, exp.err) {
				t.Errorf("apiServerAuth returned unexpected error: %s, expected: %s", err, exp.err)
			}
			if clientCAPem != exp.clientCAPem {
				t.Errorf("apiServerAuth returned unexpected clientCAPem: %s, expected: %s", clientCAPem, exp.clientCAPem)
			}
			if !reflect.DeepEqual(allowedNames, exp.allowedNames) {
				t.Errorf("apiServerAuth returned unexpected allowedNames: %s, expected: %s", allowedNames, exp.allowedNames)
			}
			if usernameHeader != exp.usernameHeader {
				t.Errorf("apiServerAuth returned unexpected usernameHeader: %s, expected: %s", usernameHeader, exp.usernameHeader)
			}
			if groupHeader != exp.groupHeader {
				t.Errorf("apiServerAuth returned unexpected groupHeader: %s, expected: %s", groupHeader, exp.groupHeader)
			}
		})
	}
}

func TestHandleTap(t *testing.T) {
	expectations := []struct {
		req    *http.Request
		params httprouter.Params
		code   int
		header http.Header
		body   string
	}{
		{
			req: &http.Request{
				URL: &url.URL{
					Path: "/apis",
				},
			},
			code:   http.StatusBadRequest,
			header: http.Header{"Content-Type": []string{"application/json"}},
			body:   `{"error":"invalid path: /apis"}`,
		},
		{
			req: &http.Request{
				URL: &url.URL{
					Path: "/apis/tap.linkerd.io/v1alpha1/watch/namespaces/foo/tap",
				},
			},
			code:   http.StatusInternalServerError,
			header: http.Header{"Content-Type": []string{"application/json"}},
			body:   `{"error":"SubjectAccessReview failed with: not authorized to access namespaces.tap.linkerd.io"}`,
		},
	}

	for i, exp := range expectations {
		exp := exp // pin

		t.Run(fmt.Sprintf("%d parses the apiServerAuth ConfigMap", i), func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI()
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			a := &apiServer{
				k8sAPI: k8sAPI,
				log:    logrus.WithField("test", t.Name),
			}
			recorder := httptest.NewRecorder()
			a.handleTap(recorder, exp.req, exp.params)

			if recorder.Code != exp.code {
				t.Errorf("Unexpected code: %d, expected: %d", recorder.Code, exp.code)
			}
			if !reflect.DeepEqual(recorder.Header(), exp.header) {
				t.Errorf("Unexpected header: %v, expected: %v", recorder.Header(), exp.header)
			}
			if recorder.Body.String() != exp.body {
				t.Errorf("Unexpected body: %s, expected: %s", recorder.Body.String(), exp.body)
			}
		})
	}
}
