package profiles

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"path"
	"sort"

	"github.com/go-openapi/spec"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	xLinkerdRetryable = "x-linkerd-retryable"
	xLinkerdTimeout   = "x-linkerd-timeout"
)

// RenderOpenAPI reads an OpenAPI spec file and renders the corresponding
// ServiceProfile to a buffer, given a namespace, service, and control plane
// namespace.
func RenderOpenAPI(fileName, namespace, name, clusterDomain string, w io.Writer) error {

	input, err := readFile(fileName)
	if err != nil {
		return err
	}

	bytes, err := ioutil.ReadAll(input)
	if err != nil {
		return fmt.Errorf("Error reading file: %s", err)
	}
	json, err := yaml.YAMLToJSON(bytes)
	if err != nil {
		return fmt.Errorf("Error parsing yaml: %s", err)
	}

	swagger := spec.Swagger{}
	err = swagger.UnmarshalJSON(json)
	if err != nil {
		return fmt.Errorf("Error parsing OpenAPI spec: %s", err)
	}

	profile := swaggerToServiceProfile(swagger, namespace, name, clusterDomain)

	return writeProfile(profile, w)
}

func swaggerToServiceProfile(swagger spec.Swagger, namespace, name, clusterDomain string) sp.ServiceProfile {
	profile := sp.ServiceProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s.%s.svc.%s", name, namespace, clusterDomain),
			Namespace: namespace,
		},
		TypeMeta: ServiceProfileMeta,
	}

	routes := make([]*sp.RouteSpec, 0)

	paths := make([]string, 0)
	if swagger.Paths != nil {
		for path := range swagger.Paths.Paths {
			paths = append(paths, path)
		}
		sort.Strings(paths)
	}

	for _, relPath := range paths {
		item := swagger.Paths.Paths[relPath]
		path := path.Join(swagger.BasePath, relPath)
		pathRegex := PathToRegex(path)
		if item.Delete != nil {
			spec := MkRouteSpec(path, pathRegex, http.MethodDelete, item.Delete)
			routes = append(routes, spec)
		}
		if item.Get != nil {
			spec := MkRouteSpec(path, pathRegex, http.MethodGet, item.Get)
			routes = append(routes, spec)
		}
		if item.Head != nil {
			spec := MkRouteSpec(path, pathRegex, http.MethodHead, item.Head)
			routes = append(routes, spec)
		}
		if item.Options != nil {
			spec := MkRouteSpec(path, pathRegex, http.MethodOptions, item.Options)
			routes = append(routes, spec)
		}
		if item.Patch != nil {
			spec := MkRouteSpec(path, pathRegex, http.MethodPatch, item.Patch)
			routes = append(routes, spec)
		}
		if item.Post != nil {
			spec := MkRouteSpec(path, pathRegex, http.MethodPost, item.Post)
			routes = append(routes, spec)
		}
		if item.Put != nil {
			spec := MkRouteSpec(path, pathRegex, http.MethodPut, item.Put)
			routes = append(routes, spec)
		}
	}

	profile.Spec.Routes = routes
	return profile
}

// MkRouteSpec makes a service profile route from an OpenAPI operation.
func MkRouteSpec(path, pathRegex string, method string, operation *spec.Operation) *sp.RouteSpec {
	retryable := false
	timeout := ""
	var responses *spec.Responses
	if operation != nil {
		retryable, _ = operation.VendorExtensible.Extensions.GetBool(xLinkerdRetryable)
		timeout, _ = operation.VendorExtensible.Extensions.GetString(xLinkerdTimeout)
		responses = operation.Responses
	}
	return &sp.RouteSpec{
		Name:            fmt.Sprintf("%s %s", method, path),
		Condition:       toReqMatch(pathRegex, method),
		ResponseClasses: toRspClasses(responses),
		IsRetryable:     retryable,
		Timeout:         timeout,
	}
}

func toReqMatch(path string, method string) *sp.RequestMatch {
	return &sp.RequestMatch{
		PathRegex: path,
		Method:    method,
	}
}

func toRspClasses(responses *spec.Responses) []*sp.ResponseClass {
	if responses == nil {
		return nil
	}
	classes := make([]*sp.ResponseClass, 0)

	statuses := make([]int, 0)
	for status := range responses.StatusCodeResponses {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)

	for _, status := range statuses {
		cond := &sp.ResponseMatch{
			Status: &sp.Range{
				Min: uint32(status),
				Max: uint32(status),
			},
		}
		classes = append(classes, &sp.ResponseClass{
			Condition: cond,
			IsFailure: status >= 500,
		})
	}
	return classes
}
