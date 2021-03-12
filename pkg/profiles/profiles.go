package profiles

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"text/template"
	"time"

	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2" // TODO: pkg/profiles should not depend on controller/gen
	"github.com/linkerd/linkerd2/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"
)

var pathParamRegex = regexp.MustCompile(`\\{[^\}]*\\}`)

type profileTemplateConfig struct {
	ServiceNamespace string
	ServiceName      string
	ClusterDomain    string
}

var (
	// ServiceProfileMeta is the TypeMeta for the ServiceProfile custom resource.
	ServiceProfileMeta = metav1.TypeMeta{
		APIVersion: k8s.ServiceProfileAPIVersion,
		Kind:       k8s.ServiceProfileKind,
	}

	minStatus uint32 = 100
	maxStatus uint32 = 599

	errRequestMatchField  = errors.New("A request match must have a field set")
	errResponseMatchField = errors.New("A response match must have a field set")
)

// Validate validates the structure of a ServiceProfile. This code is a superset
// of the validation provided by the `openAPIV3Schema`, defined in the
// ServiceProfile CRD.
// openAPIV3Schema validates:
// - types of non-recursive fields
// - presence of required fields
// This function validates:
// - types of all fields
// - presence of required fields
// - presence of unknown fields
// - recursive fields
func Validate(data []byte) error {
	var serviceProfile sp.ServiceProfile
	err := yaml.UnmarshalStrict(data, &serviceProfile)
	if err != nil {
		return fmt.Errorf("failed to validate ServiceProfile: %s", err)
	}

	errs := validation.IsDNS1123Subdomain(serviceProfile.Name)
	if len(errs) > 0 {
		return fmt.Errorf("ServiceProfile \"%s\" has invalid name: %s", serviceProfile.Name, errs[0])
	}

	if len(serviceProfile.Spec.Routes) == 0 {
		return fmt.Errorf("ServiceProfile \"%s\" has no routes", serviceProfile.Name)
	}

	for _, route := range serviceProfile.Spec.Routes {
		if route.Name == "" {
			return fmt.Errorf("ServiceProfile \"%s\" has a route with no name", serviceProfile.Name)
		}
		if route.Timeout != "" {
			_, err := time.ParseDuration(route.Timeout)
			if err != nil {
				return fmt.Errorf("ServiceProfile \"%s\" has a route with an invalid timeout: %s", serviceProfile.Name, err)
			}
		}
		if route.Condition == nil {
			return fmt.Errorf("ServiceProfile \"%s\" has a route with no condition", serviceProfile.Name)
		}
		err := ValidateRequestMatch(route.Condition)
		if err != nil {
			return fmt.Errorf("ServiceProfile \"%s\" has a route with an invalid condition: %s", serviceProfile.Name, err)
		}
		for _, rc := range route.ResponseClasses {
			if rc.Condition == nil {
				return fmt.Errorf("ServiceProfile \"%s\" has a response class with no condition", serviceProfile.Name)
			}
			err = ValidateResponseMatch(rc.Condition)
			if err != nil {
				return fmt.Errorf("ServiceProfile \"%s\" has a response class with an invalid condition: %s", serviceProfile.Name, err)
			}
		}
	}

	rb := serviceProfile.Spec.RetryBudget
	if rb != nil {
		if rb.RetryRatio < 0 {
			return fmt.Errorf("ServiceProfile \"%s\" RetryBudget RetryRatio must be non-negative: %f", serviceProfile.Name, rb.RetryRatio)
		}

		if rb.TTL == "" {
			return fmt.Errorf("ServiceProfile \"%s\" RetryBudget missing TTL field", serviceProfile.Name)
		}

		_, err := time.ParseDuration(rb.TTL)
		if err != nil {
			return fmt.Errorf("ServiceProfile \"%s\" RetryBudget: %s", serviceProfile.Name, err)
		}
	}

	return nil
}

