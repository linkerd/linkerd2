package charts

import "time"

const (
	// LinkerdIdentityIssuerType is the type that represents the linkerd-identity service as the certificate issuer
	LinkerdIdentityIssuerType string = "linkerd"
	// AwsAcmPcaIdentityIssuerType is the type that represents Aws Acm Pca as the certificate issuer
	AwsAcmPcaIdentityIssuerType string = "awsacmpca"
)

type (
	// Values contains the top-level elements in the Helm charts
	Values struct {
		Namespace        string
		ClusterDomain    string
		HighAvailability bool
		Identity         *Identity

		Proxy     *Proxy
		ProxyInit *ProxyInit
	}

	// Proxy contains the fields to set the proxy sidecar container
	Proxy struct {
		Capabilities           *Capabilities
		Component              string
		DisableIdentity        bool
		DisableTap             bool
		EnableExternalProfiles bool
		Image                  *Image
		LogLevel               string
		SAMountPath            *SAMountPath
		Ports                  *Ports
		Resources              *Resources
		UID                    int64
	}

	// ProxyInit contains the fields to set the proxy-init container
	ProxyInit struct {
		Capabilities        *Capabilities
		IgnoreInboundPorts  string
		IgnoreOutboundPorts string
		Image               *Image
		SAMountPath         *SAMountPath
		Resources           *Resources
	}

	// DebugContainer contains the fields to set the debugging sidecar
	DebugContainer struct {
		Image *Image
	}

	// Image contains the details to define a container image
	Image struct {
		Name       string
		PullPolicy string
		Version    string
	}

	// Ports contains all the port-related setups
	Ports struct {
		Admin    int32
		Control  int32
		Inbound  int32
		Outbound int32
	}

	// Constraints wraps the Limit and Request settings for computational resources
	Constraints struct {
		Limit   string
		Request string
	}

	// Capabilities contains the SecurityContext capabilities to add/drop into the injected
	// containers
	Capabilities struct {
		Add  []string
		Drop []string
	}

	// SAMountPath contains the details for ServiceAccount volume mount
	SAMountPath struct {
		Name      string
		MountPath string
		ReadOnly  bool
	}

	// Resources represents the computational resources setup for a given container
	Resources struct {
		CPU    Constraints
		Memory Constraints
	}

	// Identity contains the fields to set the identity variables in the proxy
	// sidecar container
	Identity struct {
		TrustAnchorsPEM string
		TrustDomain     string
		Issuer          *Issuer
	}

	// Issuer has the Helm variables of the identity issuer
	Issuer struct {
		IssuanceLifetime string
		IssuerType       string
	}

	// LinkerdIdentityIssuer has the Helm variables of the linkerd identity issuer
	LinkerdIdentityIssuer struct {
		ClockSkewAllowance  string
		CrtExpiryAnnotation string
		CrtExpiry           time.Time
		TLS                 *TLS
	}

	// AwsAcmPcaIdentityIssuer has the Helm variables of the aws acm pca identity issuer
	AwsAcmPcaIdentityIssuer struct {
		CaArn    string
		CaRegion string
	}

	// ProxyInjector has all the proxy injector's Helm variables
	ProxyInjector struct {
		*TLS
	}

	// ProfileValidator has all the profile validator's Helm variables
	ProfileValidator struct {
		*TLS
	}

	// Tap has all the Tap's Helm variables
	Tap struct {
		*TLS
	}

	// TLS has a pair of PEM-encoded key and certificate variables used in the
	// Helm templates
	TLS struct {
		KeyPEM, CrtPEM string
	}
)
