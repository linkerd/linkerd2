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

type InMemoryPodIndex struct {
	BackingMap map[string]*v1.Pod
}

func (i *InMemoryPodIndex) GetPod(key string) (*v1.Pod, error) {
	return i.BackingMap[key], nil
}

func (i *InMemoryPodIndex) GetPodsByIndex(key string) ([]*v1.Pod, error) {
	return []*v1.Pod{i.BackingMap[key]}, nil
}

func (i *InMemoryPodIndex) List() ([]*v1.Pod, error) {
	var pods []*v1.Pod
	for _, value := range i.BackingMap {
		pods = append(pods, value)
	}

	return pods, nil
}
func (i *InMemoryPodIndex) Run() error { return nil }
func (i *InMemoryPodIndex) Stop()      {}

func NewEmptyPodIndex() PodIndex {
	return &InMemoryPodIndex{BackingMap: map[string]*v1.Pod{}}
}
