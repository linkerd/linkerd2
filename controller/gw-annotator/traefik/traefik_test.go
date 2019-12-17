package traefik

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/linkerd/linkerd2/controller/gw-annotator/gateway"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestNeedsAnnotation(t *testing.T) {
	testCases := []struct {
		desc           string
		configMode     gateway.ConfigMode
		obj            *unstructured.Unstructured
		expectedOutput bool
	}{
		// Ingress
		{
			desc:       "no annotations",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": nil,
					},
				},
			},
			expectedOutput: true,
		},
		{
			desc:       "no custom request headers annotation",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": map[string]interface{}{
							"k1": "v1",
						},
					},
				},
			},
			expectedOutput: true,
		},
		{
			desc:       "empty custom request headers annotation",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": map[string]interface{}{
							CustomRequestHeadersKey: "",
						},
					},
				},
			},
			expectedOutput: true,
		},
		{
			desc:       "custom request headers annotation present but no l5d header",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": map[string]interface{}{
							CustomRequestHeadersKey: "k1:v1||k2:v2",
						},
					},
				},
			},
			expectedOutput: true,
		},
		{
			desc:       "custom request headers annotation present with invalid l5d header (svc and port changed)",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "test-ns",
						"annotations": map[string]interface{}{
							CustomRequestHeadersKey: fmt.Sprintf("k1:v1||%s:%s||k2:v2", gateway.L5DHeader, L5DHeaderTestsValue),
						},
					},
					"spec": map[string]interface{}{
						"backend": map[string]interface{}{
							"serviceName": "test-svc2",
							"servicePort": float64(8889),
						},
					},
				},
			},
			expectedOutput: true,
		},
		{
			desc:       "custom request headers annotation present with valid l5d header",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "test-ns",
						"annotations": map[string]interface{}{
							CustomRequestHeadersKey: fmt.Sprintf("k1:v1||%s:%s||k2:v2", gateway.L5DHeader, L5DHeaderTestsValue),
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
			expectedOutput: false,
		},
	}

	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprintf("test_%d: %s", i, tc.desc), func(t *testing.T) {
			g := &Gateway{Object: tc.obj, ConfigMode: tc.configMode}
			g.SetClusterDomain(gateway.DefaultClusterDomain)
			output := g.NeedsAnnotation()
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
		{
			desc:       "custom request headers annotation present with invalid l5d header (svc and port changed)",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "test-ns",
						"annotations": map[string]interface{}{
							CustomRequestHeadersKey: fmt.Sprintf("k1:v1||%s:%s||k2:v2", gateway.L5DHeader, "test-svc2:8889"),
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
				Value: fmt.Sprintf("k1:v1||k2:v2||%s:%s", gateway.L5DHeader, L5DHeaderTestsValue),
			}},
			expectedError: nil,
		},
		{
			desc:       "custom request headers annotation present with invalid l5d header (multiple services in ingress - different ports)",
			configMode: gateway.Ingress,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "test-ns",
						"annotations": map[string]interface{}{
							CustomRequestHeadersKey: fmt.Sprintf("%s:%s", gateway.L5DHeader, L5DHeaderTestsValue),
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
			clusterDomain: gateway.DefaultClusterDomain,
			expectedOutput: []gateway.PatchOperation{{
				Op:    "replace",
				Path:  annotationPath,
				Value: fmt.Sprintf("%s:", gateway.L5DHeader),
			}},
			expectedError: nil,
		},
	}

	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprintf("test_%d: %s", i, tc.desc), func(t *testing.T) {
			g := &Gateway{Object: tc.obj, ConfigMode: gateway.Ingress}
			g.SetClusterDomain(tc.clusterDomain)
			output, err := g.GenerateAnnotationPatch()
			if err != tc.expectedError {
				t.Fatalf("expecting error to be %v but got %v", tc.expectedError, err)
			}
			if !reflect.DeepEqual(output, tc.expectedOutput) {
				t.Errorf("expecting output to be\n %v\n but got\n %v", tc.expectedOutput, output)
			}
		})
	}
}
