package injector

import (
	"fmt"
	"testing"

	"github.com/linkerd/linkerd2/controller/gen/config"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/linkerd/linkerd2/controller/proxy-injector/fake"
	"github.com/linkerd/linkerd2/pkg/inject"
	pkgK8s "github.com/linkerd/linkerd2/pkg/k8s"
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
		WithNsAnnotations(map[string]string{pkgK8s.ProxyInjectAnnotation: pkgK8s.ProxyInjectEnabled})
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

func TestGetLabelForParent(t *testing.T) {
	t.Run("by checking annotations", func(t *testing.T) {
		testCases := []struct {
			k8sConfigs         []string
			pod                string
			expectedLabelKey   string
			expectedLabelValue string
		}{
			{
				k8sConfigs: []string{},
				pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji-svc
  namespace: emojivoto
  ownerReferences:                                                      
  - apiVersion: apps/v1beta2
    kind: Deployment                                                    
    name: emoji-deploy`,
				expectedLabelKey:   pkgK8s.ProxyDeploymentLabel,
				expectedLabelValue: "emoji-deploy",
			},
			{
				k8sConfigs: []string{},
				pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji-svc
  namespace: emojivoto
  ownerReferences:                                                      
  - apiVersion: v1
    kind: ReplicationController                                                    
    name: emoji-rc`,
				expectedLabelKey:   pkgK8s.ProxyReplicationControllerLabel,
				expectedLabelValue: "emoji-rc",
			},
			{
				k8sConfigs: []string{`
apiVersion: apps/v1beta2
kind: ReplicaSet
metadata:
  name: emoji-rs
  namespace: emojivoto
  ownerReferences:
  - apiVersion: apps/v1beta2
    kind: Deployment
    name: emoji-deploy`,
				},
				pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji-svc
  namespace: emojivoto
  ownerReferences:                                                      
  - apiVersion: apps/v1beta2
    kind: ReplicaSet                                                    
    name: emoji-rs`,
				expectedLabelKey:   pkgK8s.ProxyDeploymentLabel,
				expectedLabelValue: "emoji-deploy",
			},
			{
				k8sConfigs: []string{},
				pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji-svc
  namespace: emojivoto
  ownerReferences:                                                      
  - apiVersion: batch/v1
    kind: Job                                                
    name: emoji-job`,
				expectedLabelKey:   pkgK8s.ProxyJobLabel,
				expectedLabelValue: "emoji-job",
			},
			{
				k8sConfigs: []string{},
				pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji-svc
  namespace: emojivoto
  ownerReferences:                                                      
  - apiVersion: apps/v1
    kind: DaemonSet                                         
    name: emoji-ds`,
				expectedLabelKey:   pkgK8s.ProxyDaemonSetLabel,
				expectedLabelValue: "emoji-ds",
			},
			{
				k8sConfigs: []string{},
				pod: `
apiVersion: v1
kind: Pod
metadata:
  name: emoji-svc
  namespace: emojivoto
  ownerReferences:                                                      
  - apiVersion: apps/v1
    kind: StatefulSet                                         
    name: emoji-sts`,
				expectedLabelKey:   pkgK8s.ProxyStatefulSetLabel,
				expectedLabelValue: "emoji-sts",
			},
		}

		for _, tt := range testCases {
			k8sConfigs := append(tt.k8sConfigs, tt.pod)
			k8sAPI, err := k8s.NewFakeAPI(k8sConfigs...)
			if err != nil {
				t.Fatalf("Error instantiating client: %s", err)
			}
			k8sAPI.Sync()

			webhook, err := NewWebhook(k8sAPI, "emojivoto", false)
			if err != nil {
				t.Fatalf("Error instantiating Webhook: %s", err)
			}

			b := []byte(tt.pod)

			conf := confNsEnabled()
			nonEmpty, err := conf.ParseMeta(b, "")
			if err != nil {
				t.Fatal(err)
			}
			if !nonEmpty {
				t.Fatalf("Unexpected empty result from ParseMeta()")
			}

			if err := conf.Parse(b); err != nil {
				t.Fatalf("Error in parse(): %s", err)
			}

			k, v, err := webhook.getLabelForParent(conf)
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
