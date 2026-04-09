package watcher

import (
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/discovery/v1"
)

type (
	filteredListenerGroup struct {
		key                     FilterKey
		nodeTopologyZone        string
		enableEndpointFiltering bool
		enableIPv6              bool
		localTrafficPolicy      bool
		snapshot                AddressSet
		listeners               []EndpointUpdateListener
	}
)

func newFilteredListenerGroup(key FilterKey, nodeTopologyZone string, enableIPv6 bool, localTrafficPolicy bool) *filteredListenerGroup {
	return &filteredListenerGroup{
		key:                     key,
		nodeTopologyZone:        nodeTopologyZone,
		enableEndpointFiltering: key.EnableEndpointFiltering,
		enableIPv6:              enableIPv6,
		localTrafficPolicy:      localTrafficPolicy,
		snapshot:                AddressSet{Addresses: make(map[ID]*Address)},
	}
}

func (group *filteredListenerGroup) publishDiff(addresses AddressSet) {
	filtered := group.filterAddresses(addresses)
	add, remove := diffAddresses(group.snapshot, filtered)
	group.snapshot = filtered

	for _, listener := range group.listeners {
		if len(remove.Addresses) > 0 {
			listener.Remove(remove)
		}
		if len(add.Addresses) > 0 {
			listener.Add(add)
		}
	}
}

func (group *filteredListenerGroup) publishNoEndpoints() {
	remove := group.snapshot
	group.snapshot = AddressSet{Addresses: make(map[ID]*Address)}

	for _, listener := range group.listeners {
		if len(remove.Addresses) > 0 {
			listener.Remove(remove)
		}
	}
}

func (group *filteredListenerGroup) updateLocalTrafficPolicy(localTrafficPolicy bool) {
	group.localTrafficPolicy = localTrafficPolicy
	group.publishDiff(group.snapshot)
}

func (group *filteredListenerGroup) filterAddresses(addresses AddressSet) AddressSet {
	filtered := make(map[ID]*Address)

	for id, address := range addresses.Addresses {
		// If hostname filtering is specified, only include addresses that match the hostname.
		// This filtering should be applied even if endpoint filtering is disabled.
		if group.key.Hostname != "" && group.key.Hostname != address.Pod.Spec.Hostname {
			continue
		}

		if group.enableEndpointFiltering {
			// If the Service has local traffic policy enabled, only include addresses that are local to the node.
			// Otherwise, perform zone filtering if the address has zone information.
			if group.localTrafficPolicy {
				if address.Pod == nil || address.Pod.Spec.NodeName != group.key.NodeName {
					continue
				}
			} else {
				if len(address.ForZones) > 0 {
					if !containsZone(address.ForZones, group.nodeTopologyZone) {
						continue
					}
				}
			}
		}

		filtered[id] = address
	}

	// If zone filtering removed all addresses, we fall back to including all addresses.
	// Note that hostname filtering is still applied in this case, if specified.
	if group.enableEndpointFiltering && !group.localTrafficPolicy && len(filtered) == 0 {
		for k, v := range addresses.Addresses {
			if group.key.Hostname == "" || v.Pod.Spec.Hostname == group.key.Hostname {
				filtered[k] = v
			}
		}
	}

	return selectAddressFamily(AddressSet{
		Addresses: filtered,
		Labels:    addresses.Labels,
	}, group.enableIPv6)
}

func containsZone(zones []v1.ForZone, zone string) bool {
	for _, z := range zones {
		if z.Name == zone {
			return true
		}
	}
	return false
}

func selectAddressFamily(addresses AddressSet, enableIPv6 bool) AddressSet {
	filtered := make(map[ID]*Address)
	for id, addr := range addresses.Addresses {
		if id.IPFamily == corev1.IPv6Protocol && !enableIPv6 {
			continue
		}

		if id.IPFamily == corev1.IPv4Protocol && enableIPv6 {
			altID := id
			altID.IPFamily = corev1.IPv6Protocol
			if _, ok := addresses.Addresses[altID]; ok {
				continue
			}
		}

		filtered[id] = addr
	}

	return AddressSet{
		Addresses: filtered,
		Labels:    addresses.Labels,
	}
}
