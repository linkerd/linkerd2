package cmd

import (
	"bytes"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
)

func TestParseProfile(t *testing.T) {
	templateConfig := BuildConfig("myns", "mysvc")

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

	expectedServiceProfile := GenServiceProfile("mysvc", "myns")

	err = ServiceProfileYamlEquals(serviceProfile, expectedServiceProfile)
	if err != nil {
		t.Fatalf("ServiceProfiles are not equal: %v", err)
	}
}
