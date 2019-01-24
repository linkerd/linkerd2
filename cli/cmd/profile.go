package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/ghodss/yaml"
	"github.com/go-openapi/spec"
	"github.com/linkerd/linkerd2/controller/api/util"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/pkg/profiles"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

type templateConfig struct {
	ControlPlaneNamespace string
	ServiceNamespace      string
	ServiceName           string
	ClusterZone           string
}

var pathParamRegex = regexp.MustCompile(`\\{[^\}]*\\}`)

type profileOptions struct {
	name        string
	namespace   string
	template    bool
	openAPI     string
	tap         string
	tapDuration string
}

func newProfileOptions() *profileOptions {
	return &profileOptions{
		name:        "",
		namespace:   "default",
		template:    false,
		openAPI:     "",
		tap:         "",
		tapDuration: "5s",
	}
}

func (options *profileOptions) validate() error {
	outputs := 0
	if options.template {
		outputs++
	}
	if options.openAPI != "" {
		outputs++
	}
	if options.tap != "" {
		outputs++
	}
	if outputs != 1 {
		return errors.New("You must specify exactly one of --template or --open-api or --tap")
	}

	// a DNS-1035 label must consist of lower case alphanumeric characters or '-',
	// start with an alphabetic character, and end with an alphanumeric character
	if errs := validation.IsDNS1035Label(options.name); len(errs) != 0 {
		return fmt.Errorf("invalid service %q: %v", options.name, errs)
	}

	// a DNS-1123 label must consist of lower case alphanumeric characters or '-',
	// and must start and end with an alphanumeric character
	if errs := validation.IsDNS1123Label(options.namespace); len(errs) != 0 {
		return fmt.Errorf("invalid namespace %q: %v", options.namespace, errs)
	}

	if _, err := time.ParseDuration(options.tapDuration); err != nil {
		return fmt.Errorf("invalid duration %q: %v", options.tapDuration, err)
	}

	return nil
}

func newCmdProfile() *cobra.Command {

	options := newProfileOptions()

	cmd := &cobra.Command{
		Use:   "profile [flags] (--template | --open-api file | --tap resource) (SERVICE)",
		Short: "Output service profile config for Kubernetes",
		Long: `Output service profile config for Kubernetes.

This outputs a service profile for the given service.

If the --template flag is specified, it outputs a service profile template.
Edit the template and then apply it with kubectl to add a service profile to
a service.

If the --tap flag is specified, it runs linkerd tap target for --tap-duration seconds,
and creates a profile for the SERVICE based on the requests seen in that window

Example:
  linkerd profile -n emojivoto --template web-svc > web-svc-profile.yaml
  # (edit web-svc-profile.yaml manually)
  kubectl apply -f web-svc-profile.yaml

If the --open-api flag is specified, it reads the given OpenAPI
specification file and outputs a corresponding service profile.

Example:
	linkerd profile --tap deploy/books --tap-duration 10s books > book-svc-profile.yaml
	# (edit book-svc-profile.yaml manualy)
	kubectl apply -f book-svc-profile.yaml

The command will run linkerd tap deploy/books for tap-duration seconds, and then create
a service profile for the books service with routes prepopulated from the tap data.

Example:
  linkerd profile -n emojivoto --open-api web-svc.swagger web-svc | kubectl apply -f -`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.name = args[0]

			err := options.validate()
			if err != nil {
				return err
			}

			if options.template {
				return profiles.RenderProfileTemplate(options.namespace, options.name, controlPlaneNamespace, os.Stdout)
			} else if options.openAPI != "" {
				return renderOpenAPI(options, os.Stdout)
			} else if options.tap != "" {
				return renderTapOutputProfile(options, controlPlaneNamespace, os.Stdout)
			}

			// we should never get here
			return errors.New("Unexpected error")
		},
	}

	cmd.PersistentFlags().BoolVar(&options.template, "template", options.template, "Output a service profile template")
	cmd.PersistentFlags().StringVar(&options.openAPI, "open-api", options.openAPI, "Output a service profile based on the given OpenAPI spec file")
	cmd.PersistentFlags().StringVar(&options.tap, "tap", options.tap, "Output a service profile based on tap data for the given target resource")
	cmd.PersistentFlags().StringVarP(&options.tapDuration, "tap-duration", "t", options.tapDuration, "Duration over which tap data is collected (for example: \"10s\", \"1m\", \"10m\")")
	cmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the service")

	return cmd
}

func renderTapOutputProfile(options *profileOptions, controlPlaneNamespace string, w io.Writer) error {
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

	log.Debugf("Running `linkerd tap %s --namespace %s`", options.tap, options.namespace)
	client := cliPublicAPIClient()
	requestParams := util.TapRequestParams{
		Resource:  options.tap,
		Namespace: options.namespace,
	}
	req, err := util.BuildTapByResourceRequest(requestParams)
	rsp, err := client.TapByResource(context.Background(), req)
	if err != nil {
		return err
	}
	tapDuration, _ := time.ParseDuration(options.tapDuration) // err discarded because validation has already occurred
	routes := routeSpecFromTap(w, rsp, tapDuration)

	profile.Spec.Routes = routes
	output, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("Error writing Service Profile: %s", err)
	}
	w.Write(output)
	return nil
}

func routeSpecFromTap(w io.Writer, tapClient pb.Api_TapByResourceClient, tapDuration time.Duration) []*sp.RouteSpec {
	routes := make([]*sp.RouteSpec, 0)
	routesMap := recordRoutesFromTap(tapClient, tapDuration)

	for _, path := range sortMapKeys(routesMap) {
		routes = append(routes, routesMap[path])
	}
	return routes
}

func recordRoutesFromTap(tapClient pb.Api_TapByResourceClient, tapDuration time.Duration) map[string]*sp.RouteSpec {
	routes := make(map[string]*sp.RouteSpec)
	timerChannel := make(chan string, 1)
	go func() {
		time.Sleep(tapDuration)
		timerChannel <- "done"
	}()
	done := ""

	for {
		log.Debug("Waiting for data...")
		event, err := tapClient.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			break
		}
		routeSpec := getPathDataFromTap(event)

		if routeSpec != nil {
			routes[routeSpec.Name] = routeSpec
		}

		select {
		case done = <-timerChannel:
		default:
			// do nothing
		}
		if done != "" {
			break
		}
	}

	return routes
}

func sortMapKeys(m map[string]*sp.RouteSpec) (keys []string) {
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return
}

func getPathDataFromTap(event *pb.TapEvent) *sp.RouteSpec {
	switch ev := event.GetHttp().GetEvent().(type) {
	case *pb.TapEvent_Http_RequestInit_:
		path := ev.RequestInit.GetPath()
		if path == "/" {
			return nil
		}
		return mkRouteSpec(
			path,
			pathToRegex(path), // for now, no path consolidation
			ev.RequestInit.GetMethod().GetRegistered().String(),
			nil)
	default:
		return nil
	}
}

func renderOpenAPI(options *profileOptions, w io.Writer) error {
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
