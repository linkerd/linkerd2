package inject

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-test/deep"
	l5dcharts "github.com/linkerd/linkerd2/pkg/charts/linkerd2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestProduceMergedPatch(t *testing.T) {
	createTestResourceConfig := func(clusterNetworks string) *ResourceConfig {
		values, err := l5dcharts.NewValues()
		if err != nil {
			t.Fatalf("Failed to create test values: %v", err)
		}
		values.ClusterNetworks = clusterNetworks

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-deployment",
				Namespace: "test-ns",
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "app",
								Image: "test:latest",
								Ports: []corev1.ContainerPort{
									{
										Name:          "http",
										ContainerPort: 8080,
									},
								},
							},
						},
					},
				},
			},
		}

		data, err := yaml.Marshal(deployment)
		if err != nil {
			t.Fatalf("Failed to marshal deployment: %v", err)
		}

		resourceConfig := NewResourceConfig(values, OriginCLI, "linkerd")
		if _, err := resourceConfig.ParseMetaAndYAML(data); err != nil {
			t.Fatalf("Failed to parse resource config: %v", err)
		}

		return resourceConfig
	}

	createMockOverrider := func(returnError bool) ValueOverrider {
		return func(values *l5dcharts.Values, overrides map[string]string, namedPorts map[string]int32) (*OverriddenValues, error) {
			if returnError {
				return nil, fmt.Errorf("mock overrider error")
			}
			copy, err := values.DeepCopy()
			if err != nil {
				return nil, err
			}
			overriddenValues := &OverriddenValues{
				Values: copy,
			}
			overriddenValues.Proxy.PodInboundPorts = "8080"
			return overriddenValues, nil
		}
	}

	createMockPatchProducer := func(patch []JSONPatch, returnError bool) PatchProducer {
		return func(conf *ResourceConfig, injectProxy bool, values *OverriddenValues, patchPathPrefix string) ([]byte, error) {
			if returnError {
				return nil, fmt.Errorf("mock patch producer error")
			}
			return json.Marshal(patch)
		}
	}

	testCases := []struct {
		name            string
		producers       []PatchProducer
		resourceConfig  *ResourceConfig
		overrider       ValueOverrider
		expectedError   string
		validatePatch   func(t *testing.T, patch []byte)
		clusterNetworks string
	}{
		{name: "single patch producer - optimization path",
			producers: []PatchProducer{
				createMockPatchProducer([]JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test", Value: "value1"},
				}, false),
			},
			resourceConfig: createTestResourceConfig(""),
			overrider:      createMockOverrider(false),
			validatePatch: func(t *testing.T, patch []byte) {
				var patches []JSONPatch
				if err := json.Unmarshal(patch, &patches); err != nil {
					t.Fatalf("Failed to unmarshal patch: %v", err)
				}
				expected := []JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test", Value: "value1"},
				}
				if diff := deep.Equal(patches, expected); diff != nil {
					t.Errorf("Patch mismatch: %+v", diff)
				}
			},
		},
		{
			name: "multiple patch producers - merge path",
			producers: []PatchProducer{
				createMockPatchProducer([]JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test1", Value: "value1"},
				}, false),
				createMockPatchProducer([]JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test2", Value: "value2"},
				}, false),
				createMockPatchProducer([]JSONPatch{
					{Operation: "add", Path: "/metadata/labels/test", Value: "label1"},
				}, false),
			},
			resourceConfig: createTestResourceConfig(""),
			overrider:      createMockOverrider(false),
			validatePatch: func(t *testing.T, patch []byte) {
				var patches []JSONPatch
				if err := json.Unmarshal(patch, &patches); err != nil {
					t.Fatalf("Failed to unmarshal patch: %v", err)
				}
				expected := []JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test1", Value: "value1"},
					{Operation: "add", Path: "/metadata/annotations/test2", Value: "value2"},
					{Operation: "add", Path: "/metadata/labels/test", Value: "label1"},
				}
				if diff := deep.Equal(patches, expected); diff != nil {
					t.Errorf("Patch mismatch: %+v", diff)
				}
			},
		},
		{
			name: "empty patch handling",
			producers: []PatchProducer{
				createMockPatchProducer([]JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test1", Value: "value1"},
				}, false),
				createMockPatchProducer([]JSONPatch{}, false),
				createMockPatchProducer([]JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test2", Value: "value2"},
				}, false),
			},
			resourceConfig: createTestResourceConfig(""),
			overrider:      createMockOverrider(false),
			validatePatch: func(t *testing.T, patch []byte) {
				var patches []JSONPatch
				if err := json.Unmarshal(patch, &patches); err != nil {
					t.Fatalf("Failed to unmarshal patch: %v", err)
				}
				expected := []JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test1", Value: "value1"},
					{Operation: "add", Path: "/metadata/annotations/test2", Value: "value2"},
				}
				if diff := deep.Equal(patches, expected); diff != nil {
					t.Errorf("Patch mismatch: %+v", diff)
				}
			},
		},
		{
			name:           "empty producers list",
			producers:      []PatchProducer{},
			resourceConfig: createTestResourceConfig(""),
			overrider:      createMockOverrider(false),
			validatePatch: func(t *testing.T, patch []byte) {
				var patches []JSONPatch
				if err := json.Unmarshal(patch, &patches); err != nil {
					t.Fatalf("Failed to unmarshal patch: %v", err)
				}
				if len(patches) != 0 {
					t.Errorf("Expected empty patch, got %d patches", len(patches))
				}
			},
		},
		{
			name: "overrider error",
			producers: []PatchProducer{
				createMockPatchProducer([]JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test", Value: "value1"},
				}, false),
			},
			resourceConfig: createTestResourceConfig(""),
			overrider:      createMockOverrider(true),
			expectedError:  "could not generate Overridden Values: mock overrider error",
		},
		{
			name: "patch producer error",
			producers: []PatchProducer{
				createMockPatchProducer([]JSONPatch{}, true),
			},
			resourceConfig: createTestResourceConfig(""),
			overrider:      createMockOverrider(false),
			expectedError:  "mock patch producer error",
		},
		{
			name: "multiple producers with one error",
			producers: []PatchProducer{
				createMockPatchProducer([]JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test1", Value: "value1"},
				}, false),
				createMockPatchProducer([]JSONPatch{}, true),
			},
			resourceConfig: createTestResourceConfig(""),
			overrider:      createMockOverrider(false),
			expectedError:  "mock patch producer error",
		},
		{
			name: "valid cluster networks",
			producers: []PatchProducer{
				createMockPatchProducer([]JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test", Value: "value1"},
				}, false),
			},
			resourceConfig:  createTestResourceConfig("10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"),
			overrider:       createMockOverrider(false),
			clusterNetworks: "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16",
			validatePatch: func(t *testing.T, patch []byte) {
				var patches []JSONPatch
				if err := json.Unmarshal(patch, &patches); err != nil {
					t.Fatalf("Failed to unmarshal patch: %v", err)
				}
				expected := []JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test", Value: "value1"},
				}
				if diff := deep.Equal(patches, expected); diff != nil {
					t.Errorf("Patch mismatch: %+v", diff)
				}
			},
		},
		{
			name: "invalid cluster networks",
			producers: []PatchProducer{
				createMockPatchProducer([]JSONPatch{
					{Operation: "add", Path: "/metadata/annotations/test", Value: "value1"},
				}, false),
			},
			resourceConfig:  createTestResourceConfig("invalid-cidr,10.0.0.0/8"),
			overrider:       createMockOverrider(false),
			clusterNetworks: "invalid-cidr,10.0.0.0/8",
			expectedError:   "cannot parse destination get networks: invalid CIDR address: invalid-cidr",
		},
	}

	for _, tc := range testCases {
		tc := tc // pin
		t.Run(tc.name, func(t *testing.T) {
			if tc.clusterNetworks != "" {
				tc.resourceConfig.values.ClusterNetworks = tc.clusterNetworks
			}

			patch, err := ProduceMergedPatch(tc.producers, tc.resourceConfig, true, tc.overrider)

			if tc.expectedError != "" {
				if err == nil {
					t.Fatalf("Expected error containing '%s', but got no error", tc.expectedError)
				}
				if err.Error() != tc.expectedError {
					t.Fatalf("Expected error containing '%s', but got '%s'", tc.expectedError, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tc.validatePatch != nil {
				tc.validatePatch(t, patch)
			}
		})
	}
}
