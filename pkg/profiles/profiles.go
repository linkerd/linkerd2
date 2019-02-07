package profiles

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/golang/protobuf/ptypes/duration"
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	"github.com/linkerd/linkerd2/pkg/util"
	log "github.com/sirupsen/logrus"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type profileTemplateConfig struct {
	ControlPlaneNamespace string
	ServiceNamespace      string
	ServiceName           string
	ClusterZone           string
}

var (
	// DefaultRetryBudget is used for routes which do not specify one.
	DefaultRetryBudget = pb.RetryBudget{
		MinRetriesPerSecond: 10,
		RetryRatio:          0.2,
		Ttl: &duration.Duration{
			Seconds: 10,
		},
	}
	// ServiceProfileMeta is the TypeMeta for the ServiceProfile custom resource.
	ServiceProfileMeta = meta_v1.TypeMeta{
		APIVersion: "linkerd.io/v1alpha1",
		Kind:       "ServiceProfile",
	}
	// DefaultServiceProfile is used for services with no service profile.
	DefaultServiceProfile = pb.DestinationProfile{
		Routes:      []*pb.Route{},
		RetryBudget: &DefaultRetryBudget,
	}
	// DefaultRouteTimeout is the default timeout for routes that do not specify
	// one.
	DefaultRouteTimeout = 10 * time.Second

	minStatus uint32 = 100
	maxStatus uint32 = 599

	clusterZoneSuffix = []string{"svc", "cluster", "local"}

	errRequestMatchField  = errors.New("A request match must have a field set")
	errResponseMatchField = errors.New("A response match must have a field set")
)

func toDuration(d time.Duration) *duration.Duration {
	return &duration.Duration{
		Seconds: int64(d / time.Second),
		Nanos:   int32(d % time.Second),
	}
}

// ToServiceProfile returns a Proxy API DestinationProfile, given a
// ServiceProfile.
func ToServiceProfile(profile *sp.ServiceProfileSpec) (*pb.DestinationProfile, error) {
	routes := make([]*pb.Route, 0)
	for _, route := range profile.Routes {
		pbRoute, err := ToRoute(route)
		if err != nil {
			return nil, err
		}
		routes = append(routes, pbRoute)
	}
	budget := DefaultRetryBudget
	if profile.RetryBudget != nil {
		budget.MinRetriesPerSecond = profile.RetryBudget.MinRetriesPerSecond
		budget.RetryRatio = profile.RetryBudget.RetryRatio
		ttl, err := time.ParseDuration(profile.RetryBudget.TTL)
		if err != nil {
			return nil, err
		}
		budget.Ttl = toDuration(ttl)
	}
	return &pb.DestinationProfile{
		Routes:      routes,
		RetryBudget: &budget,
	}, nil
}

// ToRoute returns a Proxy API Route, given a ServiceProfile Route.
func ToRoute(route *sp.RouteSpec) (*pb.Route, error) {
	cond, err := ToRequestMatch(route.Condition)
	if err != nil {
		return nil, err
	}
	rcs := make([]*pb.ResponseClass, 0)
	for _, rc := range route.ResponseClasses {
		pbRc, err := ToResponseClass(rc)
		if err != nil {
			return nil, err
		}
		rcs = append(rcs, pbRc)
	}
	timeout, err := time.ParseDuration(route.Timeout)
	if err != nil {
		log.Errorf("failed to parse duration for route %s: %s", route.Name, err)
		timeout = DefaultRouteTimeout
	}
	ret := pb.Route{
		Condition:       cond,
		ResponseClasses: rcs,
		MetricsLabels:   map[string]string{"route": route.Name},
		IsRetryable:     route.IsRetryable,
		Timeout:         toDuration(timeout),
	}
	return &ret, nil
}

// ToResponseClass returns a Proxy API ResponseClass, given a ServiceProfile
// ResponseClass.
func ToResponseClass(rc *sp.ResponseClass) (*pb.ResponseClass, error) {
	cond, err := ToResponseMatch(rc.Condition)
	if err != nil {
		return nil, err
	}
	return &pb.ResponseClass{
		Condition: cond,
		IsFailure: rc.IsFailure,
	}, nil
}

