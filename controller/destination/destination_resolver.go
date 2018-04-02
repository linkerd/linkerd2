package destination

import (
	common "github.com/runconduit/conduit/controller/gen/common"
)

type streamingDestinationResolver interface {
	canResolve(host string, port int) (bool, error)
	streamResolution(host string, port int, listener updateListener) error
}

type updateListener interface {
	Update(add []common.TcpAddress, remove []common.TcpAddress)
	Done() <-chan struct{}
	NoEndpoints(exists bool)
}
