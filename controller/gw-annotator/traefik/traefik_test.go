package traefik

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/controller/gw-annotator/gateway"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestIsAnnotated(t *testing.T) {
	testCases := []struct {
		desc           string
		configMode     gateway.ConfigMode
		annotations    map[string]interface{}
		expectedOutput bool
	}{
		// Ingress
		{
			desc:           "no annotations",
			configMode:     gateway.Ingress,
			annotations:    nil,
			expectedOutput: false,
		},
		{
			desc:       "no custom request headers annotation",
			configMode: gateway.Ingress,
			annotations: map[string]interface{}{
				"k1": "v1",
			},
			expectedOutput: false,
		},
		{
			desc:       "empty custom request headers annotation",
			configMode: gateway.Ingress,
			annotations: map[string]interface{}{
				CustomRequestHeadersKey: "",
			},
			expectedOutput: false,
		},
		{
			desc:       "custom request headers annotation present but no l5d header",
			configMode: gateway.Ingress,
			annotations: map[string]interface{}{
				CustomRequestHeadersKey: "k1:v1||k2:v2",
			},
			expectedOutput: false,
		},
		{
			desc:       "custom request headers annotation present with l5d header",
			configMode: gateway.Ingress,
			annotations: map[string]interface{}{
				CustomRequestHeadersKey: fmt.Sprintf("k1:v1||%s:%s||k2:v2", gateway.L5DHeader, "value"),
			},
			expectedOutput: true,
		},
	}

	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprintf("test_%d: %s", i, tc.desc), func(t *testing.T) {
			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": tc.annotations,
					},
				},
			}
			g := &Gateway{Object: obj, ConfigMode: tc.configMode}
			output := g.IsAnnotated()
			if output != tc.expectedOutput {
				t.Errorf("expecting output to be %v but got %v", tc.expectedOutput, output)
			}
		})
	}
}

