package destination

import (
	"fmt"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	logging "github.com/sirupsen/logrus"
)

// This file contains default strategy implementations for building protobuf updates.
// These strategies are pluggable via the endpointViewConfig function fields.

// defaultBuildClientAdd is the default strategy for building Add updates.
// It converts a set of watcher.Address into a protobuf WeightedAddrSet.
func defaultBuildClientAdd(log *logging.Entry, cfg *endpointViewConfig, set watcher.AddressSet) *pb.Update {
	addrs := []*pb.WeightedAddr{}
	for _, address := range set.Addresses {
		wa, err := buildWeightedAddrFromConfig(cfg, address)
		if err != nil {
			log.WithError(err).Error("Failed to build weighted address")
			continue
		}
		// Apply enhancement strategy (e.g., zone locality)
		// Fallback to defaultEnhanceAddr if not configured
		enhanceAddr := cfg.EnhanceAddr
		if enhanceAddr == nil {
			enhanceAddr = defaultEnhanceAddr
		}
		enhanceAddr(cfg, address, wa)
		addrs = append(addrs, wa)
	}

	add := &pb.Update{Update: &pb.Update_Add{
		Add: &pb.WeightedAddrSet{
			Addrs:        addrs,
			MetricLabels: set.Labels,
		},
	}}

	log.Debugf("Built destination add: %+v", add)
	return add
}

// defaultBuildClientRemove is the default strategy for building Remove updates.
// It converts a set of watcher.Address into a protobuf AddrSet.
func defaultBuildClientRemove(log *logging.Entry, set watcher.AddressSet) *pb.Update {
	addrs := []*net.TcpAddress{}
	for _, address := range set.Addresses {
		tcpAddr, err := toAddr(address)
		if err != nil {
			log.WithError(err).Error("Failed to translate endpoint to addr")
			continue
		}
		addrs = append(addrs, tcpAddr)
	}

	remove := &pb.Update{Update: &pb.Update_Remove{
		Remove: &pb.AddrSet{
			Addrs: addrs,
		},
	}}

	log.Debugf("Built destination remove: %+v", remove)
	return remove
}

// buildWeightedAddrFromConfig converts a watcher.Address to a protobuf WeightedAddr using endpointViewConfig.
// It dispatches to the appropriate builder based on address type (Pod, ExternalWorkload, or bare address).
func buildWeightedAddrFromConfig(cfg *endpointViewConfig, address watcher.Address) (*pb.WeightedAddr, error) {
	switch {
	case address.Pod != nil:
		opaquePorts := watcher.GetAnnotatedOpaquePorts(address.Pod, cfg.defaultOpaquePorts)
		wa, err := createWeightedAddr(address, opaquePorts,
			cfg.forceOpaqueTransport, cfg.enableH2Upgrade, cfg.identityTrustDomain, cfg.controllerNS, cfg.meshedHTTP2ClientParams)
		if err != nil {
			return nil, fmt.Errorf("build weighted addr: %w", err)
		}
		return wa, nil

	case address.ExternalWorkload != nil:
		opaquePorts := watcher.GetAnnotatedOpaquePortsForExternalWorkload(address.ExternalWorkload, cfg.defaultOpaquePorts)
		wa, err := createWeightedAddrForExternalWorkload(address, cfg.forceOpaqueTransport, opaquePorts, cfg.meshedHTTP2ClientParams)
		if err != nil {
			return nil, fmt.Errorf("build weighted addr: %w", err)
		}
		return wa, nil

	default:
		var tcpAddr *net.TcpAddress
		tcpAddr, err := toAddr(address)
		if err != nil {
			return nil, fmt.Errorf("build weighted addr: %w", err)
		}
		var authOverride *pb.AuthorityOverride
		if address.AuthorityOverride != "" {
			authOverride = &pb.AuthorityOverride{AuthorityOverride: address.AuthorityOverride}
		}
		wa := &pb.WeightedAddr{
			Addr:              tcpAddr,
			Weight:            defaultWeight,
			AuthorityOverride: authOverride,
			MetricLabels:      map[string]string{},
		}
		if address.Identity != "" {
			wa.TlsIdentity = &pb.TlsIdentity{
				Strategy: &pb.TlsIdentity_DnsLikeIdentity_{
					DnsLikeIdentity: &pb.TlsIdentity_DnsLikeIdentity{
						Name: address.Identity,
					},
				},
			}
			if cfg.enableH2Upgrade {
				wa.ProtocolHint = &pb.ProtocolHint{
					Protocol: &pb.ProtocolHint_H2_{
						H2: &pb.ProtocolHint_H2{},
					},
				}
			}
			wa.Http2 = cfg.meshedHTTP2ClientParams
		}
		return wa, nil
	}
}

//
// Helper functions for building protobuf weighted addresses from K8s resources
// These are used by the default strategy implementations above.
//
// These are used by the default strategy implementations above.
//
