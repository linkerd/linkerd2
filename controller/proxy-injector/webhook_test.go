package injector

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	"github.com/linkerd/linkerd2/pkg/k8s"
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

	webhook, err := NewWebhook(fakeClient, testWebhookResources, fake.DefaultControllerNamespace, fake.DefaultNoInitContainer, fake.DefaultTLSEnabled)
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

	webhook, err := NewWebhook(fakeClient, testWebhookResources, fake.DefaultControllerNamespace, fake.DefaultNoInitContainer, fake.DefaultTLSEnabled)
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
	fakeClient := fake.NewClient("")

	webhook, err := NewWebhook(fakeClient, testWebhookResources, fake.DefaultControllerNamespace, fake.DefaultNoInitContainer, fake.DefaultTLSEnabled)
	if err != nil {
		t.Fatal("Unexpected error: ", err)
	}

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
