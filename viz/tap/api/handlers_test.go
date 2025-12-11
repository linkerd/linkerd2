package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-test/deep"
	"github.com/julienschmidt/httprouter"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/sirupsen/logrus"
	authV1 "k8s.io/api/authorization/v1"
	k8sFake "k8s.io/client-go/kubernetes/fake"
	k8sTesting "k8s.io/client-go/testing"
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
			body:   `{"error":"invalid path: \"/apis\""}`,
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
			if diff := deep.Equal(recorder.Header(), exp.header); diff != nil {
				t.Errorf("Unexpected header: %v", diff)
			}
			if recorder.Body.String() != exp.body {
				t.Errorf("Unexpected body: %s, expected: %s", recorder.Body.String(), exp.body)
			}
		})
	}
}

func TestHandleTap_ExtraHeaders(t *testing.T) {
	k8sAPI, err := k8s.NewFakeAPI()
	if err != nil {
		t.Fatalf("NewFakeAPI returned an error: %s", err)
	}

	h := &handler{
		k8sAPI:            k8sAPI,
		log:               logrus.WithField("test", t.Name()),
		extraHeaderPrefix: "X-Remote-Extra-",
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/apis/tap.linkerd.io/v1alpha1/watch/namespaces/foo/tap", nil)
	req.Header.Set("X-Remote-Extra-Foo", "bar")
	req.Header.Set("X-Remote-Extra-Baz", "qux")

	params := httprouter.Params{
		{Key: "namespace", Value: "foo"},
	}

	h.handleTap(recorder, req, params)

	client := k8sAPI.Client.(*k8sFake.Clientset)
	actions := client.Actions()

	var sar *authV1.SubjectAccessReview
	for _, action := range actions {
		if action.GetVerb() == "create" && action.GetResource().Resource == "subjectaccessreviews" {
			createAction := action.(k8sTesting.CreateAction)
			obj := createAction.GetObject()
			sar = obj.(*authV1.SubjectAccessReview)
			break
		}
	}

	if sar == nil {
		t.Fatal("Expected SubjectAccessReview to be created")
	}

	if len(sar.Spec.Extra) != 2 {
		t.Errorf("Expected 2 extra headers, got %d", len(sar.Spec.Extra))
	}

	if v, ok := sar.Spec.Extra["Foo"]; !ok || v[0] != "bar" {
		t.Errorf("Expected Extra['Foo'] to be ['bar'], got %v", v)
	}
	if v, ok := sar.Spec.Extra["Baz"]; !ok || v[0] != "qux" {
		t.Errorf("Expected Extra['Baz'] to be ['qux'], got %v", v)
	}
}
