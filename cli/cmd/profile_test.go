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

func TestProfileFromTap(t *testing.T) {
	var buf bytes.Buffer
	options := newProfileOptions()
	options.name = "service-name"
	options.namespace = "service-namespace"
	options.tap = "not-a-resource/web"

	err := renderTapOutputProfile(options, controlPlaneNamespace, &buf)
	exp := errors.New("target resource invalid: cannot find Kubernetes canonical name from friendly name [not-a-resource]")

	if err.Error() != exp.Error() {
		t.Fatalf("renderTapOutputProfile returned unexpected error: %s (expected: %s)", err, exp)
	}
}
func TestValidateOptions(t *testing.T) {
	options := newProfileOptions()
	exp := errors.New("You must specify exactly one of --template, --open-api, --proto or --tap")
	err := options.validate()
	if err == nil || err.Error() != exp.Error() {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	options.template = true
	options.openAPI = "openAPI"
	exp = errors.New("You must specify exactly one of --template, --open-api, --proto or --tap")
	err = options.validate()
	if err == nil || err.Error() != exp.Error() {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	options.template = true
	exp = errors.New("invalid service \"\": [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]")
	err = options.validate()
	if err == nil || err.Error() != exp.Error() {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	options.template = true
	options.name = "template-name"
	err = options.validate()
	if err != nil {
		t.Fatalf("validateOptions returned unexpected error (%s) for options: %+v", err, options)
	}

	options = newProfileOptions()
	options.template = true
	options.name = "template-name"
	options.namespace = "namespace-name"
	err = options.validate()
	if err != nil {
		t.Fatalf("validateOptions returned unexpected error (%s) for options: %+v", err, options)
	}

	options = newProfileOptions()
	options.openAPI = "openAPI"
	options.name = "openapi-name"
	err = options.validate()
	if err != nil {
		t.Fatalf("validateOptions returned unexpected error (%s) for options: %+v", err, options)
	}

	options = newProfileOptions()
	options.template = true
	options.name = "service.name"
	exp = fmt.Errorf("invalid service \"%s\": [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]", options.name)
	err = options.validate()
	if err == nil || err.Error() != exp.Error() {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	options.template = true
	options.name = "invalid/name"
	exp = fmt.Errorf("invalid service \"%s\": [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]", options.name)
	err = options.validate()
	if err == nil || err.Error() != exp.Error() {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	options.template = true
	options.name = "service-name"
	options.namespace = ""
	exp = fmt.Errorf("invalid namespace \"%s\": [a DNS-1123 label must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character (e.g. 'my-name',  or '123-abc', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?')]", options.namespace)
	err = options.validate()
	if err == nil || err.Error() != exp.Error() {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	options.template = true
	options.name = "service-name"
	options.namespace = "invalid/namespace"
	exp = fmt.Errorf("invalid namespace \"%s\": [a DNS-1123 label must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character (e.g. 'my-name',  or '123-abc', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?')]", options.namespace)
	err = options.validate()
	if err == nil || err.Error() != exp.Error() {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}

	options = newProfileOptions()
	options.template = true
	options.name = "service-name"
	options.namespace = "7eet-ns"
	err = options.validate()
	if err != nil {
		t.Fatalf("validateOptions returned unexpected error (%s) for options: %+v", err, options)
	}

	options = newProfileOptions()
	options.template = true
	options.name = "7eet-svc"
	exp = fmt.Errorf("invalid service \"%s\": [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]", options.name)
	err = options.validate()
	if err == nil || err.Error() != exp.Error() {
		t.Fatalf("validateOptions returned unexpected error: %s (expected: %s) for options: %+v", err, exp, options)
	}
}
