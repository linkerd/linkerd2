package k8s

const (
	// ExtensionAPIServerAuthenticationConfigMapName is the name of the ConfigMap where authentication data for extension API servers is placed.
	ExtensionAPIServerAuthenticationConfigMapName = "extension-apiserver-authentication"
	// ExtensionAPIServerAuthenticationRequestHeaderClientCAFileKey is the key that contains the value of the "--requestheader-client-ca-file" flag.
	ExtensionAPIServerAuthenticationRequestHeaderClientCAFileKey = "requestheader-client-ca-file"
)
