package profiles

import (
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/emicklei/proto"
	sp "github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RenderProto reads a protobuf definition file and renders the corresponding
// ServiceProfile to a buffer, given a namespace, service, and control plane
// namespace.
func RenderProto(fileName, namespace, name, clusterDomain string, w io.Writer) error {
	input, err := readFile(fileName)
	if err != nil {
		return err
	}

	parser := proto.NewParser(input)

	profile, err := protoToServiceProfile(parser, namespace, name, clusterDomain)
	if err != nil {
		return err
	}

	return writeProfile(*profile, w)
}

func protoToServiceProfile(parser *proto.Parser, namespace, name, clusterDomain string) (*sp.ServiceProfile, error) {
	definition, err := parser.Parse()
	if err != nil {
		return nil, err
	}

	routes := make([]*sp.RouteSpec, 0)
	pkg := ""

	handle := func(visitee proto.Visitee) {
		switch typed := visitee.(type) {
		case *proto.Package:
			pkg = typed.Name
		case *proto.RPC:
			if service, ok := typed.Parent.(*proto.Service); ok {
				var path string
				switch pkg {
				case "":
					path = fmt.Sprintf("/%s/%s", service.Name, typed.Name)
				default:
					path = fmt.Sprintf("/%s.%s/%s", pkg, service.Name, typed.Name)
				}
				route := &sp.RouteSpec{
					Name: typed.Name,
					Condition: &sp.RequestMatch{
						Method:    http.MethodPost,
						PathRegex: regexp.QuoteMeta(path),
					},
				}
				routes = append(routes, route)
			}
		}
	}

	proto.Walk(definition, handle)

	return &sp.ServiceProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s.%s.svc.%s", name, namespace, clusterDomain),
			Namespace: namespace,
		},
		TypeMeta: ServiceProfileMeta,
		Spec: sp.ServiceProfileSpec{
			Routes: routes,
		},
	}, nil
}
