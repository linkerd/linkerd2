package k8s

import (
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

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

func toRuntimeObject(config string) (runtime.Object, error) {
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode([]byte(config), nil, nil)
	return obj, err
}

func NewFakeAPI(configs ...string) (*API, error) {
	objs := []runtime.Object{}
	for _, config := range configs {
		obj, err := toRuntimeObject(config)
		if err != nil {
			return nil, err
		}
		objs = append(objs, obj)
	}

	clientSet := fake.NewSimpleClientset(objs...)
	return NewAPI(clientSet), nil
}
