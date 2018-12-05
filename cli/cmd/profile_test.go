package cmd

import (
	"bytes"
	"errors"
	"fmt"
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

func TestValidateOptions(t *testing.T) {
	options := newProfileOptions()
	err := validateOptions(options)
	exp := errors.New("You must specify exactly one of --template or --open-api")
	if err == nil || err.Error() != exp.Error() {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	err = validateOptions(options)
	options.template = true
	options.openAPI = "openAPI"
	exp = errors.New("You must specify exactly one of --template or --open-api")
	if err == nil || err.Error() != exp.Error() {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	options.template = true
	err = validateOptions(options)
	if err != nil {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	options.openAPI = "openAPI"
	err = validateOptions(options)
	if err != nil {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	options.template = true
	options.name = "service.name"
	err = validateOptions(options)
	if err != nil {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	options.template = true
	options.name = "invalid/name"
	err = validateOptions(options)
	exp = fmt.Errorf("invalid service \"%s\": [may not contain '/']", options.name)
	if err == nil || err.Error() != exp.Error() {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	options.template = true
	options.namespace = "invalid/namespace"
	err = validateOptions(options)
	exp = fmt.Errorf("invalid namespace \"%s\": [may not contain '/']", options.namespace)
	if err == nil || err.Error() != exp.Error() {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}
}
