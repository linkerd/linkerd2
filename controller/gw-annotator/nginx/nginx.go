package nginx

import (
	"strings"

	"github.com/linkerd/linkerd2/controller/gw-annotator/gateway"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Gateway represents a Gateway interface implementation for Nginx.
type Gateway struct {
	Object *unstructured.Unstructured
}

// IsAnnotated implements the Gateway interface.
func (g *Gateway) IsAnnotated() bool {
	cs, ok := NewConfigSnippet(g.Object)
	if !ok {
		return false
	}
	return cs.ContainsL5DHeader()
}

// GenerateAnnotationPatch implements the Gateway interface.
func (g *Gateway) GenerateAnnotationPatch(clusterDomain string) (gateway.Patch, error) {
	cs, found := NewConfigSnippet(g.Object)
	op := "add"
	if found {
		op = "replace"
	}

	cs.Entries = append(cs.Entries,
		"proxy_set_header l5d-dst-override $service_name.$namespace.svc."+clusterDomain+":$service_port;",
		"grpc_set_header l5d-dst-override $service_name.$namespace.svc."+clusterDomain+":$service_port;",
	)

	return []gateway.PatchOperation{{
		Op:    op,
		Path:  gateway.AnnotationsPath + strings.Replace(cs.AnnotationKey, "/", "~1", -1),
		Value: cs.ToString(),
	}}, nil
}
