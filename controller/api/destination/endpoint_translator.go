package destination

import (
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	defaultWeight uint32 = 10000

	// inboundListenAddr is the environment variable holding the inbound
	// listening address for the proxy container.
	envInboundListenAddr = "LINKERD2_PROXY_INBOUND_LISTEN_ADDR"
	envAdminListenAddr   = "LINKERD2_PROXY_ADMIN_LISTEN_ADDR"
	envControlListenAddr = "LINKERD2_PROXY_CONTROL_LISTEN_ADDR"

	defaultProxyInboundPort = 4143
)

type endpointTranslatorConfig struct {
	controllerNS        string
	identityTrustDomain string
	nodeName            string
	nodeTopologyZone    string
	defaultOpaquePorts  map[uint32]struct{}

	forceOpaqueTransport    bool
	enableH2Upgrade         bool
	enableEndpointFiltering bool
	enableIPv6              bool
	extEndpointZoneWeights  bool

	meshedHTTP2ClientParams *pb.Http2ClientParams
	service                 string
}

// newEndpointTranslatorConfig creates an endpointTranslatorConfig from common
// server config and subscription parameters, reducing boilerplate when creating
// endpoint views.
func newEndpointTranslatorConfig(
	cfg *Config,
	identityTrustDomain string,
	nodeName string,
	nodeTopologyZone string,
	service string,
	enableEndpointFiltering bool,
) *endpointTranslatorConfig {
	return &endpointTranslatorConfig{
		controllerNS:            cfg.ControllerNS,
		identityTrustDomain:     identityTrustDomain,
		nodeName:                nodeName,
		nodeTopologyZone:        nodeTopologyZone,
		defaultOpaquePorts:      cfg.DefaultOpaquePorts,
		forceOpaqueTransport:    cfg.ForceOpaqueTransport,
		enableH2Upgrade:         cfg.EnableH2Upgrade,
		enableEndpointFiltering: enableEndpointFiltering,
		enableIPv6:              cfg.EnableIPv6,
		extEndpointZoneWeights:  cfg.ExtEndpointZoneWeights,
		meshedHTTP2ClientParams: cfg.MeshedHttp2ClientParams,
		service:                 service,
	}
}

var updatesQueueOverflowCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "endpoint_updates_queue_overflow",
		Help: "A counter incremented whenever the endpoint updates queue overflows",
	},
	[]string{
		"service",
	},
)
