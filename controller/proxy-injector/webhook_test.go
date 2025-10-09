package injector

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/go-test/deep"
	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	"github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	"github.com/linkerd/linkerd2/pkg/inject"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type unmarshalledPatch []map[string]interface{}

var (
	values, _ = linkerd2.NewValues()
)

func confNsEnabled() *inject.ResourceConfig {
	return inject.
		NewResourceConfig(values, inject.OriginWebhook, "linkerd").
		WithNsAnnotations(map[string]string{
			pkgK8s.ProxyInjectAnnotation: pkgK8s.ProxyInjectEnabled,
		})
}

func confNsDisabled() *inject.ResourceConfig {
	return inject.NewResourceConfig(values, inject.OriginWebhook, "linkerd").
		WithNsAnnotations(map[string]string{})
}

func confNsWithOpaquePorts() *inject.ResourceConfig {
	return inject.
		NewResourceConfig(values, inject.OriginWebhook, "linkerd").
		WithNsAnnotations(map[string]string{
			pkgK8s.ProxyInjectAnnotation:      pkgK8s.ProxyInjectEnabled,
			pkgK8s.ProxyOpaquePortsAnnotation: "3306",
		})
}

func confNsWithoutOpaquePorts() *inject.ResourceConfig {
	return inject.
		NewResourceConfig(values, inject.OriginWebhook, "linkerd").
		WithNsAnnotations(map[string]string{
			pkgK8s.ProxyInjectAnnotation: pkgK8s.ProxyInjectEnabled,
		})
}

