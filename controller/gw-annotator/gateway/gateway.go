package gateway

const (
	// AnnotationsPath represents the path were annotations are located inside
	// the k8s object.
	AnnotationsPath = "/metadata/annotations/"

	// DefaultClusterDomain represents the default cluster domain.
	DefaultClusterDomain = "cluster.local"

	// L5DHeader represents the name of the L5D header.
	L5DHeader = "l5d-dst-override"
)

// ConfigMode indicates how the gateway was configured.
type ConfigMode int

const (
	// CRD indicates the gateway was configured using a CRD.
	CRD ConfigMode = iota

	// Ingress indicates the gateway was configured using an Ingress.
	Ingress

	// ServiceAnnotation indicates the gateway was configured in a Service annotation.
	ServiceAnnotation
)

// Gateway is an abstraction for objects that represent a gateway in k8s. In
// many cases a gateway will be an ingress object, but in some cases it can be
// a service with a special annotation or a controller specific CRD.
type Gateway interface {
	SetClusterDomain(clusterDomain string)
	NeedsAnnotation() bool
	GenerateAnnotationPatch() (Patch, error)
}
