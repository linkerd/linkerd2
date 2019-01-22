package profile

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"sort"

	"github.com/ghodss/yaml"
	"github.com/go-openapi/spec"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var pathParamRegex = regexp.MustCompile(`\\{[^\}]*\\}`)

func renderOpenAPI(options *profileOptions, controlPlaneNamespace string, w io.Writer) error {
	var input io.Reader
	if options.openAPI == "-" {
		input = os.Stdin
	} else {
		var err error
		input, err = os.Open(options.openAPI)
		if err != nil {
			return err
		}
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

	profile := sp.ServiceProfile{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      fmt.Sprintf("%s.%s.svc.cluster.local", options.name, options.namespace),
			Namespace: controlPlaneNamespace,
		},
		TypeMeta: meta_v1.TypeMeta{
			APIVersion: "linkerd.io/v1alpha1",
			Kind:       "ServiceProfile",
		},
	}

	routes := make([]*sp.RouteSpec, 0)

	paths := make([]string, 0)
	if swagger.Paths != nil {
		for path := range swagger.Paths.Paths {
			paths = append(paths, path)
		}
		sort.Strings(paths)
	}

	for _, path := range paths {
		item := swagger.Paths.Paths[path]
		pathRegex := pathToRegex(path)
		if item.Delete != nil {
			spec := mkRouteSpec(path, pathRegex, http.MethodDelete, item.Delete.Responses)
			routes = append(routes, spec)
		}
		if item.Get != nil {
			spec := mkRouteSpec(path, pathRegex, http.MethodGet, item.Get.Responses)
			routes = append(routes, spec)
		}
		if item.Head != nil {
			spec := mkRouteSpec(path, pathRegex, http.MethodHead, item.Head.Responses)
			routes = append(routes, spec)
		}
		if item.Options != nil {
			spec := mkRouteSpec(path, pathRegex, http.MethodOptions, item.Options.Responses)
			routes = append(routes, spec)
		}
		if item.Patch != nil {
			spec := mkRouteSpec(path, pathRegex, http.MethodPatch, item.Patch.Responses)
			routes = append(routes, spec)
		}
		if item.Post != nil {
			spec := mkRouteSpec(path, pathRegex, http.MethodPost, item.Post.Responses)
			routes = append(routes, spec)
		}
		if item.Put != nil {
			spec := mkRouteSpec(path, pathRegex, http.MethodPut, item.Put.Responses)
			routes = append(routes, spec)
		}
	}

	profile.Spec.Routes = routes
	output, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("Error writing Service Profile: %s", err)
	}
	w.Write(output)

	return nil
}

func mkRouteSpec(path, pathRegex string, method string, responses *spec.Responses) *sp.RouteSpec {
	return &sp.RouteSpec{
		Name:            fmt.Sprintf("%s %s", method, path),
		Condition:       toReqMatch(pathRegex, method),
		ResponseClasses: toRspClasses(responses),
	}
}

func pathToRegex(path string) string {
	escaped := regexp.QuoteMeta(path)
	return pathParamRegex.ReplaceAllLiteralString(escaped, "[^/]*")
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
