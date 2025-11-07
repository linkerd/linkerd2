package destination

import (
	"fmt"

	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2-proxy-api/go/net"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	logging "github.com/sirupsen/logrus"
)

func buildClientAdd(log *logging.Entry, cfg *endpointTranslatorConfig, set watcher.AddressSet) *pb.Update {
	addrs := []*pb.WeightedAddr{}
	for _, address := range set.Addresses {
		wa, err := buildWeightedAddr(cfg, address)
		if err != nil {
			log.WithError(err).Error("Failed to build weighted address")
			continue
		}
		applyZoneLocality(cfg, address, wa)
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

func buildClientRemove(log *logging.Entry, set watcher.AddressSet) *pb.Update {
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

func buildWeightedAddr(cfg *endpointTranslatorConfig, address watcher.Address) (*pb.WeightedAddr, error) {
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

func applyZoneLocality(cfg *endpointTranslatorConfig, address watcher.Address, wa *pb.WeightedAddr) {
	if wa.MetricLabels == nil {
		wa.MetricLabels = map[string]string{}
	}

	if cfg.nodeTopologyZone == "" || address.Zone == nil {
		wa.MetricLabels["zone_locality"] = "unknown"
		return
	}

	if *address.Zone == cfg.nodeTopologyZone {
		wa.MetricLabels["zone_locality"] = "local"
		if cfg.extEndpointZoneWeights {
			wa.Weight *= 10
		}
		return
	}

	wa.MetricLabels["zone_locality"] = "remote"
}
