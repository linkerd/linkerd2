package gloo

import (
	"github.com/linkerd/linkerd2/controller/gw-annotator/gateway"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Gateway represents a Gateway interface implementation for Gloo.
type Gateway struct {
	Object     *unstructured.Unstructured
	ConfigMode gateway.ConfigMode
}

// NeedsAnnotation implements the Gateway interface.
func (g *Gateway) NeedsAnnotation() bool {
	// TODO (tegioz)
	return false
}

// GenerateAnnotationPatch implements the Gateway interface.
func (g *Gateway) GenerateAnnotationPatch(clusterDomain string) (gateway.Patch, error) {
	// TODO (tegioz)
	return nil, nil
}
