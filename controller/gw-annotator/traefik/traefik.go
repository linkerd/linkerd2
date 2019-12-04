package traefik

import (
	"errors"
	"fmt"
	"strings"

	"github.com/linkerd/linkerd2/controller/gw-annotator/gateway"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// HeadersSeparator represents the headers separator used by Traefik.
	HeadersSeparator = "||"
)

var (
	// ErrMultipleServicesFoundInIngress error indicates that multiple services
	// (or the same using different ports) appear in the ingress spec backends,
	// so it's not possible to annotate this gateway as the L5D header can only
	// specify one service and port.
	ErrMultipleServicesFoundInIngress = errors.New("multiple services found in ingress")
)

// Gateway represents a Gateway interface implementation for Traefik.
type Gateway struct {
	Object     *unstructured.Unstructured
	ConfigMode gateway.ConfigMode
}

// IsAnnotated implements the Gateway interface.
func (g *Gateway) IsAnnotated() bool {
	switch g.ConfigMode {
	case gateway.Ingress:
		headers, ok := NewCustomRequestHeaders(g.Object, HeadersSeparator)
		if ok {
			return headers.ContainsL5DHeader()
		}
	}
	return false
}

// GenerateAnnotationPatch implements the Gateway interface.
func (g *Gateway) GenerateAnnotationPatch(clusterDomain string) (gateway.Patch, error) {
	g.Object.GetNamespace()
	switch g.ConfigMode {
	case gateway.Ingress:
		headers, found := NewCustomRequestHeaders(g.Object, HeadersSeparator)
		op := "add"
		if found {
			op = "replace"
		}
		service, port, err := g.getIngressServiceAndPort()
		if err != nil {
			return nil, err
		}
		headers[gateway.L5DHeader] = fmt.Sprintf("%s.%s.svc.%s:%.0f",
			service,
			g.Object.GetNamespace(),
			clusterDomain,
			port)

		return []gateway.PatchOperation{{
			Op:    op,
			Path:  gateway.AnnotationsPath + strings.Replace(CustomRequestHeadersKey, "/", "~1", -1),
			Value: headers.ToString(HeadersSeparator),
		}}, nil
	}
	return nil, nil
}

func (g *Gateway) getIngressServiceAndPort() (string, float64, error) {
	// Get all backends configured in ingress
	var backends []map[string]interface{}

	// Default backend
	defaultBackend, ok, _ := unstructured.NestedMap(g.Object.Object, "spec", "backend")
	if ok {
		backends = append(backends, defaultBackend)
	}

	// Paths backends in ingress rules
	ingressRules, ok, _ := unstructured.NestedSlice(g.Object.Object, "spec", "rules")
	if ok {
		for _, rule := range ingressRules {
			paths, ok, _ := unstructured.NestedSlice(rule.(map[string]interface{}), "http", "paths")
			if ok {
				for _, path := range paths {
					pathBackend, ok, _ := unstructured.NestedMap(path.(map[string]interface{}), "backend")
					if ok {
						backends = append(backends, pathBackend)
					}
				}
			}
		}
	}

	// Return service name and port if it's available and unique
	type option struct {
		serviceName string
		servicePort float64
	}
	options := make(map[option]struct{})
	for _, backend := range backends {
		if backend["serviceName"] != "" && backend["servicePort"] != "" {
			options[option{
				serviceName: backend["serviceName"].(string),
				servicePort: backend["servicePort"].(float64),
			}] = struct{}{}
		}
	}
	if len(options) == 1 {
		for o := range options {
			return o.serviceName, o.servicePort, nil
		}
	}
	return "", 0, ErrMultipleServicesFoundInIngress
}
