package injector

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	"github.com/linkerd/linkerd2/pkg/k8s"
	log "github.com/sirupsen/logrus"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
)

var (
	webhook *Webhook
	factory *fake.Factory
)

func init() {
	// create a webhook which uses its fake client to seed the sidecar configmap
	fakeClient, err := fake.NewClient("")
	if err != nil {
		panic(err)
	}

	webhook, err = NewWebhook(fakeClient, testWebhookResources, fake.DefaultControllerNamespace)
	if err != nil {
		panic(err)
	}
	log.SetOutput(ioutil.Discard)
}

func TestMutate(t *testing.T) {
	var testCases = []struct {
		title        string
		requestFile  string
		responseFile string
	}{
		{title: "no labels", requestFile: "inject-no-labels-request.json", responseFile: "inject-no-labels-response.yaml"},
		{title: "inject enabled", requestFile: "inject-enabled-request.json", responseFile: "inject-enabled-response.yaml"},
		{title: "inject disabled", requestFile: "inject-disabled-request.json", responseFile: "inject-disabled-response.yaml"},
		{title: "inject completed", requestFile: "inject-completed-request.json", responseFile: "inject-completed-response.yaml"},
	}

	for _, testCase := range testCases {
		t.Run(fmt.Sprintf("%s", testCase.title), func(t *testing.T) {
			data, err := factory.HTTPRequestBody(testCase.requestFile)
			if err != nil {
				t.Fatal("Unexpected error: ", err)
			}

			expected, err := factory.AdmissionReview(testCase.responseFile)
			if err != nil {
				t.Fatal("Unexpected error: ", err)
			}

			actual := webhook.Mutate(data)
			assertEqualAdmissionReview(t, expected, actual)
		})
	}
}

func TestIgnore(t *testing.T) {
	t.Run("by checking labels", func(t *testing.T) {
		var testCases = []struct {
			filename string
			expected bool
		}{
			{filename: "deployment-inject-status-empty.yaml", expected: false},
			{filename: "deployment-inject-status-enabled.yaml", expected: false},
			{filename: "deployment-inject-status-disabled.yaml", expected: true},
			{filename: "deployment-inject-status-completed.yaml", expected: true},
		}

		for id, testCase := range testCases {
			t.Run(fmt.Sprintf("%d", id), func(t *testing.T) {
				deployment, err := factory.Deployment(testCase.filename)
				if err != nil {
					t.Fatal("Unexpected error: ", err)
				}

				if actual := webhook.ignore(deployment); actual != testCase.expected {
					t.Errorf("Boolean mismatch. Expected: %t. Actual: %t", testCase.expected, actual)
				}
			})
		}
	})

	t.Run("by checking container spec", func(t *testing.T) {
		deployment, err := factory.Deployment("deployment-with-injected-proxy.yaml")
		if err != nil {
			t.Fatal("Unexpected error: ", err)
		}

		if !webhook.ignore(deployment) {
			t.Errorf("Expected deployment with injected proxy to be ignored")
		}
	})
}

func TestContainersSpec(t *testing.T) {
	expectedSidecar, err := factory.Container("inject-sidecar-container-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	expectedInit, err := factory.Container("inject-init-container-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	identity := &k8s.TLSIdentity{
		Name:                "nginx",
		Kind:                "deployment",
		Namespace:           fake.DefaultNamespace,
		ControllerNamespace: fake.DefaultControllerNamespace,
	}

	actualSidecar, actualInit, err := webhook.containersSpec(identity)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	if !reflect.DeepEqual(expectedSidecar, actualSidecar) {
		t.Errorf("Content mismatch\nExpected: %+v\nActual: %+v", expectedSidecar, actualSidecar)
	}

	if !reflect.DeepEqual(expectedInit, actualInit) {
		t.Errorf("Content mismatch\nExpected: %+v\nActual: %+v", expectedInit, actualInit)
	}
}

func TestVolumesSpec(t *testing.T) {
	expectedTrustAnchors, err := factory.Volume("inject-trust-anchors-volume-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	expectedLinkerdSecrets, err := factory.Volume("inject-linkerd-secrets-volume-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	identity := &k8s.TLSIdentity{
		Name:                "nginx",
		Kind:                "deployment",
		Namespace:           fake.DefaultNamespace,
		ControllerNamespace: fake.DefaultControllerNamespace,
	}

	actualTrustAnchors, actualLinkerdSecrets, err := webhook.volumesSpec(identity)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	if !reflect.DeepEqual(expectedTrustAnchors, actualTrustAnchors) {
		t.Errorf("Content mismatch\nExpected: %+v\nActual: %+v", expectedTrustAnchors, actualTrustAnchors)
	}

	if !reflect.DeepEqual(expectedLinkerdSecrets, actualLinkerdSecrets) {
		t.Errorf("Content mismatch\nExpected: %+v\nActual: %+v", expectedLinkerdSecrets, actualLinkerdSecrets)
	}
}

func assertEqualAdmissionReview(t *testing.T, expected, actual *admissionv1beta1.AdmissionReview) {
	if !reflect.DeepEqual(expected.Request, actual.Request) {
		if !reflect.DeepEqual(expected.Request.Object, actual.Request.Object) {
			t.Errorf("Request object mismatch\nExpected: %s\nActual: %s", expected.Request.Object, actual.Request.Object)
		} else {
			t.Errorf("Request mismatch\nExpected: %+v\nActual: %+v", expected.Request, actual.Request)
		}
	}

	if !reflect.DeepEqual(expected.Response, actual.Response) {
		if actual.Response.Result != nil {
			t.Errorf("Actual response message: %s", actual.Response.Result.Message)
		}
		t.Errorf("Response patch mismatch\nExpected: %s\nActual: %s", expected.Response.Patch, actual.Response.Patch)
	}
}
