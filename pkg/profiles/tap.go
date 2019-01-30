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

func RenderTapOutputProfile(client pb.ApiClient, tapResource, namespace, name, controlPlaneNamespace string, tapDuration time.Duration, routeLimit int, w io.Writer) error {
	requestParams := util.TapRequestParams{
		Resource:  tapResource,
		Namespace: namespace,
	}
	log.Debugf("Running `linkerd tap %s --namespace %s`", tapResource, namespace)

	req, err := util.BuildTapByResourceRequest(requestParams)
	if err != nil {
		return err
	}

	profile, err := tapToServiceProfile(client, req, namespace, name, controlPlaneNamespace, tapDuration, routeLimit)
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

func tapToServiceProfile(client pb.ApiClient, tapReq *pb.TapByResourceRequest, namespace, name, controlPlaneNamespace string, tapDuration time.Duration, routeLimit int) (sp.ServiceProfile, error) {
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

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(tapDuration))
	defer cancel()

	tapClient, err := client.TapByResource(context.Background(), tapReq)
	if err != nil {
		return profile, err
	}

	routes := routeSpecFromTap(ctx, tapClient, routeLimit)

	profile.Spec.Routes = routes

	return profile, nil
}

func routeSpecFromTap(ctx context.Context, tapClient pb.Api_TapByResourceClient, routeLimit int) []*sp.RouteSpec {
	routes := make([]*sp.RouteSpec, 0)
	routesMap := make(map[string]*sp.RouteSpec)

recvLoop:
	for {
		select {
		case <-ctx.Done():
			break recvLoop
		default:
			log.Debug("Waiting for data...")
			event, err := tapClient.Recv()

			if err == io.EOF {
				break recvLoop
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				break recvLoop
			}

			routeSpec := getPathDataFromTap(event)

			if routeSpec != nil {
				routesMap[routeSpec.Name] = routeSpec
			}

			if len(routesMap) > routeLimit {
				break recvLoop
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
