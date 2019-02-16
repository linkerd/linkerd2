package injector

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	"github.com/linkerd/linkerd2/pkg/k8s"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

var (
	factory *fake.Factory
)

func TestShouldInject(t *testing.T) {
	nsEnabled, err := factory.Namespace("namespace-inject-enabled.yaml")
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	nsDisabled, err := factory.Namespace("namespace-inject-disabled.yaml")
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	fakeClient := fake.NewClient("", nsEnabled, nsDisabled)

	webhook, err := NewWebhook(fakeClient, testWebhookResources, fake.DefaultControllerNamespace, fake.DefaultNoInitContainer)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	t.Run("by checking annotations", func(t *testing.T) {
		var testCases = []struct {
			filename string
			ns       *corev1.Namespace
			expected bool
		}{
			{
				filename: "deployment-inject-empty.yaml",
				ns:       nsEnabled,
				expected: true,
			},
			{
				filename: "deployment-inject-enabled.yaml",
				ns:       nsEnabled,
				expected: true,
			},
			{
				filename: "deployment-inject-disabled.yaml",
				ns:       nsEnabled,
				expected: false,
			},
			{
				filename: "deployment-inject-empty.yaml",
				ns:       nsDisabled,
				expected: false,
			},
			{
				filename: "deployment-inject-enabled.yaml",
				ns:       nsDisabled,
				expected: true,
			},
			{
				filename: "deployment-inject-disabled.yaml",
				ns:       nsDisabled,
				expected: false,
			},
		}

		for id, testCase := range testCases {
			t.Run(fmt.Sprintf("%d", id), func(t *testing.T) {
				deployment, err := factory.Deployment(testCase.filename)
				if err != nil {
					t.Fatalf("Unexpected error: %s", err)
				}

				inject, err := webhook.shouldInject(testCase.ns.GetName(), deployment)
				if err != nil {
					t.Fatalf("Unexpected shouldInject error: %s", err)
				}
				if inject != testCase.expected {
					t.Fatalf("Boolean mismatch. Expected: %t. Actual: %t", testCase.expected, inject)
				}
			})
		}
	})

	t.Run("by checking container spec", func(t *testing.T) {
		deployment, err := factory.Deployment("deployment-with-injected-proxy.yaml")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		inject, err := webhook.shouldInject(nsEnabled.GetName(), deployment)
		if err != nil {
			t.Fatalf("Unexpected shouldInject error: %s", err)
		}
		if inject {
			t.Fatal("Expected deployment with injected proxy to be skipped")
		}
	})
}

func TestContainersSpec(t *testing.T) {
	fakeClient := fake.NewClient("")

	webhook, err := NewWebhook(fakeClient, testWebhookResources, fake.DefaultControllerNamespace, fake.DefaultNoInitContainer)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	expectedSidecar, err := factory.Container("inject-sidecar-container-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	expectedInit, err := factory.Container("inject-init-container-spec.yaml")
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

	id := k8s.TLSIdentity{
		Name:                "nginx",
		Kind:                "deployment",
		Namespace:           fake.DefaultNamespace,
		ControllerNamespace: fake.DefaultControllerNamespace,
	}.ToDNSName()

	actualSidecar, actualInit, err := webhook.containersSpec(id)
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
