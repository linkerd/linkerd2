package destination

import (
	"reflect"

	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

func selectAddressFamily(cfg *endpointTranslatorConfig, addresses watcher.AddressSet) watcher.AddressSet {
	filtered := make(map[watcher.ID]watcher.Address)
	for id, addr := range addresses.Addresses {
		if id.IPFamily == corev1.IPv6Protocol && !cfg.enableIPv6 {
			continue
		}

		if id.IPFamily == corev1.IPv4Protocol && cfg.enableIPv6 {
			// Only consider IPv4 address for which there's not already an IPv6
			// alternative.
			altID := id
			altID.IPFamily = corev1.IPv6Protocol
			if _, ok := addresses.Addresses[altID]; ok {
				continue
			}
		}

		filtered[id] = addr
	}

	return watcher.AddressSet{
		Addresses:          filtered,
		Labels:             addresses.Labels,
		LocalTrafficPolicy: addresses.LocalTrafficPolicy,
		Cluster:            addresses.Cluster,
	}
}

// filterAddresses is responsible for filtering endpoints based on the node's
// topology zone. The client will only receive endpoints with the same
// consumption zone as the node. An endpoints consumption zone is set
// by its Hints field and can be different than its actual Topology zone.
// when service.spec.internalTrafficPolicy is set to local, Topology Aware
// Hints are not used.
func filterAddresses(cfg *endpointTranslatorConfig, available *watcher.AddressSet, log *logging.Entry) watcher.AddressSet {
	filtered := make(map[watcher.ID]watcher.Address)

	// If endpoint filtering is disabled globally or unsupported by the data
	// source, return all available addresses.
	if !cfg.enableEndpointFiltering || available.Cluster != "local" {
		for k, v := range available.Addresses {
			filtered[k] = v
		}
		return watcher.AddressSet{
			Addresses:          filtered,
			Labels:             available.Labels,
			LocalTrafficPolicy: available.LocalTrafficPolicy,
			Cluster:            available.Cluster,
		}
	}

	// If service.spec.internalTrafficPolicy is set to local, filter and return the addresses
	// for local node only
	if available.LocalTrafficPolicy {
		log.Debugf("Filtering through addresses that should be consumed by node %s", cfg.nodeName)
		for id, address := range available.Addresses {
			if address.Pod != nil && address.Pod.Spec.NodeName == cfg.nodeName {
				filtered[id] = address
			}
		}
		log.Debugf("Filtered from %d to %d addresses", len(available.Addresses), len(filtered))
		return watcher.AddressSet{
			Addresses:          filtered,
			Labels:             available.Labels,
			LocalTrafficPolicy: available.LocalTrafficPolicy,
			Cluster:            available.Cluster,
		}
	}
	// If any address does not have a hint, then all hints are ignored and all
	// available addresses are returned. This replicates kube-proxy behavior
	// documented in the KEP: https://github.com/kubernetes/enhancements/blob/master/keps/sig-network/2433-topology-aware-hints/README.md#kube-proxy
	for _, address := range available.Addresses {
		if len(address.ForZones) == 0 {
			for k, v := range available.Addresses {
				filtered[k] = v
			}
			log.Debugf("Hints not available on endpointslice. Zone Filtering disabled. Falling back to routing to all pods")
			return watcher.AddressSet{
				Addresses:          filtered,
				Labels:             available.Labels,
				LocalTrafficPolicy: available.LocalTrafficPolicy,
				Cluster:            available.Cluster,
			}
		}
	}

	// Each address that has a hint matching the node's zone should be added
	// to the set of addresses that will be returned.
	log.Debugf("Filtering through addresses that should be consumed by zone %s", cfg.nodeTopologyZone)
	for id, address := range available.Addresses {
		for _, zone := range address.ForZones {
			if zone.Name == cfg.nodeTopologyZone {
				filtered[id] = address
			}
		}
	}
	if len(filtered) > 0 {
		log.Debugf("Filtered from %d to %d addresses", len(available.Addresses), len(filtered))
		return watcher.AddressSet{
			Addresses:          filtered,
			Labels:             available.Labels,
			LocalTrafficPolicy: available.LocalTrafficPolicy,
			Cluster:            available.Cluster,
		}
	}

	// If there were no filtered addresses, then fall to using endpoints from
	// all zones.
	for k, v := range available.Addresses {
		filtered[k] = v
	}
	return watcher.AddressSet{
		Addresses:          filtered,
		Labels:             available.Labels,
		LocalTrafficPolicy: available.LocalTrafficPolicy,
		Cluster:            available.Cluster,
	}
}

// diffEndpoints calculates the difference between the filtered set of
// endpoints in the current (Add/Remove) operation and the snapshot of
// previously filtered endpoints.
func diffEndpoints(previous watcher.AddressSet, filtered watcher.AddressSet) (watcher.AddressSet, watcher.AddressSet) {
	add := make(map[watcher.ID]watcher.Address)
	remove := make(map[watcher.ID]watcher.Address)

	for id, new := range filtered.Addresses {
		old, ok := previous.Addresses[id]
		if !ok {
			add[id] = new
		} else if !reflect.DeepEqual(old, new) {
			add[id] = new
		}
	}

	for id, address := range previous.Addresses {
		if _, ok := filtered.Addresses[id]; !ok {
			remove[id] = address
		}
	}

	return watcher.AddressSet{
			Addresses:          add,
			Labels:             filtered.Labels,
			LocalTrafficPolicy: filtered.LocalTrafficPolicy,
			Cluster:            filtered.Cluster,
		},
		watcher.AddressSet{
			Addresses:          remove,
			Labels:             filtered.Labels,
			LocalTrafficPolicy: filtered.LocalTrafficPolicy,
			Cluster:            filtered.Cluster,
		}
}
