package api

import (
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
			code:   http.StatusForbidden,
			header: http.Header{"Content-Type": []string{"application/json"}},
			body:   `{"error":"tap authorization failed (not authorized to access namespaces.tap.linkerd.io), visit https://linkerd.io/tap-rbac for more information"}`,
		},
	}

	for i, exp := range expectations {
		exp := exp // pin

		t.Run(fmt.Sprintf("%d handle the tap request", i), func(t *testing.T) {
			k8sAPI, err := k8s.NewFakeAPI()
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			h := &handler{
				k8sAPI: k8sAPI,
				log:    logrus.WithField("test", t.Name()),
			}
			recorder := httptest.NewRecorder()
			h.handleTap(recorder, exp.req, exp.params)

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
