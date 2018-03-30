package k8s

import "k8s.io/api/core/v1"

type MockEndpointsWatcher struct {
	HostReceived         string
	ListenerSubscribed   EndpointsListener
	ListenerUnsubscribed EndpointsListener
	ServiceToReturn      *v1.Service
	ExistsToReturn       bool
	ErrToReturn          error
}

func (m *MockEndpointsWatcher) GetService(service string) (*v1.Service, bool, error) {
	m.HostReceived = service
	return m.ServiceToReturn, m.ExistsToReturn, m.ErrToReturn
}
func (m *MockEndpointsWatcher) Subscribe(service string, port uint32, listener EndpointsListener) error {
	m.ListenerSubscribed = listener
	return m.ErrToReturn
}
func (m *MockEndpointsWatcher) Unsubscribe(service string, port uint32, listener EndpointsListener) error {
	m.ListenerUnsubscribed = listener
	return m.ErrToReturn
}
func (m *MockEndpointsWatcher) Run() error {
	return m.ErrToReturn
}

func (m *MockEndpointsWatcher) Stop() {}
