package multicluster

// Values contains the top-level elements in the Helm charts
type Values struct {
	GatewayName             string `json:"gatewayName"`
	GatewayNamespace        string `json:"gatewayNamespace"`
	IdentityTrustDomain     string `json:"identityTrustDomain"`
	IncomingPort            uint32 `json:"incomingPort"`
	LinkerdNamespace        string `json:"linkerdNamespace"`
	ProbePath               string `json:"probePath"`
	ProbePeriodSeconds      uint32 `json:"probePeriodSeconds"`
	ProbePort               uint32 `json:"probePort"`
	ProxyOutboundPort       uint32 `json:"proxyOutboundPort"`
	ServiceAccountName      string `json:"serviceAccountName"`
	ServiceAccountNamespace string `json:"serviceAccountNamespace"`
}
