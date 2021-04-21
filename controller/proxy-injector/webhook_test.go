package injector

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/inject"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type unmarshalledPatch []map[string]interface{}

var (
	values, _ = linkerd2.NewValues()
)

func confNsEnabled() *inject.ResourceConfig {
	return inject.
		NewResourceConfig(values, inject.OriginWebhook).
		WithNsAnnotations(map[string]string{pkgK8s.ProxyInjectAnnotation: pkgK8s.ProxyInjectEnabled})
}

func confNsDisabled() *inject.ResourceConfig {
	return inject.NewResourceConfig(values, inject.OriginWebhook).WithNsAnnotations(map[string]string{})
}

func confNsWithOpaquePorts() *inject.ResourceConfig {
	return inject.
		NewResourceConfig(values, inject.OriginWebhook).
		WithNsAnnotations(map[string]string{
			pkgK8s.ProxyInjectAnnotation:      pkgK8s.ProxyInjectEnabled,
			pkgK8s.ProxyOpaquePortsAnnotation: "3306",
		})
}

func confNsWithoutOpaquePorts() *inject.ResourceConfig {
	return inject.
		NewResourceConfig(values, inject.OriginWebhook).
		WithNsAnnotations(map[string]string{pkgK8s.ProxyInjectAnnotation: pkgK8s.ProxyInjectEnabled})
}

func confNsWithConfigAnnotations() *inject.ResourceConfig {
	return inject.
		NewResourceConfig(values, inject.OriginWebhook).
		WithNsAnnotations(map[string]string{
			pkgK8s.ProxyInjectAnnotation:                pkgK8s.ProxyInjectEnabled,
			pkgK8s.ProxyIgnoreOutboundPortsAnnotation:   "34567",
			pkgK8s.ProxyWaitBeforeExitSecondsAnnotation: "300",
			"config.linkerd.io/invalid-key":             "invalid-value",
		})
}
func TestGetPodPatch(t *testing.T) {

	values.Proxy.DisableIdentity = true

	factory := fake.NewFactory(filepath.Join("fake", "data"))
	nsEnabled, err := factory.Namespace("namespace-inject-enabled.yaml")
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	nsDisabled, err := factory.Namespace("namespace-inject-disabled.yaml")
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	t.Run("by checking annotations", func(t *testing.T) {
		var testCases = []struct {
			filename string
			ns       *corev1.Namespace
			conf     *inject.ResourceConfig
		}{
			{
				filename: "pod-inject-empty.yaml",
				ns:       nsEnabled,
				conf:     confNsEnabled(),
			},
			{
				filename: "pod-inject-enabled.yaml",
				ns:       nsEnabled,
				conf:     confNsEnabled(),
			},
			{
				filename: "pod-inject-enabled.yaml",
				ns:       nsDisabled,
				conf:     confNsDisabled(),
			},
			{
				filename: "pod-with-debug-disabled.yaml",
				ns:       nsDisabled,
				conf:     confNsDisabled(),
			},
		}

		expectedPatchBytes, err := factory.FileContents("pod.patch.json")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		expectedPatch, err := unmarshalPatch(expectedPatchBytes)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		for id, testCase := range testCases {
			testCase := testCase // pin
			t.Run(fmt.Sprintf("%d", id), func(t *testing.T) {
				pod, err := factory.FileContents(testCase.filename)
				if err != nil {
					t.Fatalf("Unexpected error: %s", err)
				}

				fakeReq := getFakePodReq(pod)
				fullConf := testCase.conf.
					WithKind(fakeReq.Kind.Kind).
					WithOwnerRetriever(ownerRetrieverFake)
				_, err = fullConf.ParseMetaAndYAML(fakeReq.Object.Raw)
				if err != nil {
					t.Fatal(err)
				}

				patchJSON, err := fullConf.GetPodPatch(true)
				if err != nil {
					t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
				}
				actualPatch, err := unmarshalPatch(patchJSON)
				if err != nil {
					t.Fatalf("Unexpected error: %s", err)
				}
				if !reflect.DeepEqual(expectedPatch, actualPatch) {
					t.Fatalf("The actual patch didn't match what was expected.\nExpected: %s\nActual: %s",
						expectedPatchBytes, patchJSON)
				}

			})
		}
	})

	t.Run("by checking annotations with debug", func(t *testing.T) {
		expectedPatchBytes, err := factory.FileContents("pod-with-debug.patch.json")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		expectedPatch, err := unmarshalPatch(expectedPatchBytes)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		pod, err := factory.FileContents("pod-with-debug-enabled.yaml")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		fakeReq := getFakePodReq(pod)
		conf := confNsEnabled().WithKind(fakeReq.Kind.Kind).WithOwnerRetriever(ownerRetrieverFake)
		_, err = conf.ParseMetaAndYAML(fakeReq.Object.Raw)
		if err != nil {
			t.Fatal(err)
		}

		patchJSON, err := conf.GetPodPatch(true)
		if err != nil {
			t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
		}
		actualPatch, err := unmarshalPatch(patchJSON)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		if !reflect.DeepEqual(expectedPatch, actualPatch) {
			t.Fatalf("The actual patch didn't match what was expected.\nExpected: %s\nActual: %s",
				expectedPatchBytes, patchJSON)
		}

	})

	t.Run("by checking pod inherits config annotations from namespace", func(t *testing.T) {
		expectedPatchBytes, err := factory.FileContents("pod-with-ns-annotations.patch.json")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		expectedPatch, err := unmarshalPatch(expectedPatchBytes)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		pod, err := factory.FileContents("pod-inject-enabled.yaml")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		fakeReq := getFakePodReq(pod)
		conf := confNsWithConfigAnnotations().
			WithKind(fakeReq.Kind.Kind).
			WithOwnerRetriever(ownerRetrieverFake)
		_, err = conf.ParseMetaAndYAML(fakeReq.Object.Raw)
		if err != nil {
			t.Fatal(err)
		}

		// The namespace has two config annotations: one valid and one invalid
		// the pod patch should only contain the valid annotation.
		conf.AppendNamespaceAnnotations()
		patchJSON, err := conf.GetPodPatch(true)
		if err != nil {
			t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
		}
		actualPatch, err := unmarshalPatch(patchJSON)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		if !reflect.DeepEqual(expectedPatch, actualPatch) {
			t.Fatalf("The actual patch didn't match what was expected.\nExpected: %s\nActual: %s", expectedPatchBytes, patchJSON)
		}
	})

	t.Run("by checking container spec", func(t *testing.T) {
		deployment, err := factory.FileContents("deployment-with-injected-proxy.yaml")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		fakeReq := getFakePodReq(deployment)
		conf := confNsDisabled().WithKind(fakeReq.Kind.Kind)
		patchJSON, err := conf.GetPodPatch(true)
		if err != nil {
			t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
		}

		if len(patchJSON) == 0 {
			t.Errorf("Expected empty patch")
		}
	})
}

