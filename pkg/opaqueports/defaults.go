package opaqueports

// DefaultOpaquePorts is the default list of opaque ports that the destination
// server will use to determine whether a destination is an opaque protocol.
// When a pod or service already has its own annotation, that value will have
// priority of this.
//
var DefaultOpaquePorts = map[uint32]struct{}{}
