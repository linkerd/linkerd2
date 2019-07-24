package profiles

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/controller/api/util"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	log "github.com/sirupsen/logrus"
)

// RenderTapOutputProfile performs a tap on the desired resource and generates
// a service profile with routes pre-populated from the tap data
// Only inbound tap traffic is considered.
func RenderTapOutputProfile(client pb.ApiClient, tapResource, namespace, name string, tapDuration time.Duration, routeLimit int, w io.Writer) error {
	requestParams := util.TapRequestParams{
		Resource:  tapResource,
		Namespace: namespace,
	}
	log.Debugf("Running `linkerd tap %s --namespace %s`", tapResource, namespace)

	req, err := util.BuildTapByResourceRequest(requestParams)
	if err != nil {
		return err
	}

	profile, err := tapToServiceProfile(client, req, namespace, name, tapDuration, routeLimit)
	if err != nil {
		return err
	}

	output, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("Error writing Service Profile: %s", err)
	}
	w.Write(output)
	return nil
}

func tapToServiceProfile(client pb.ApiClient, tapReq *pb.TapByResourceRequest, namespace, name string, tapDuration time.Duration, routeLimit int) (sp.ServiceProfile, error) {
	profile := sp.ServiceProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace),
			Namespace: namespace,
		},
		TypeMeta: serviceProfileMeta,
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(tapDuration))
	defer cancel()

	tapClient, err := client.TapByResource(ctx, tapReq)
	if err != nil {
		if strings.HasSuffix(err.Error(), context.DeadlineExceeded.Error()) {
			// return a more user friendly error if we've exceeded the specified duration
			return profile, errors.New("Tap duration exceeded, try increasing --tap-duration")
		}
		return profile, err
	}

	routes := routeSpecFromTap(tapClient, routeLimit)

	profile.Spec.Routes = routes

	return profile, nil
}

func routeSpecFromTap(tapClient pb.Api_TapByResourceClient, routeLimit int) []*sp.RouteSpec {
	routes := make([]*sp.RouteSpec, 0)
	routesMap := make(map[string]*sp.RouteSpec)

	for {
		log.Debug("Waiting for data...")
		event, err := tapClient.Recv()

		if err != nil {
			// expected errors when hitting the tapDuration deadline
			if err != io.EOF &&
				!strings.HasSuffix(err.Error(), context.DeadlineExceeded.Error()) &&
				!strings.HasSuffix(err.Error(), "http2: response body closed") {
				fmt.Fprintln(os.Stderr, err)
			}
			break
		}

		routeSpec := getPathDataFromTap(event)
		log.Debugf("Created route spec: %v", routeSpec)

		if routeSpec != nil {
			routesMap[routeSpec.Name] = routeSpec
			if len(routesMap) >= routeLimit {
				break
			}
		}
	}

	for _, path := range sortMapKeys(routesMap) {
		routes = append(routes, routesMap[path])
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
	if event.GetProxyDirection() != pb.TapEvent_INBOUND {
		return nil
	}

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
