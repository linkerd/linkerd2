package destination

import (
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	logging "github.com/sirupsen/logrus"
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

// endpointViewConfig holds configuration and strategy functions for customizing
// endpoint view behavior. Strategy functions can be overridden to provide
// custom implementations (e.g., commercial features, testing).
type endpointViewConfig struct {
	// Configuration
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

	// Strategy functions - can be overridden at construction
	// BuildClientAdd builds a protobuf Add update from a set of addresses
	BuildClientAdd func(log *logging.Entry, cfg *endpointViewConfig, set watcher.AddressSet) *pb.Update

	// BuildClientRemove builds a protobuf Remove update from a set of addresses
	BuildClientRemove func(log *logging.Entry, set watcher.AddressSet) *pb.Update

	// EnhanceAddr allows post-processing of WeightedAddr (e.g., zone locality)
	EnhanceAddr func(cfg *endpointViewConfig, address watcher.Address, wa *pb.WeightedAddr)
}

// newEndpointViewConfig creates an endpointViewConfig from common server config
// and subscription parameters. Strategy functions are set to default
// implementations which can be overridden by the caller.
func newEndpointViewConfig(
	cfg *Config,
	identityTrustDomain string,
	nodeName string,
	nodeTopologyZone string,
	service string,
	enableEndpointFiltering bool,
) *endpointViewConfig {
	viewCfg := &endpointViewConfig{
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

	// Set default strategy implementations
	viewCfg.BuildClientAdd = defaultBuildClientAdd
	viewCfg.BuildClientRemove = defaultBuildClientRemove
	viewCfg.EnhanceAddr = defaultEnhanceAddr

	return viewCfg
}

// Backward compatibility alias
type endpointTranslatorConfig = endpointViewConfig

func newEndpointTranslatorConfig(
	cfg *Config,
	identityTrustDomain string,
	nodeName string,
	nodeTopologyZone string,
	service string,
	enableEndpointFiltering bool,
) *endpointTranslatorConfig {
	return newEndpointViewConfig(cfg, identityTrustDomain, nodeName, nodeTopologyZone, service, enableEndpointFiltering)
}
