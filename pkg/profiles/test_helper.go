package profiles

import (
	"fmt"

	"github.com/go-test/deep"
	"github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GenServiceProfile generates a mock ServiceProfile.
func GenServiceProfile(service, namespace, clusterDomain string) v1alpha2.ServiceProfile {
	return v1alpha2.ServiceProfile{
		TypeMeta: ServiceProfileMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      service + "." + namespace + ".svc." + clusterDomain,
			Namespace: namespace,
		},
		Spec: v1alpha2.ServiceProfileSpec{
			Routes: []*v1alpha2.RouteSpec{
				{
					Name: "/authors/{id}",
					Condition: &v1alpha2.RequestMatch{
						PathRegex: "/authors/\\d+",
						Method:    "POST",
					},
					ResponseClasses: []*v1alpha2.ResponseClass{
						{
							Condition: &v1alpha2.ResponseMatch{
								Status: &v1alpha2.Range{
									Min: 500,
									Max: 599,
								},
							},
							IsFailure: true,
						},
					},
				},
			},
		},
	}
}

// ServiceProfileYamlEquals validates whether two ServiceProfiles are equal.
func ServiceProfileYamlEquals(actual, expected v1alpha2.ServiceProfile) error {
	if diff := deep.Equal(actual, expected); diff != nil {
		return fmt.Errorf("ServiceProfile mismatch: %+v", diff)
	}
	return nil
}