func confNsWithConfigAnnotations() *inject.ResourceConfig {
	return inject.
		NewResourceConfig(values, inject.OriginWebhook, "linkerd").
		WithNsAnnotations(map[string]string{
			pkgK8s.ProxyInjectAnnotation:                pkgK8s.ProxyInjectEnabled,
			pkgK8s.ProxyIgnoreOutboundPortsAnnotation:   "34567",
			pkgK8s.ProxyWaitBeforeExitSecondsAnnotation: "300",
			"config.linkerd.io/invalid-key":             "invalid-value",
		})
}
func TestGetPodPatch(t *testing.T) {

	values.IdentityTrustAnchorsPEM = "IdentityTrustAnchorsPEM"

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

		_, expectedPatch := loadPatch(factory, t, "pod.patch.json")
		for _, testCase := range testCases {
			testCase := testCase // pin
			t.Run(testCase.filename, func(t *testing.T) {
				pod := fileContents(factory, t, testCase.filename)
				fakeReq := getFakePodReq(pod)
				fullConf := testCase.conf.
					WithKind(fakeReq.Kind.Kind).
					WithRootOwnerRetriever(rootOwnerRetrieverFake)
				_, err = fullConf.ParseMetaAndYAML(fakeReq.Object.Raw)
				if err != nil {
					t.Fatal(err)
				}

				patchJSON, err := fullConf.GetPodPatch(true, inject.GetOverriddenValues)
				if err != nil {
					t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
				}
				actualPatch := unmarshalPatch(t, patchJSON)
				if diff := deep.Equal(expectedPatch, actualPatch); diff != nil {
					t.Fatalf("The actual patch didn't match what was expected.\n%+v", diff)
				}
			})
		}
	})

	t.Run("by checking annotations with debug", func(t *testing.T) {
		_, expectedPatch := loadPatch(factory, t, "pod-with-debug.patch.json")

		pod := fileContents(factory, t, "pod-with-debug-enabled.yaml")
		fakeReq := getFakePodReq(pod)
		conf := confNsEnabled().WithKind(fakeReq.Kind.Kind).
			WithRootOwnerRetriever(rootOwnerRetrieverFake)
		_, err = conf.ParseMetaAndYAML(fakeReq.Object.Raw)
		if err != nil {
			t.Fatal(err)
		}

		patchJSON, err := conf.GetPodPatch(true, inject.GetOverriddenValues)
		if err != nil {
			t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
		}
		actualPatch := unmarshalPatch(t, patchJSON)
		if diff := deep.Equal(expectedPatch, actualPatch); diff != nil {
			t.Fatalf("The actual patch didn't match what was expected.\n%+v", diff)
		}
	})

	t.Run("by checking annotations with custom debug image version", func(t *testing.T) {
		_, expectedPatch := loadPatch(factory, t, "pod-with-custom-debug.patch.json")

		pod := fileContents(factory, t, "pod-with-custom-debug-tag.yaml")
		fakeReq := getFakePodReq(pod)
		conf := confNsEnabled().WithKind(fakeReq.Kind.Kind).
			WithRootOwnerRetriever(rootOwnerRetrieverFake)
		_, err = conf.ParseMetaAndYAML(fakeReq.Object.Raw)
		if err != nil {
			t.Fatal(err)
		}

		patchJSON, err := conf.GetPodPatch(true, inject.GetOverriddenValues)
		if err != nil {
			t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
		}
		actualPatch := unmarshalPatch(t, patchJSON)
		if diff := deep.Equal(expectedPatch, actualPatch); diff != nil {
			t.Fatalf("The actual patch didn't match what was expected.\n%+v", diff)
		}
	})

	t.Run("by configuring log level", func(t *testing.T) {
		_, expectedPatch := loadPatch(factory, t, "pod-log-level.json")

		pod := fileContents(factory, t, "pod-inject-enabled-log-level.yaml")
		fakeReq := getFakePodReq(pod)
		conf := confNsWithoutOpaquePorts().
			WithKind(fakeReq.Kind.Kind).
			WithRootOwnerRetriever(rootOwnerRetrieverFake)
		_, err = conf.ParseMetaAndYAML(fakeReq.Object.Raw)
		if err != nil {
			t.Fatal(err)
		}

		patchJSON, err := conf.GetPodPatch(true, inject.GetOverriddenValues)
		if err != nil {
			t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
		}

		actualPatch := unmarshalPatch(t, patchJSON)
		if diff := deep.Equal(expectedPatch, actualPatch); diff != nil {
			t.Fatalf("The actual patch didn't match what was expected.\n%+v", diff)
		}
	})

	t.Run("by configuring cpu limit by ratio", func(t *testing.T) {
		_, expectedPatch := loadPatch(factory, t, "pod-cpu-ratio.json")

		pod := fileContents(factory, t, "pod-inject-enabled-cpu-ratio.yaml")
		fakeReq := getFakePodReq(pod)
		conf := confNsWithoutOpaquePorts().
			WithKind(fakeReq.Kind.Kind).
			WithRootOwnerRetriever(rootOwnerRetrieverFake)
		_, err = conf.ParseMetaAndYAML(fakeReq.Object.Raw)
		if err != nil {
			t.Fatal(err)
		}

		patchJSON, err := conf.GetPodPatch(true, inject.GetOverriddenValues)
		if err != nil {
			t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
		}

		actualPatch := unmarshalPatch(t, patchJSON)
		if diff := deep.Equal(expectedPatch, actualPatch); diff != nil {
			expectedJSON, _ := json.MarshalIndent(expectedPatch, "", "  ")
			actualJSON, _ := json.MarshalIndent(actualPatch, "", "  ")
			t.Fatalf("Expected:\n%s\n\nActual:\n%s\n\nDiff:%+v",
				string(expectedJSON), string(actualJSON), diff)
		}
	})

	t.Run("by checking pod inherits config annotations from namespace", func(t *testing.T) {
		_, expectedPatch := loadPatch(factory, t, "pod-with-ns-annotations.patch.json")

		pod := fileContents(factory, t, "pod-inject-enabled.yaml")
		fakeReq := getFakePodReq(pod)
		conf := confNsWithConfigAnnotations().
			WithKind(fakeReq.Kind.Kind).
			WithRootOwnerRetriever(rootOwnerRetrieverFake)
		_, err = conf.ParseMetaAndYAML(fakeReq.Object.Raw)
		if err != nil {
			t.Fatal(err)
		}

		// The namespace has two config annotations: one valid and one invalid
		// the pod patch should only contain the valid annotation.
		inject.AppendNamespaceAnnotations(conf.GetOverrideAnnotations(), conf.GetNsAnnotations(), conf.GetWorkloadAnnotations())
		patchJSON, err := conf.GetPodPatch(true, inject.GetOverriddenValues)
		if err != nil {
			t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
		}
		actualPatch := unmarshalPatch(t, patchJSON)
		if diff := deep.Equal(expectedPatch, actualPatch); diff != nil {
			t.Fatalf("The actual patch didn't match what was expected.\n+%v", diff)
		}
	})

	t.Run("by checking container spec", func(t *testing.T) {
		deployment := fileContents(factory, t, "deployment-with-injected-proxy.yaml")
		fakeReq := getFakePodReq(deployment)
		conf := confNsDisabled().WithKind(fakeReq.Kind.Kind)
		patchJSON, err := conf.GetPodPatch(true, inject.GetOverriddenValues)
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
		servicePatchBytes, servicePatch := loadPatch(factory, t, "annotation.patch.json")
		podPatchBytes, podPatch := loadPatch(factory, t, "annotation.patch.json")
		filteredServiceBytes, filteredServicePatch := loadPatch(factory, t, "filtered-service-opaque-ports.json")
		filteredPodBytes, filteredPodPatch := loadPatch(factory, t, "filtered-pod-opaque-ports.json")
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
			{
				name:     "pod without opaque ports and namespace without",
				filename: "pod-without-opaque-ports.yaml",
				ns:       nsWithoutOpaquePorts,
				conf:     confNsWithoutOpaquePorts(),
			},
			{
				name:               "service opaque ports are filtered",
				filename:           "filter-service-opaque-ports.yaml",
				ns:                 nsWithoutOpaquePorts,
				conf:               confNsWithoutOpaquePorts(),
				expectedPatchBytes: filteredServiceBytes,
				expectedPatch:      filteredServicePatch,
			},
			{
				name:               "pod opaque ports are filtered",
				filename:           "filter-pod-opaque-ports.yaml",
				ns:                 nsWithoutOpaquePorts,
				conf:               confNsWithoutOpaquePorts(),
				expectedPatchBytes: filteredPodBytes,
				expectedPatch:      filteredPodPatch,
			},
		}
		for _, testCase := range testCases {
			testCase := testCase // pin
			t.Run(testCase.name, func(t *testing.T) {
				service := fileContents(factory, t, testCase.filename)
				fakeReq := getFakeServiceReq(service)
				fullConf := testCase.conf.
					WithKind(fakeReq.Kind.Kind).
					WithRootOwnerRetriever(rootOwnerRetrieverFake)
				_, err = fullConf.ParseMetaAndYAML(fakeReq.Object.Raw)
				if err != nil {
					t.Fatal(err)
				}
				patchJSON, err := fullConf.CreateOpaquePortsPatch()
				if err != nil {
					t.Fatalf("Unexpected error creating default opaque ports patch: %s", err)
				}
				if len(testCase.expectedPatchBytes) != 0 && len(patchJSON) == 0 {
					t.Fatalf("There was no patch, but one was expected: %s", testCase.expectedPatchBytes)
				} else if len(testCase.expectedPatchBytes) == 0 && len(patchJSON) != 0 {
					t.Fatalf("No patch was expected, but one was returned: %s", patchJSON)
				}
				if len(testCase.expectedPatchBytes) == 0 {
					return
				}
				actualPatch := unmarshalPatch(t, patchJSON)
				if diff := deep.Equal(testCase.expectedPatch, actualPatch); diff != nil {
					t.Fatalf("The actual patch didn't match what was expected.\n%+v", diff)
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

func rootOwnerRetrieverFake(tm *metav1.TypeMeta, om *metav1.ObjectMeta) (*metav1.TypeMeta, *metav1.ObjectMeta) {
	return &metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"}, &metav1.ObjectMeta{Name: "owner-deployment"}
}

func loadPatch(factory *fake.Factory, t *testing.T, name string) ([]byte, unmarshalledPatch) {
	t.Helper()
	bytes := fileContents(factory, t, name)
	patch := unmarshalPatch(t, bytes)
	return bytes, patch
}

func fileContents(factory *fake.Factory, t *testing.T, name string) []byte {
	t.Helper()
	b, err := factory.FileContents(name)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	return b
}

func unmarshalPatch(t *testing.T, patchJSON []byte) unmarshalledPatch {
	t.Helper()
	var actualPatch unmarshalledPatch
	if err := json.Unmarshal(patchJSON, &actualPatch); err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	return actualPatch
}
