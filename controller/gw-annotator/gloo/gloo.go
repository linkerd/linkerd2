package gloo

import (
	"github.com/linkerd/linkerd2/controller/gw-annotator/gateway"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Gateway represents a Gateway interface implementation for Gloo.
type Gateway struct {
	Object        *unstructured.Unstructured
	ConfigMode    gateway.ConfigMode
	clusterDomain string
}

// SetClusterDomain implements the Gateway interface.
func (g *Gateway) SetClusterDomain(clusterDomain string) {
	g.clusterDomain = clusterDomain
}

// NeedsAnnotation implements the Gateway interface.
func (g *Gateway) NeedsAnnotation() bool {
	// TODO (tegioz)
	return false
}

// GenerateAnnotationPatch implements the Gateway interface.
func (g *Gateway) GenerateAnnotationPatch() (gateway.Patch, error) {
	// TODO (tegioz)
	return nil, nil
}
