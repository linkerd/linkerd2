package cmd

import (
	"bytes"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/pkg/profiles"
)

func TestParseProfile(t *testing.T) {
	var buf bytes.Buffer

	err := profiles.RenderProfileTemplate("myns", "mysvc", "linkerd", &buf)
	if err != nil {
		t.Fatalf("Error rendering service profile template: %v", err)
	}

	var serviceProfile v1alpha1.ServiceProfile
	err = yaml.Unmarshal(buf.Bytes(), &serviceProfile)
	if err != nil {
		t.Fatalf("Error parsing service profile: %v", err)
	}

	expectedServiceProfile := profiles.GenServiceProfile("mysvc", "myns", "linkerd")

	err = profiles.ServiceProfileYamlEquals(serviceProfile, expectedServiceProfile)
	if err != nil {
		t.Fatalf("ServiceProfiles are not equal: %v", err)
	}
}
