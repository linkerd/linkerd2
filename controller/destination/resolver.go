package destination

type streamingDestinationResolver interface {
	canResolve(host string, port int) (bool, error)
	streamResolution(host string, port int, listener updateListener) error
	stop()
}