func TestGetAnnotationPatch(t *testing.T) {
	factory := fake.NewFactory(filepath.Join("fake", "data"))
	nsWithOpaquePorts, err := factory.Namespace("namespace-with-opaque-ports.yaml")
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	nsWithoutOpaquePorts, err := factory.Namespace("namespace-inject-enabled.yaml")
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	t.Run("by checking patch annotations", func(t *testing.T) {
		servicePatchBytes, err := factory.FileContents("annotation.patch.json")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		servicePatch, err := unmarshalPatch(servicePatchBytes)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		podPatchBytes, err := factory.FileContents("annotation.patch.json")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		podPatch, err := unmarshalPatch(podPatchBytes)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		var testCases = []struct {
			name               string
			filename           string
			ns                 *corev1.Namespace
			conf               *inject.ResourceConfig
			expectedPatchBytes []byte
			expectedPatch      unmarshalledPatch
		}{
			{
				name:               "service without opaque ports and namespace with",
				filename:           "service-without-opaque-ports.yaml",
				ns:                 nsWithOpaquePorts,
				conf:               confNsWithOpaquePorts(),
				expectedPatchBytes: servicePatchBytes,
				expectedPatch:      servicePatch,
			},
			{
				name:     "service with opaque ports and namespace with",
				filename: "service-with-opaque-ports.yaml",
				ns:       nsWithOpaquePorts,
				conf:     confNsWithOpaquePorts(),
			},
			{
				name:     "service with opaque ports and namespace without",
				filename: "service-with-opaque-ports.yaml",
				ns:       nsWithoutOpaquePorts,
				conf:     confNsWithoutOpaquePorts(),
			},
			{
				name:     "service without opaque ports and namespace without",
				filename: "service-without-opaque-ports.yaml",
				ns:       nsWithoutOpaquePorts,
				conf:     confNsWithoutOpaquePorts(),
			},
			{
				name:               "pod without opaque ports and namespace with",
				filename:           "pod-without-opaque-ports.yaml",
				ns:                 nsWithOpaquePorts,
				conf:               confNsWithOpaquePorts(),
				expectedPatchBytes: podPatchBytes,
				expectedPatch:      podPatch,
			},
			{
				name:     "pod with opaque ports and namespace with",
				filename: "pod-with-opaque-ports.yaml",
				ns:       nsWithOpaquePorts,
				conf:     confNsWithOpaquePorts(),
			},
			{
				name:     "pod with opaque ports and namespace without",
				filename: "pod-with-opaque-ports.yaml",
				ns:       nsWithoutOpaquePorts,
				conf:     confNsWithoutOpaquePorts(),
			},
		}
		for _, testCase := range testCases {
			testCase := testCase // pin
			t.Run(testCase.name, func(t *testing.T) {
				service, err := factory.FileContents(testCase.filename)
				if err != nil {
					t.Fatalf("Unexpected error: %s", err)
				}
				fakeReq := getFakeServiceReq(service)
				fullConf := testCase.conf.
					WithKind(fakeReq.Kind.Kind).
					WithOwnerRetriever(ownerRetrieverFake)
				_, err = fullConf.ParseMetaAndYAML(fakeReq.Object.Raw)
				if err != nil {
					t.Fatal(err)
				}
				var patchJSON []byte
				opaquePorts, ok := fullConf.GetConfigAnnotation(pkgK8s.ProxyOpaquePortsAnnotation)
				if ok {
					fullConf.AppendPodAnnotation(pkgK8s.ProxyOpaquePortsAnnotation, opaquePorts)
					patchJSON, err = fullConf.CreateAnnotationPatch(opaquePorts)
					if err != nil {
						t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
					}
				}
				if len(testCase.expectedPatchBytes) != 0 && len(patchJSON) == 0 {
					t.Fatalf("There was no patch, but one was expected: %s", testCase.expectedPatchBytes)
				} else if len(testCase.expectedPatchBytes) == 0 && len(patchJSON) != 0 {
					t.Fatalf("No patch was expected, but one was returned: %s", patchJSON)
				}
				if len(testCase.expectedPatchBytes) == 0 {
					return
				}
				actualPatch, err := unmarshalPatch(patchJSON)
				if err != nil {
					t.Fatalf("Unexpected error: %s", err)
				}
				if !reflect.DeepEqual(testCase.expectedPatch, actualPatch) {
					t.Fatalf("The actual patch didn't match what was expected.\nExpected: %s\nActual: %s",
						testCase.expectedPatchBytes, patchJSON)
				}
			})
		}
	})
}

func getFakePodReq(b []byte) *admissionv1beta1.AdmissionRequest {
	return &admissionv1beta1.AdmissionRequest{
		Kind:      metav1.GroupVersionKind{Kind: "Pod"},
		Name:      "foobar",
		Namespace: "linkerd",
		Object:    runtime.RawExtension{Raw: b},
	}
}

func getFakeServiceReq(b []byte) *admissionv1beta1.AdmissionRequest {
	return &admissionv1beta1.AdmissionRequest{
		Kind:      metav1.GroupVersionKind{Kind: "Service"},
		Name:      "foobar",
		Namespace: "linkerd",
		Object:    runtime.RawExtension{Raw: b},
	}
}

func ownerRetrieverFake(p *v1.Pod) (string, string) {
	return pkgK8s.Deployment, "owner-deployment"
}

func unmarshalPatch(patchJSON []byte) (unmarshalledPatch, error) {
	var actualPatch unmarshalledPatch
	err := json.Unmarshal(patchJSON, &actualPatch)
	if err != nil {
		return nil, err
	}

	return actualPatch, nil
}
