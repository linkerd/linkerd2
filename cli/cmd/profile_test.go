package cmd

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestParseProfile(t *testing.T) {
	templateConfig := buildConfig("myns", "mysvc")

	var buf bytes.Buffer

	err := renderProfileTemplate(templateConfig, &buf)
	if err != nil {
		t.Fatalf("Error rendering service profile template: %v", err)
	}

	var serviceProfile v1alpha1.ServiceProfile
	err = yaml.Unmarshal(buf.Bytes(), &serviceProfile)
	if err != nil {
		t.Fatalf("Error parsing service profile: %v", err)
	}

	expectedServiceProfile := v1alpha1.ServiceProfile{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "linkerd.io/v1alpha1",
			Kind:       "ServiceProfile",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mysvc.myns.svc.cluster.local",
			Namespace: controlPlaneNamespace,
		},
		Spec: v1alpha1.ServiceProfileSpec{
			Routes: []*v1alpha1.RouteSpec{
				&v1alpha1.RouteSpec{
					Name: "/authors/{id}",
					Condition: &v1alpha1.RequestMatch{
						Path:   "^/authors/\\d+$",
						Method: "POST",
					},
					ResponseClasses: []*v1alpha1.ResponseClass{
						&v1alpha1.ResponseClass{
							Condition: &v1alpha1.ResponseMatch{
								Status: &v1alpha1.Range{
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

	if !reflect.DeepEqual(serviceProfile, expectedServiceProfile) {

		acutalYaml, err := yaml.Marshal(serviceProfile)
		if err != nil {
			t.Fatalf("Service profile mismatch but failed to marshal actual service profile: %v", err)
		}
		expectedYaml, err := yaml.Marshal(expectedServiceProfile)
		if err != nil {
			t.Fatalf("Serivce profile mismatch but failed to marshal expected service profile: %v", err)
		}
		t.Fatalf("Expected [%s] but got [%s]", string(expectedYaml), string(acutalYaml))
	}
}
