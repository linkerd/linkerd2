package injector

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
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
	configs = &config.All{
		Global: &config.Global{
			LinkerdNamespace: "linkerd",
			CniEnabled:       false,
			IdentityContext:  nil,
		},
		Proxy: &config.Proxy{
			ProxyImage:              &config.Image{ImageName: "gcr.io/linkerd-io/proxy", PullPolicy: "IfNotPresent"},
			ProxyInitImage:          &config.Image{ImageName: "gcr.io/linkerd-io/proxy-init", PullPolicy: "IfNotPresent"},
			ControlPort:             &config.Port{Port: 4190},
			IgnoreInboundPorts:      nil,
			IgnoreOutboundPorts:     nil,
			InboundPort:             &config.Port{Port: 4143},
			AdminPort:               &config.Port{Port: 4191},
			OutboundPort:            &config.Port{Port: 4140},
			Resource:                &config.ResourceRequirements{RequestCpu: "", RequestMemory: "", LimitCpu: "", LimitMemory: ""},
			ProxyUid:                2102,
			LogLevel:                &config.LogLevel{Level: "warn,linkerd2_proxy=info"},
			DisableExternalProfiles: false,
		},
	}
)

func confNsEnabled() *inject.ResourceConfig {
	return inject.
		NewResourceConfig(configs, inject.OriginWebhook).
		WithNsAnnotations(map[string]string{pkgK8s.ProxyInjectAnnotation: pkgK8s.ProxyInjectEnabled})
}

func confNsDisabled() *inject.ResourceConfig {
	return inject.NewResourceConfig(configs, inject.OriginWebhook).WithNsAnnotations(map[string]string{})
}

func TestGetPatch(t *testing.T) {
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
			expected bool
		}{
			{
				filename: "pod-inject-empty.yaml",
				ns:       nsEnabled,
				conf:     confNsEnabled(),
				expected: true,
			},
			{
				filename: "pod-inject-enabled.yaml",
				ns:       nsEnabled,
				conf:     confNsEnabled(),
				expected: true,
			},
			{
				filename: "pod-inject-enabled.yaml",
				ns:       nsDisabled,
				conf:     confNsDisabled(),
				expected: true,
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

				fakeReq := getFakeReq(pod)
				fullConf := testCase.conf.
					WithKind(fakeReq.Kind.Kind).
					WithOwnerRetriever(ownerRetrieverFake)
				_, err = fullConf.ParseMetaAndYAML(fakeReq.Object.Raw)
				if err != nil {
					t.Fatal(err)
				}

				p, err := fullConf.GetPatch(fakeReq.Object.Raw)
				if err != nil {
					t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
				}
				patchJSON, err := p.Marshal()
				if err != nil {
					t.Fatalf("Unexpected Marshal error: %s", err)
				}
				patchStr := string(patchJSON)
				if patchStr != "[]" && !testCase.expected {
					t.Fatalf("Did not expect injection for file '%s'", testCase.filename)
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

	t.Run("by checking container spec", func(t *testing.T) {
		deployment, err := factory.FileContents("deployment-with-injected-proxy.yaml")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		fakeReq := getFakeReq(deployment)
		conf := confNsDisabled().WithKind(fakeReq.Kind.Kind)
		p, err := conf.GetPatch(fakeReq.Object.Raw)
		if err != nil {
			t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
		}

		if !p.IsEmpty() {
			t.Errorf("Expected empty patch")
		}
	})
}

func getFakeReq(b []byte) *admissionv1beta1.AdmissionRequest {
	return &admissionv1beta1.AdmissionRequest{
		Kind:      metav1.GroupVersionKind{Kind: "Pod"},
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
