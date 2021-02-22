package pkg

const (
	// MulticlusterAnnotationsPrefix is the prefix of all multicluster-related annotations
	MulticlusterAnnotationsPrefix = "multicluster.linkerd.io"

	// RemoteResourceVersionAnnotation is the last observed remote resource
	// version of a mirrored resource. Useful when doing updates
	RemoteResourceVersionAnnotation = MulticlusterAnnotationsPrefix + "/remote-resource-version"

	// RemoteGatewayResourceVersionAnnotation is the last observed remote resource
	// version of the gateway for a particular mirrored service. It is used
	// in cases we detect a change in a remote gateway
	RemoteGatewayResourceVersionAnnotation = MulticlusterAnnotationsPrefix + "/remote-gateway-resource-version"

	// GatewayPortName is the name of the incoming port of the gateway
	GatewayPortName = "mc-gateway"

	// ServiceMirrorLabel is the value used in the controller component label
	ServiceMirrorLabel = "servicemirror"
)
