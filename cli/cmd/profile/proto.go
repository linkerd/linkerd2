package profile

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/emicklei/proto"
	"github.com/ghodss/yaml"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func renderProto(options *profileOptions, controlPlaneNamespace string, w io.Writer) error {
	var input io.Reader
	if options.proto == "-" {
		input = os.Stdin
	} else {
		var err error
		input, err = os.Open(options.proto)
		if err != nil {
			return err
		}
	}

	// READ PROTO
	parser := proto.NewParser(input)
	definition, err := parser.Parse()
	if err != nil {
		return err
	}

	routes := make([]*sp.RouteSpec, 0)
	pkg := ""

	handle := func(visitee proto.Visitee) {
		switch typed := visitee.(type) {
		case *proto.Package:
			pkg = typed.Name
		case *proto.RPC:
			if service, ok := typed.Parent.(*proto.Service); ok {
				route := &sp.RouteSpec{
					Name: typed.Name,
					Condition: &sp.RequestMatch{
						Method:    http.MethodPost,
						PathRegex: fmt.Sprintf("/%s.%s/%s", pkg, service.Name, typed.Name),
					},
				}
				routes = append(routes, route)
			}
		}
	}

	proto.Walk(definition, handle)

	profile := sp.ServiceProfile{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      fmt.Sprintf("%s.%s.svc.cluster.local", options.name, options.namespace),
			Namespace: controlPlaneNamespace,
		},
		TypeMeta: meta_v1.TypeMeta{
			APIVersion: "linkerd.io/v1alpha1",
			Kind:       "ServiceProfile",
		},
		Spec: sp.ServiceProfileSpec{
			Routes: routes,
		},
	}

	output, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("Error writing Service Profile: %s", err)
	}
	w.Write(output)

	return nil
}
