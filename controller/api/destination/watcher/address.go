package watcher

import (
	ewv1beta1 "github.com/linkerd/linkerd2/controller/gen/apis/externalworkload/v1beta1"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
)

type (
	// Address represents an individual port on a specific endpoint.
	// This endpoint might be the result of a the existence of a pod
	// that is targeted by this service; alternatively it can be the
	// case that this endpoint is not associated with a pod and maps
	// to some other IP (i.e. a remote service gateway)
	Address struct {
		IP                string
		Port              Port
		Pod               *corev1.Pod
		ExternalWorkload  *ewv1beta1.ExternalWorkload
		OwnerName         string
		OwnerKind         string
		Identity          string
		AuthorityOverride string
		Zone              *string
		ForZones          []discovery.ForZone
		OpaqueProtocol    bool
		Hostname          *string
	}

	// AddressSet is a set of Address, indexed by ID.
	// The ID can be either:
	// 1) A reference to service: id.Name contains both the service name and
	// the target IP and port (see newServiceRefAddress)
	// 2) A reference to a pod: id.Name refers to the pod's name, and
	// id.IPFamily refers to the ES AddressType (see newPodRefAddress).
	// 3) A reference to an ExternalWorkload: id.Name refers to the EW's name.
	AddressSet struct {
		Addresses map[ID]Address
		Labels    map[string]string
	}
)

// shallowCopy returns a shallow copy of addr, in the sense that the Pod and
// ExternalWorkload fields of the Addresses map values still point to the
// locations of the original variable
func (addr AddressSet) shallowCopy() AddressSet {
	addresses := make(map[ID]Address)
	for k, v := range addr.Addresses {
		addresses[k] = v
	}

	labels := make(map[string]string)
	for k, v := range addr.Labels {
		labels[k] = v
	}

	return AddressSet{
		Addresses: addresses,
		Labels:    labels,
	}
}
