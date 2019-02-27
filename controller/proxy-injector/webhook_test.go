package injector

import (
	"fmt"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	"github.com/linkerd/linkerd2/pkg/inject"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	factory      *fake.Factory
	globalConfig = &config.Global{
		LinkerdNamespace: "linkerd",
		CniEnabled:       false,
		Registry:         "gcr.io/linkerd-io",
		IdentityContext:  nil,
	}
	proxyConfig = &config.Proxy{
		ProxyImage:              &config.Image{ImageName: "gcr.io/linkerd-io/proxy", PullPolicy: "IfNotPresent"},
		ProxyInitImage:          &config.Image{ImageName: "gcr.io/linkerd-io/proxy-init", PullPolicy: "IfNotPresent"},
		DestinationApiPort:      &config.Port{Port: 8086},
		ControlPort:             &config.Port{Port: 4190},
		IgnoreInboundPorts:      nil,
		IgnoreOutboundPorts:     nil,
		InboundPort:             &config.Port{Port: 4143},
		MetricsPort:             &config.Port{Port: 4191},
		OutboundPort:            &config.Port{Port: 4140},
		Resource:                &config.ResourceRequirements{RequestCpu: "", RequestMemory: "", LimitCpu: "", LimitMemory: ""},
		ProxyUid:                2102,
		LogLevel:                &config.LogLevel{Level: "warn,linkerd2_proxy=info"},
		DisableExternalProfiles: false,
	}
)

func confNsEnabled() *inject.ResourceConfig {
	return inject.NewResourceConfig(globalConfig, proxyConfig).WithNsAnnotations(map[string]string{"linkerd.io/inject": "enabled"})
}

func confNsDisabled() *inject.ResourceConfig {
	return inject.NewResourceConfig(globalConfig, proxyConfig).WithNsAnnotations(map[string]string{})
}

func TestShouldInject(t *testing.T) {
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
				filename: "deployment-inject-empty.yaml",
				ns:       nsEnabled,
				conf:     confNsEnabled(),
				expected: true,
			},
			{
				filename: "deployment-inject-enabled.yaml",
				ns:       nsEnabled,
				conf:     confNsEnabled(),
				expected: true,
			},
			{
				filename: "deployment-inject-disabled.yaml",
				ns:       nsEnabled,
				conf:     confNsEnabled(),
				expected: false,
			},
			{
				filename: "deployment-inject-empty.yaml",
				ns:       nsDisabled,
				conf:     confNsDisabled(),
				expected: false,
			},
			{
				filename: "deployment-inject-enabled.yaml",
				ns:       nsDisabled,
				conf:     confNsDisabled(),
				expected: true,
			},
			{
				filename: "deployment-inject-disabled.yaml",
				ns:       nsDisabled,
				conf:     confNsDisabled(),
				expected: false,
			},
		}

		for id, testCase := range testCases {
			testCase := testCase // pin
			t.Run(fmt.Sprintf("%d", id), func(t *testing.T) {
				deployment, err := factory.HTTPRequestBody(testCase.filename)
				if err != nil {
					t.Fatalf("Unexpected error: %s", err)
				}

				fakeReq := getFakeReq(deployment)
				fullConf := testCase.conf.WithMeta(fakeReq.Kind.Kind, fakeReq.Namespace, fakeReq.Name)
				patchJSON, _, err := fullConf.GetPatch(fakeReq.Object.Raw)
				if err != nil {
					t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
				}
				patchStr := string(patchJSON)
				if patchStr != "[]" && !testCase.expected {
					t.Fatalf("Did not expect injection for file '%s'", testCase.filename)
				}
				if patchStr == "[]" && testCase.expected {
					t.Fatalf("Was expecting injection for file '%s'", testCase.filename)
				}
			})
		}
	})

	t.Run("by checking container spec", func(t *testing.T) {
		deployment, err := factory.HTTPRequestBody("deployment-with-injected-proxy.yaml")
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		fakeReq := getFakeReq(deployment)
		conf := confNsDisabled().WithMeta(fakeReq.Kind.Kind, fakeReq.Namespace, fakeReq.Name)
		patchJSON, _, err := conf.GetPatch(fakeReq.Object.Raw)
		if err != nil {
			t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
		}
		if string(patchJSON) != "[]" {
			t.Fatal("Expected deployment with injected proxy to be skipped")
		}
	})
}

func getFakeReq(b []byte) *admissionv1beta1.AdmissionRequest {
	return &admissionv1beta1.AdmissionRequest{
		Kind:      metav1.GroupVersionKind{Kind: "Deployment"},
		Name:      "foobar",
		Namespace: "linkerd",
		Object:    runtime.RawExtension{Raw: b},
	}
}