// ToResponseMatch returns a Proxy API ResponseMatch, given a ServiceProfile
// ResponseMatch.
func ToResponseMatch(rspMatch *sp.ResponseMatch) (*pb.ResponseMatch, error) {
	if rspMatch == nil {
		return nil, errors.New("missing response match")
	}
	err := ValidateResponseMatch(rspMatch)
	if err != nil {
		return nil, err
	}

	matches := make([]*pb.ResponseMatch, 0)

	if rspMatch.All != nil {
		all := make([]*pb.ResponseMatch, 0)
		for _, m := range rspMatch.All {
			pbM, err := ToResponseMatch(m)
			if err != nil {
				return nil, err
			}
			all = append(all, pbM)
		}
		matches = append(matches, &pb.ResponseMatch{
			Match: &pb.ResponseMatch_All{
				All: &pb.ResponseMatch_Seq{
					Matches: all,
				},
			},
		})
	}

	if rspMatch.Any != nil {
		any := make([]*pb.ResponseMatch, 0)
		for _, m := range rspMatch.Any {
			pbM, err := ToResponseMatch(m)
			if err != nil {
				return nil, err
			}
			any = append(any, pbM)
		}
		matches = append(matches, &pb.ResponseMatch{
			Match: &pb.ResponseMatch_Any{
				Any: &pb.ResponseMatch_Seq{
					Matches: any,
				},
			},
		})
	}

	if rspMatch.Status != nil {
		matches = append(matches, &pb.ResponseMatch{
			Match: &pb.ResponseMatch_Status{
				Status: &pb.HttpStatusRange{
					Max: rspMatch.Status.Max,
					Min: rspMatch.Status.Min,
				},
			},
		})
	}

	if rspMatch.Not != nil {
		not, err := ToResponseMatch(rspMatch.Not)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &pb.ResponseMatch{
			Match: &pb.ResponseMatch_Not{
				Not: not,
			},
		})
	}

	if len(matches) == 0 {
		return nil, errResponseMatchField
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return &pb.ResponseMatch{
		Match: &pb.ResponseMatch_All{
			All: &pb.ResponseMatch_Seq{
				Matches: matches,
			},
		},
	}, nil
}

// ToRequestMatch returns a Proxy API RequestMatch, given a ServiceProfile
// RequestMatch.
func ToRequestMatch(reqMatch *sp.RequestMatch) (*pb.RequestMatch, error) {
	if reqMatch == nil {
		return nil, errors.New("missing request match")
	}
	err := ValidateRequestMatch(reqMatch)
	if err != nil {
		return nil, err
	}

	matches := make([]*pb.RequestMatch, 0)

	if reqMatch.All != nil {
		all := make([]*pb.RequestMatch, 0)
		for _, m := range reqMatch.All {
			pbM, err := ToRequestMatch(m)
			if err != nil {
				return nil, err
			}
			all = append(all, pbM)
		}
		matches = append(matches, &pb.RequestMatch{
			Match: &pb.RequestMatch_All{
				All: &pb.RequestMatch_Seq{
					Matches: all,
				},
			},
		})
	}

	if reqMatch.Any != nil {
		any := make([]*pb.RequestMatch, 0)
		for _, m := range reqMatch.Any {
			pbM, err := ToRequestMatch(m)
			if err != nil {
				return nil, err
			}
			any = append(any, pbM)
		}
		matches = append(matches, &pb.RequestMatch{
			Match: &pb.RequestMatch_Any{
				Any: &pb.RequestMatch_Seq{
					Matches: any,
				},
			},
		})
	}

	if reqMatch.Method != "" {
		matches = append(matches, &pb.RequestMatch{
			Match: &pb.RequestMatch_Method{
				Method: util.ParseMethod(reqMatch.Method),
			},
		})
	}

	if reqMatch.Not != nil {
		not, err := ToRequestMatch(reqMatch.Not)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &pb.RequestMatch{
			Match: &pb.RequestMatch_Not{
				Not: not,
			},
		})
	}

	if reqMatch.PathRegex != "" {
		matches = append(matches, &pb.RequestMatch{
			Match: &pb.RequestMatch_Path{
				Path: &pb.PathMatch{
					Regex: reqMatch.PathRegex,
				},
			},
		})
	}

	if len(matches) == 0 {
		return nil, errRequestMatchField
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return &pb.RequestMatch{
		Match: &pb.RequestMatch_All{
			All: &pb.RequestMatch_Seq{
				Matches: matches,
			},
		},
	}, nil
}

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
	err := yaml.UnmarshalStrict([]byte(data), &serviceProfile)
	if err != nil {
		return fmt.Errorf("failed to validate ServiceProfile: %s", err)
	}

	_, _, err = ValidateName(serviceProfile.Name)
	if err != nil {
		return err
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

// ValidateName validates that a ServiceProfile's name is of the form:
// <service>.<namespace>.svc.cluster.local
func ValidateName(name string) (string, string, error) {
	nameParts := strings.Split(name, ".")
	if len(nameParts) != 2+len(clusterZoneSuffix) {
		return "", "", fmt.Errorf("ServiceProfile \"%s\" has invalid name (must be \"<service>.<namespace>.svc.cluster.local\")", name)
	}
	for i, part := range nameParts[2:] {
		if part != clusterZoneSuffix[i] {
			return "", "", fmt.Errorf("ServiceProfile \"%s\" has invalid name (must be \"<service>.<namespace>.svc.cluster.local\")", name)
		}
	}

	return nameParts[0], nameParts[1], nil
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

func buildConfig(namespace, service, controlPlaneNamespace string) *profileTemplateConfig {
	return &profileTemplateConfig{
		ControlPlaneNamespace: controlPlaneNamespace,
		ServiceNamespace:      namespace,
		ServiceName:           service,
		ClusterZone:           strings.Join(clusterZoneSuffix, "."),
	}
}

// RenderProfileTemplate renders a ServiceProfile template to a buffer, given a
// namespace, service, and control plane namespace.
func RenderProfileTemplate(namespace, service, controlPlaneNamespace string, w io.Writer) error {
	config := buildConfig(namespace, service, controlPlaneNamespace)
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