// ValidateRequestMatch validates whether a ServiceProfile RequestMatch has at
// least one field set.
func ValidateRequestMatch(reqMatch *sp.RequestMatch) error {
	matchKindSet := false
	if reqMatch.All != nil {
		matchKindSet = true
		for _, child := range reqMatch.All {
			err := ValidateRequestMatch(child)
			if err != nil {
				return err
			}
		}
	}
	if reqMatch.Any != nil {
		matchKindSet = true
		for _, child := range reqMatch.Any {
			err := ValidateRequestMatch(child)
			if err != nil {
				return err
			}
		}
	}
	if reqMatch.Method != "" {
		matchKindSet = true
	}
	if reqMatch.Not != nil {
		matchKindSet = true
		err := ValidateRequestMatch(reqMatch.Not)
		if err != nil {
			return err
		}
	}
	if reqMatch.PathRegex != "" {
		matchKindSet = true
	}

	if !matchKindSet {
		return errRequestMatchField
	}

	return nil
}

// ValidateResponseMatch validates whether a ServiceProfile ResponseMatch has at
// least one field set, and sanity checks the Status Range.
func ValidateResponseMatch(rspMatch *sp.ResponseMatch) error {
	matchKindSet := false
	if rspMatch.All != nil {
		matchKindSet = true
		for _, child := range rspMatch.All {
			err := ValidateResponseMatch(child)
			if err != nil {
				return err
			}
		}
	}
	if rspMatch.Any != nil {
		matchKindSet = true
		for _, child := range rspMatch.Any {
			err := ValidateResponseMatch(child)
			if err != nil {
				return err
			}
		}
	}
	if rspMatch.Status != nil {
		if rspMatch.Status.Min != 0 && (rspMatch.Status.Min < minStatus || rspMatch.Status.Min > maxStatus) {
			return fmt.Errorf("Range minimum must be between %d and %d, inclusive", minStatus, maxStatus)
		} else if rspMatch.Status.Max != 0 && (rspMatch.Status.Max < minStatus || rspMatch.Status.Max > maxStatus) {
			return fmt.Errorf("Range maximum must be between %d and %d, inclusive", minStatus, maxStatus)
		} else if rspMatch.Status.Max != 0 && rspMatch.Status.Min != 0 && rspMatch.Status.Max < rspMatch.Status.Min {
			return errors.New("Range maximum cannot be smaller than minimum")
		}
		matchKindSet = true
	}
	if rspMatch.Not != nil {
		matchKindSet = true
		err := ValidateResponseMatch(rspMatch.Not)
		if err != nil {
			return err
		}
	}

	if !matchKindSet {
		return errResponseMatchField
	}

	return nil
}

func buildConfig(namespace, service, clusterDomain string) *profileTemplateConfig {
	return &profileTemplateConfig{
		ServiceNamespace: namespace,
		ServiceName:      service,
		ClusterDomain:    clusterDomain,
	}
}

// RenderProfileTemplate renders a ServiceProfile template to a buffer, given a
// namespace, service, and control plane namespace.
func RenderProfileTemplate(namespace, service, clusterDomain string, w io.Writer) error {
	config := buildConfig(namespace, service, clusterDomain)
	template, err := template.New("profile").Parse(Template)
	if err != nil {
		return err
	}
	buf := &bytes.Buffer{}
	err = template.Execute(buf, config)
	if err != nil {
		return err
	}

	_, err = w.Write(buf.Bytes())
	return err
}

func readFile(fileName string) (io.Reader, error) {
	if fileName == "-" {
		return os.Stdin, nil
	}
	return os.Open(fileName)
}

func writeProfile(profile sp.ServiceProfile, w io.Writer) error {
	output, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("Error writing Service Profile: %s", err)
	}
	_, err = w.Write(output)
	return err
}

// PathToRegex converts a path into a regex.
func PathToRegex(path string) string {
	escaped := regexp.QuoteMeta(path)
	return pathParamRegex.ReplaceAllLiteralString(escaped, "[^/]*")
}