func TestGenerateAnnotationPatch(t *testing.T) {
	annotationKey := CustomRequestHeadersKey
	annotationPath := gateway.AnnotationsPath + strings.Replace(annotationKey, "/", "~1", -1)

	testCases := []struct {
		desc           string
		configMode     gateway.ConfigMode
		obj            *unstructured.Unstructured
		clusterDomain  string
		expectedOutput gateway.Patch
		expectedError  error
	}{
		// Ingress
		{
			desc:       "no annotations",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "test-ns",
					},
					"spec": map[string]interface{}{
						"backend": map[string]interface{}{
							"serviceName": "test-svc",
							"servicePort": float64(8888),
						},
					},
				},
			},
			clusterDomain: gateway.DefaultClusterDomain,
			expectedOutput: []gateway.PatchOperation{{
				Op:    "add",
				Path:  annotationPath,
				Value: fmt.Sprintf("%s:%s", gateway.L5DHeader, L5DHeaderTestsValue),
			}},
			expectedError: nil,
		},
		{
			desc:       "no custom request headers annotation",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "test-ns",
						"annotations": map[string]interface{}{
							"k1": "v1",
						},
					},
					"spec": map[string]interface{}{
						"backend": map[string]interface{}{
							"serviceName": "test-svc",
							"servicePort": float64(8888),
						},
					},
				},
			},
			clusterDomain: gateway.DefaultClusterDomain,
			expectedOutput: []gateway.PatchOperation{{
				Op:    "add",
				Path:  annotationPath,
				Value: fmt.Sprintf("%s:%s", gateway.L5DHeader, L5DHeaderTestsValue),
			}},
			expectedError: nil,
		},
		{
			desc:       "no custom request headers annotation (using custom cluster domain)",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "test-ns",
						"annotations": map[string]interface{}{
							"k1": "v1",
						},
					},
					"spec": map[string]interface{}{
						"backend": map[string]interface{}{
							"serviceName": "test-svc",
							"servicePort": float64(8888),
						},
					},
				},
			},
			clusterDomain: "my-domain.org",
			expectedOutput: []gateway.PatchOperation{{
				Op:   "add",
				Path: annotationPath,
				Value: fmt.Sprintf("%s:%s",
					gateway.L5DHeader,
					strings.Replace(L5DHeaderTestsValue, gateway.DefaultClusterDomain, "my-domain.org", -1),
				),
			}},
			expectedError: nil,
		},
		{
			desc:       "no custom request headers annotation (multiple services in ingress)",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "test-ns",
						"annotations": map[string]interface{}{
							"k1": "v1",
						},
					},
					"spec": map[string]interface{}{
						"backend": map[string]interface{}{
							"serviceName": "test-svc",
							"servicePort": float64(8888),
						},
						"rules": []interface{}{
							map[string]interface{}{
								"http": map[string]interface{}{
									"paths": []interface{}{
										map[string]interface{}{
											"backend": map[string]interface{}{
												"serviceName": "test-svc2",
												"servicePort": float64(8888),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			clusterDomain:  gateway.DefaultClusterDomain,
			expectedOutput: nil,
			expectedError:  ErrMultipleServicesFoundInIngress,
		},
		{
			desc:       "no custom request headers annotation (multiple services in ingress - different ports)",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "test-ns",
						"annotations": map[string]interface{}{
							"k1": "v1",
						},
					},
					"spec": map[string]interface{}{
						"backend": map[string]interface{}{
							"serviceName": "test-svc",
							"servicePort": float64(8888),
						},
						"rules": []interface{}{
							map[string]interface{}{
								"http": map[string]interface{}{
									"paths": []interface{}{
										map[string]interface{}{
											"backend": map[string]interface{}{
												"serviceName": "test-svc",
												"servicePort": float64(8889),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			clusterDomain:  gateway.DefaultClusterDomain,
			expectedOutput: nil,
			expectedError:  ErrMultipleServicesFoundInIngress,
		},
		{
			desc:       "no custom request headers annotation (multiple services in ingress - different ports)",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "test-ns",
						"annotations": map[string]interface{}{
							"k1": "v1",
						},
					},
					"spec": map[string]interface{}{
						"rules": []interface{}{
							map[string]interface{}{
								"http": map[string]interface{}{
									"paths": []interface{}{
										map[string]interface{}{
											"backend": map[string]interface{}{
												"serviceName": "test-svc",
												"servicePort": float64(8888),
											},
										},
									},
								},
							},
							map[string]interface{}{
								"http": map[string]interface{}{
									"paths": []interface{}{
										map[string]interface{}{
											"backend": map[string]interface{}{
												"serviceName": "test-svc2",
												"servicePort": float64(8889),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			clusterDomain:  gateway.DefaultClusterDomain,
			expectedOutput: nil,
			expectedError:  ErrMultipleServicesFoundInIngress,
		},
		{
			desc:       "empty custom request headers annotation",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "test-ns",
						"annotations": map[string]interface{}{
							CustomRequestHeadersKey: "",
						},
					},
					"spec": map[string]interface{}{
						"backend": map[string]interface{}{
							"serviceName": "test-svc",
							"servicePort": float64(8888),
						},
					},
				},
			},
			clusterDomain: gateway.DefaultClusterDomain,
			expectedOutput: []gateway.PatchOperation{{
				Op:    "replace",
				Path:  annotationPath,
				Value: fmt.Sprintf("%s:%s", gateway.L5DHeader, L5DHeaderTestsValue),
			}},
			expectedError: nil,
		},
		{
			desc:       "custom request headers annotation present but no l5d header",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "test-ns",
						"annotations": map[string]interface{}{
							CustomRequestHeadersKey: "k1:v1||k2:v2:extra_colon",
						},
					},
					"spec": map[string]interface{}{
						"backend": map[string]interface{}{
							"serviceName": "test-svc",
							"servicePort": float64(8888),
						},
					},
				},
			},
			clusterDomain: gateway.DefaultClusterDomain,
			expectedOutput: []gateway.PatchOperation{{
				Op:    "replace",
				Path:  annotationPath,
				Value: fmt.Sprintf("k1:v1||k2:v2:extra_colon||%s:%s", gateway.L5DHeader, L5DHeaderTestsValue),
			}},
			expectedError: nil,
		},
	}

	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprintf("test_%d: %s", i, tc.desc), func(t *testing.T) {
			g := &Gateway{Object: tc.obj, ConfigMode: gateway.Ingress}
			output, err := g.GenerateAnnotationPatch(tc.clusterDomain)
			if err != tc.expectedError {
				t.Fatalf("expecting error to be %v but got %v", tc.expectedError, err)
			}
			if !reflect.DeepEqual(output, tc.expectedOutput) {
				t.Errorf("expecting output to be\n %v\n but got\n %v", tc.expectedOutput, output)
			}
		})
	}
}
