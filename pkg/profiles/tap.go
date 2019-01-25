package profiles

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/ghodss/yaml"
	"github.com/linkerd/linkerd2/controller/api/util"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	log "github.com/sirupsen/logrus"
)

func RenderTapOutputProfile(client pb.ApiClient, tapResource, namespace, name, controlPlaneNamespace string, tapResourceDuration time.Duration, routeLimit int, w io.Writer) error {
	profile := sp.ServiceProfile{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace),
			Namespace: controlPlaneNamespace,
		},
		TypeMeta: meta_v1.TypeMeta{
			APIVersion: "linkerd.io/v1alpha1",
			Kind:       "ServiceProfile",
		},
	}

	res, err := util.BuildResource(namespace, tapResource)
	if err != nil {
		return err
	}

	// there is kind of a duplication of param parsing, because we need to reformulate
	// a request like linkerd profile --tap deploy/web to run the tap
	// linkerd tap deploy --to deploy/web
	requestParams := util.TapRequestParams{
		Resource:    res.Type,
		Namespace:   namespace,
		ToResource:  tapResource,
		ToNamespace: namespace,
	}
	log.Debugf("Running `linkerd tap %s  --namespace %s --to %s --to-namespace %s`", res.Type, namespace, tapResource, namespace)

	req, err := util.BuildTapByResourceRequest(requestParams)
	if err != nil {
		return err
	}
	rsp, err := client.TapByResource(context.Background(), req)
	if err != nil {
		return err
	}

	routes := routeSpecFromTap(rsp, tapResourceDuration, routeLimit)

	profile.Spec.Routes = routes
	output, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("Error writing Service Profile: %s", err)
	}
	w.Write(output)
	return nil
}

func routeSpecFromTap(tapClient pb.Api_TapByResourceClient, tapDuration time.Duration, routeLimit int) []*sp.RouteSpec {
	routes := make([]*sp.RouteSpec, 0)
	routesMap := make(map[string]*sp.RouteSpec)

	tapEventChannel := make(chan *pb.TapEvent, 10)
	timerChannel := make(chan struct{}, 1)

	go func() {
		time.Sleep(tapDuration)
		timerChannel <- struct{}{}
	}()
	go func() {
		recordRoutesFromTap(tapClient, tapEventChannel)
	}()

	stopTap := false
	for {
		select {
		case <-timerChannel:
			stopTap = true
		case event := <-tapEventChannel:
			routeSpec := getPathDataFromTap(event)

			if len(routesMap) > routeLimit {
				stopTap = true
				break
			}

			if routeSpec != nil {
				routesMap[routeSpec.Name] = routeSpec
			}
		default:
			// do nothing
		}
		if stopTap {
			break
		}
	}

	for _, path := range sortMapKeys(routesMap) {
		routes = append(routes, routesMap[path])
	}
	return routes
}

func recordRoutesFromTap(tapClient pb.Api_TapByResourceClient, tapEventChannel chan *pb.TapEvent) {
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

		tapEventChannel <- event
	}
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
