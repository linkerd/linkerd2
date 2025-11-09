package destination

import (
	pb "github.com/linkerd/linkerd2-proxy-api/go/destination"
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
)

// This file contains locality/zone-aware routing strategies for endpoint enhancement.
// The default implementation adds zone locality metrics for observability.
// Commercial builds can override with weight multipliers for zone-aware load balancing.

// defaultEnhanceAddr adds zone locality metric labels to weighted addresses.
// This is the OSS implementation that provides observability without weight modification.
func defaultEnhanceAddr(cfg *endpointViewConfig, address watcher.Address, wa *pb.WeightedAddr) {
	if wa.MetricLabels == nil {
		wa.MetricLabels = map[string]string{}
	}

	// If we don't have zone information, mark as unknown
	if cfg.nodeTopologyZone == "" || address.Zone == nil {
		wa.MetricLabels["zone_locality"] = "unknown"
		return
	}

	// Mark whether the address is in the same zone as the node
	if *address.Zone == cfg.nodeTopologyZone {
		wa.MetricLabels["zone_locality"] = "local"
		// OSS version doesn't modify weights, just adds labels for metrics
		// Commercial builds can set extEndpointZoneWeights=true to multiply weight by 10
		if cfg.extEndpointZoneWeights {
			wa.Weight *= 10
		}
		return
	}

	wa.MetricLabels["zone_locality"] = "remote"
}
