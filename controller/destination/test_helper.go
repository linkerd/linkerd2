package destination

import (
	"context"

	common "github.com/runconduit/conduit/controller/gen/common"
)

type collectUpdateListener struct {
	added   []common.TcpAddress
	removed []common.TcpAddress
	context context.Context
}

func (c *collectUpdateListener) Update(add []common.TcpAddress, remove []common.TcpAddress) {
	c.added = append(c.added, add...)
	c.removed = append(c.removed, remove...)
}

func (c *collectUpdateListener) Done() <-chan struct{} {
	return c.context.Done()
}

func (c *collectUpdateListener) NoEndpoints(exists bool) {}

func newCollectUpdateListener() (*collectUpdateListener, context.CancelFunc) {
	ctx, cancelFn := context.WithCancel(context.Background())
	return &collectUpdateListener{context: ctx}, cancelFn
}

type mockDnsWatcher struct {
	ListenerSubscribed   DnsListener
	ListenerUnsubscribed DnsListener
	errToReturn          error
}

func (m *mockDnsWatcher) Subscribe(host string, listener DnsListener) error {
	m.ListenerSubscribed = listener
	return m.errToReturn
}

func (m *mockDnsWatcher) Unsubscribe(host string, listener DnsListener) error {
	m.ListenerUnsubscribed = listener
	return m.errToReturn
}
