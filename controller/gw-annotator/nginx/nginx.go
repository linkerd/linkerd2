package nginx

import (
	"strings"

	"github.com/linkerd/linkerd2/controller/gw-annotator/gateway"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Gateway represents a Gateway interface implementation for Nginx.
type Gateway struct {
	Object        *unstructured.Unstructured
	clusterDomain string
}

// SetClusterDomain implements the Gateway interface.
func (g *Gateway) SetClusterDomain(clusterDomain string) {
	g.clusterDomain = clusterDomain
}

// NeedsAnnotation implements the Gateway interface.
func (g *Gateway) NeedsAnnotation() bool {
	cs, ok := NewConfigSnippet(g.Object)
	if !ok {
		return true
	}
	return !cs.ContainsL5DHeader()
}

// GenerateAnnotationPatch implements the Gateway interface.
func (g *Gateway) GenerateAnnotationPatch() (gateway.Patch, error) {
	cs, found := NewConfigSnippet(g.Object)
	op := "add"
	if found {
		op = "replace"
	}

	cs.Entries = append(cs.Entries,
		"proxy_set_header l5d-dst-override $service_name.$namespace.svc."+g.clusterDomain+":$service_port;",
		"grpc_set_header l5d-dst-override $service_name.$namespace.svc."+g.clusterDomain+":$service_port;",
	)

	return []gateway.PatchOperation{{
		Op:    op,
		Path:  gateway.AnnotationsPath + strings.Replace(cs.AnnotationKey, "/", "~1", -1),
		Value: cs.ToString(),
	}}, nil
}
