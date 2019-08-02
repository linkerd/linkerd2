package profiles

import (
	"bufio"
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
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/protohttp"
	"github.com/linkerd/linkerd2/pkg/tap"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RenderTapOutputProfile performs a tap on the desired resource and generates
// a service profile with routes pre-populated from the tap data
// Only inbound tap traffic is considered.
func RenderTapOutputProfile(k8sAPI *k8s.KubernetesAPI, tapResource, namespace, name string, tapDuration time.Duration, routeLimit int, w io.Writer) error {
	requestParams := util.TapRequestParams{
		Resource:  tapResource,
		Namespace: namespace,
	}
	log.Debugf("Running `linkerd tap %s --namespace %s`", tapResource, namespace)

	req, err := util.BuildTapByResourceRequest(requestParams)
	if err != nil {
		return err
	}

	profile, err := tapToServiceProfile(k8sAPI, req, namespace, name, tapDuration, routeLimit)
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

func tapToServiceProfile(k8sAPI *k8s.KubernetesAPI, tapReq *pb.TapByResourceRequest, namespace, name string, tapDuration time.Duration, routeLimit int) (sp.ServiceProfile, error) {
	profile := sp.ServiceProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace),
			Namespace: namespace,
		},
		TypeMeta: serviceProfileMeta,
	}

	reader, body, err := tap.Reader(k8sAPI, tapReq, tapDuration)
	if err != nil {
		return profile, err
	}
	defer body.Close()

	routes := routeSpecFromTap(reader, routeLimit)

	profile.Spec.Routes = routes

	return profile, nil
}

func routeSpecFromTap(tapByteStream *bufio.Reader, routeLimit int) []*sp.RouteSpec {
	routes := make([]*sp.RouteSpec, 0)
	routesMap := make(map[string]*sp.RouteSpec)

	for {
		log.Debug("Waiting for data...")
		event := pb.TapEvent{}
		err := protohttp.FromByteStreamToProtocolBuffers(tapByteStream, &event)
		if err != nil {
			// expected errors when hitting the tapDuration deadline
			if err != io.EOF &&
				!strings.HasSuffix(err.Error(), "(Client.Timeout exceeded while reading body)") &&
				!strings.HasSuffix(err.Error(), "http2: response body closed") {
				fmt.Fprintln(os.Stderr, err)
			}
			break
		}

		routeSpec := getPathDataFromTap(&event)
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
