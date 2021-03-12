package profiles

import (
	"fmt"
	"reflect"

	"github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
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
	if !reflect.DeepEqual(actual, expected) {
		actualYaml, err := yaml.Marshal(actual)
		if err != nil {
			return fmt.Errorf("Service profile mismatch but failed to marshal actual service profile: %v", err)
		}
		expectedYaml, err := yaml.Marshal(expected)
		if err != nil {
			return fmt.Errorf("Service profile mismatch but failed to marshal expected service profile: %v", err)
		}
		return fmt.Errorf("Expected [%s] but got [%s]", string(expectedYaml), string(actualYaml))
	}
	return nil
}
