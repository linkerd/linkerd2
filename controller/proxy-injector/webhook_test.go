package injector

import (
	"fmt"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	"github.com/linkerd/linkerd2/pkg/inject"
	"github.com/linkerd/linkerd2/pkg/k8s"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	factory *fake.Factory
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
		NewResourceConfig(configs).
		WithNsAnnotations(map[string]string{k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled})
}

func confNsDisabled() *inject.ResourceConfig {
	return inject.NewResourceConfig(configs).WithNsAnnotations(map[string]string{})
}

func TestGetPatch(t *testing.T) {
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
				fullConf := testCase.conf.WithKind(fakeReq.Kind.Kind)
				p, _, err := fullConf.GetPatch(fakeReq.Object.Raw, inject.ShouldInjectWebhook)
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
		conf := confNsDisabled().WithKind(fakeReq.Kind.Kind)
		p, _, err := conf.GetPatch(fakeReq.Object.Raw, inject.ShouldInjectWebhook)
		if err != nil {
			t.Fatalf("Unexpected PatchForAdmissionRequest error: %s", err)
		}

		if !p.IsEmpty() {
			t.Errorf("Expected empty patch")
		}
	})
}

// All the cases are tested for full coverage purposes, but the ReplicaSet
// one is the only interesting one, where we actually look into the ReplicaSet's
// ownerReference
func TestParentRefLabel(t *testing.T) {
	t.Run("by checking annotations", func(t *testing.T) {
		testCases := []struct {
			k8sConfigs         []string
			ownerRef           metav1.OwnerReference
			expectedLabelKey   string
			expectedLabelValue string
		}{
			{
				k8sConfigs: []string{`
apiVersion: apps/v1beta1
kind: Deployment
metadata:
  name: emoji-dep
  namespace: emojivoto
`,
				},
				ownerRef: metav1.OwnerReference{
					Kind: "Deployment",
					Name: "emoji-dep",
				},
				expectedLabelKey:   k8s.ProxyDeploymentLabel,
				expectedLabelValue: "emoji-dep",
			},
			{
				k8sConfigs: []string{`
apiVersion: v1
kind: ReplicationController
metadata:
  labels:
    app: emoji-svc
  name: emoji-rc
  namespace: emojivoto
`,
				},
				ownerRef: metav1.OwnerReference{
					Kind: "ReplicationController",
					Name: "emoji-rc",
				},
				expectedLabelKey:   k8s.ProxyReplicationControllerLabel,
				expectedLabelValue: "emoji-rc",
			},
			{
				k8sConfigs: []string{`
apiVersion: extensions/v1beta1
kind: ReplicaSet
metadata:
  labels:
    app: emoji-svc
  name: emoji-rs
  namespace: emojivoto
  ownerReferences:
  - apiVersion: apps/v1
    kind: Deployment
    name: emoji
`,
				},
				ownerRef: metav1.OwnerReference{
					Kind: "ReplicaSet",
					Name: "emoji-rs",
				},
				expectedLabelKey:   k8s.ProxyDeploymentLabel,
				expectedLabelValue: "emoji",
			},
			{
				k8sConfigs: []string{`
apiVersion: batch/v1
kind: Job
metadata:
  name: emoji-job
  namespace: emojivoto
`,
				},
				ownerRef: metav1.OwnerReference{
					Kind: "Job",
					Name: "emoji-job",
				},
				expectedLabelKey:   k8s.ProxyJobLabel,
				expectedLabelValue: "emoji-job",
			},
			{
				k8sConfigs: []string{`
apiVersion: apps/v1beta2
kind: DaemonSet
metadata:
  name: emoji-ds
  namespace: emojivoto
`,
				},
				ownerRef: metav1.OwnerReference{
					Kind: "DaemonSet",
					Name: "emoji-ds",
				},
				expectedLabelKey:   k8s.ProxyDaemonSetLabel,
				expectedLabelValue: "emoji-ds",
			},
			{
				k8sConfigs: []string{`
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: emoji-sts
  namespace: emojivoto
`,
				},
				ownerRef: metav1.OwnerReference{
					Kind: "StatefulSet",
					Name: "emoji-sts",
				},
				expectedLabelKey:   k8s.ProxyStatefulSetLabel,
				expectedLabelValue: "emoji-sts",
			},
		}

		for _, tt := range testCases {
			fakeClient, _, err := k8s.NewFakeClientSets(tt.k8sConfigs...)
			if err != nil {
				t.Fatalf("Error instantiating client: %s", err)
			}
			webhook, err := NewWebhook(fakeClient, "emojivoto", false, true)
			if err != nil {
				t.Fatalf("Error instantiating Webhook: %s", err)
			}
			k, v, err := webhook.parentRefLabel("emojivoto", tt.ownerRef)
			if err != nil {
				t.Fatalf("Error building parent label: %s", err)
			}
			if k != tt.expectedLabelKey {
				t.Fatalf("Expected label key to be \"%s\", got \"%s\"", tt.expectedLabelKey, k)
			}
			if v != tt.expectedLabelValue {
				t.Fatalf("Expected label value to be \"%s\", got \"%s\"", tt.expectedLabelValue, v)
			}
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
