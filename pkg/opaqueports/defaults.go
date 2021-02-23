package opaqueports

// DefaultOpaquePorts is the default list of opaque ports that the destination
// server will use to determine whether a destination is an opaque protocol.
// When a pod or service already has its own annotation, that value will have
// priority of this.
//
// Note: Keep in sync with proxy.opaquePorts in values.yaml
var DefaultOpaquePorts = make(map[uint32]struct{})

func init() {
	DefaultOpaquePorts[25] = struct{}{}
	DefaultOpaquePorts[443] = struct{}{}
	DefaultOpaquePorts[587] = struct{}{}
	DefaultOpaquePorts[3306] = struct{}{}
	DefaultOpaquePorts[11211] = struct{}{}
}
