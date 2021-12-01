package destination

import (
	"github.com/linkerd/linkerd2/controller/api/destination/watcher"
	"github.com/linkerd/linkerd2/controller/gen/apis/serviceprofile/v1alpha2"
)

type serverAdaptor struct {
	listener        watcher.ProfileUpdateListener
	port            watcher.Port
	isOpaque        bool
	annotationPorts map[uint32]struct{}
}

func newServerAdaptor(listener watcher.ProfileUpdateListener, port watcher.Port) *serverAdaptor {
	return &serverAdaptor{
		listener: listener,
		port:     port,
	}
}

func (sa *serverAdaptor) Update(isOpaque bool) {
	sa.isOpaque = isOpaque
	ports := make(map[uint32]struct{})
	for port := range sa.annotationPorts {
		ports[port] = struct{}{}
	}
	// If a Server has marked the port as opaque, then ensure the port is in
	// the set of opaque ports.
	if isOpaque {
		ports[sa.port] = struct{}{}
	}
	sp := v1alpha2.ServiceProfile{}
	sp.Spec.OpaquePorts = ports
	sa.listener.Update(&sp)
}
