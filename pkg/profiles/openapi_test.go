package profiles

import (
	"testing"

	"github.com/go-openapi/spec"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSwaggerToServiceProfile(t *testing.T) {
	namespace := "myns"
	name := "mysvc"
	clusterDomain := "mycluster.local"

	swagger := spec.Swagger{
		SwaggerProps: spec.SwaggerProps{
			Paths: &spec.Paths{
				Paths: map[string]spec.PathItem{
					"/authors/{id}": {
						PathItemProps: spec.PathItemProps{
							Post: &spec.Operation{
								OperationProps: spec.OperationProps{
									Responses: &spec.Responses{
										ResponsesProps: spec.ResponsesProps{
											StatusCodeResponses: map[int]spec.Response{
												500: {},
											},
										},
									},
								},
								VendorExtensible: spec.VendorExtensible{
									Extensions: spec.Extensions{xLinkerdRetryable: true, xLinkerdTimeout: "60s"},
								},
							},
						},
					},
				},
			},
		},
	}

	expectedServiceProfile := sp.ServiceProfile{
		TypeMeta: ServiceProfileMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "." + namespace + ".svc." + clusterDomain,
			Namespace: namespace,
		},
		Spec: sp.ServiceProfileSpec{
			Routes: []*sp.RouteSpec{
				{
					Name: "POST /authors/{id}",
					Condition: &sp.RequestMatch{
						PathRegex: "/authors/[^/]*",
						Method:    "POST",
					},
					ResponseClasses: []*sp.ResponseClass{
						{
							Condition: &sp.ResponseMatch{
								Status: &sp.Range{
									Min: 500,
									Max: 500,
								},
							},
							IsFailure: true,
						},
					},
					IsRetryable: true,
					Timeout:     "60s",
				},
			},
		},
	}

	actualServiceProfile := swaggerToServiceProfile(swagger, namespace, name, clusterDomain)

	err := ServiceProfileYamlEquals(actualServiceProfile, expectedServiceProfile)
	if err != nil {
		t.Fatalf("ServiceProfiles are not equal: %v", err)
	}
}
